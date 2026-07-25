package offload

import (
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"UAmask/internal/events"
	"UAmask/internal/model"
	"UAmask/internal/policy"
	"UAmask/internal/state"
	"UAmask/internal/testkit/manualclock"

	"github.com/sirupsen/logrus"
)

type targetOffloaderStub struct {
	mu        sync.Mutex
	signals   []model.OffloadSignal
	result    error
	handled   chan model.OffloadSignal
	completed chan struct{}
}

func (s *targetOffloaderStub) Handle(signal model.OffloadSignal, completion func(error)) error {
	s.mu.Lock()
	s.signals = append(s.signals, signal)
	s.mu.Unlock()
	if s.handled != nil {
		s.handled <- signal
	}
	completion(s.result)
	if s.completed != nil {
		s.completed <- struct{}{}
	}
	return nil
}

func (s *targetOffloaderStub) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.signals)
}

type collectorSink struct {
	mu     sync.Mutex
	events []model.SessionEvent
}

type delayedTargetOffloaderStub struct {
	mu         sync.Mutex
	count      int
	completion func(error)
}

type blockingTargetOffloaderStub struct {
	mu      sync.Mutex
	count   int
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingTargetOffloaderStub) Handle(_ model.OffloadSignal, completion func(error)) error {
	s.mu.Lock()
	s.count++
	s.mu.Unlock()
	s.once.Do(func() { close(s.started) })
	<-s.release
	completion(nil)
	return nil
}

func (s *blockingTargetOffloaderStub) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

func (s *delayedTargetOffloaderStub) Handle(_ model.OffloadSignal, completion func(error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count++
	s.completion = completion
	return nil
}

func (s *delayedTargetOffloaderStub) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

func (s *delayedTargetOffloaderStub) Complete(err error) {
	s.mu.Lock()
	completion := s.completion
	s.mu.Unlock()
	completion(err)
}

func (c *collectorSink) Append(event model.SessionEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
}

func (c *collectorSink) HasType(eventType model.SessionEventType) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, event := range c.events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func TestCoordinatorUsesSnapshotAndEmitsFacts(t *testing.T) {
	t.Parallel()

	runtimeClock := manualclock.New(time.Unix(100, 0))
	profiles := state.NewStoreWithClock(state.Config{
		NonHTTPEnabled:         true,
		NonHTTPThreshold:       2,
		HTTPCooldownPeriod:     time.Minute,
		DecisionDelay:          10 * time.Millisecond,
		ProfileCleanupInterval: time.Minute,
	}, runtimeClock)

	targetOffloader := &targetOffloaderStub{}
	collector := &collectorSink{}
	dispatcher := events.NewDispatcher()
	coordinator := NewCoordinatorWithClock(
		nilLogger(),
		profiles,
		policy.NewTargetPolicyWithClock(60, 3600, 2, true, runtimeClock),
		targetOffloader,
		NoopFlowOffloader{},
		dispatcher,
		runtimeClock,
	)
	dispatcher.Add(profiles, collector, coordinator)

	target := model.Target{IP: "1.1.1.1", Port: 80}
	event := model.SessionEvent{
		Type:        model.EventRewriteSkipped,
		Target:      target,
		Reason:      "Hit Firewall UA Whitelist",
		FirewallHit: true,
		Timestamp:   runtimeClock.Now(),
	}

	profiles.Append(event)
	coordinator.Append(event)

	if targetOffloader.Count() != 1 {
		t.Fatalf("expected one target offload signal, got %d", targetOffloader.Count())
	}
	if !collector.HasType(model.EventTargetOffloadSucceeded) {
		t.Fatalf("expected target offload success fact to be emitted")
	}
}

func TestTargetOffloadFlow(t *testing.T) {
	t.Run("FLOW-NONHTTP-001/facts produce one target action and result fact", func(t *testing.T) {
		runtimeClock := manualclock.New(time.Unix(100, 0))
		profiles := state.NewStoreWithClock(state.Config{
			NonHTTPEnabled:         true,
			NonHTTPThreshold:       2,
			HTTPCooldownPeriod:     time.Minute,
			DecisionDelay:          10 * time.Second,
			ProfileCleanupInterval: time.Minute,
		}, runtimeClock)
		targetOffloader := &targetOffloaderStub{
			handled:   make(chan model.OffloadSignal, 1),
			completed: make(chan struct{}, 1),
		}
		dispatcher := events.NewDispatcher()
		coordinator := NewCoordinatorWithClock(
			nilLogger(),
			profiles,
			policy.NewTargetPolicyWithClock(60, 3600, 2, true, runtimeClock),
			targetOffloader,
			NoopFlowOffloader{},
			dispatcher,
			runtimeClock,
		)
		defer coordinator.Stop()
		dispatcher.Add(profiles, coordinator)

		target := model.Target{IP: "203.0.113.10", Port: 443}
		first := model.SessionEvent{
			Type:      model.EventNonHTTPObserved,
			SessionID: 1,
			Target:    target,
			Timestamp: runtimeClock.Now(),
		}
		dispatcher.Append(first)
		if snapshot := profiles.Snapshot(target); snapshot.NonHTTPScore != 1 || !snapshot.PendingTargetOffloadAt.IsZero() {
			t.Fatalf("first fact produced unexpected snapshot: %+v", snapshot)
		}

		second := first
		second.SessionID = 2
		second.Timestamp = first.Timestamp.Add(time.Millisecond)
		dispatcher.Append(second)
		runtimeClock.Advance(10*time.Second + time.Millisecond)

		var signal model.OffloadSignal
		select {
		case signal = <-targetOffloader.handled:
		default:
			t.Fatal("target offload signal was not submitted")
		}
		if signal.Kind != model.SignalTryTargetOffload || signal.Target != target || signal.Timeout != 60 || signal.Reason != "non-http threshold reached" {
			t.Fatalf("unexpected target offload signal: %+v", signal)
		}
		select {
		case <-targetOffloader.completed:
		default:
			t.Fatal("target offload completion was not projected")
		}

		snapshot := profiles.Snapshot(target)
		if snapshot.NonHTTPScore != 0 || snapshot.TargetOffloadUntil.IsZero() || snapshot.LastEventType != model.EventTargetOffloadSucceeded {
			t.Fatalf("success fact was not projected into state: %+v", snapshot)
		}

		third := second
		third.SessionID = 3
		third.Timestamp = second.Timestamp.Add(time.Millisecond)
		dispatcher.Append(third)
		if targetOffloader.Count() != 1 {
			t.Fatalf("offloaded target was submitted %d times, want 1", targetOffloader.Count())
		}
	})
}

func TestCoordinatorKeepsTargetInFlightUntilResultEventIsProjected(t *testing.T) {
	t.Parallel()

	profiles := state.NewStore(state.Config{NonHTTPThreshold: 2})
	targetOffloader := &delayedTargetOffloaderStub{}
	dispatcher := events.NewDispatcher()
	coordinator := NewCoordinator(
		nilLogger(),
		profiles,
		policy.NewTargetPolicy(60, 3600, 2, true),
		targetOffloader,
		NoopFlowOffloader{},
		dispatcher,
	)
	dispatcher.Add(profiles, coordinator)

	target := model.Target{IP: "1.1.1.1", Port: 80}
	signal := model.OffloadSignal{
		Kind:    model.SignalTryTargetOffload,
		Target:  target,
		Timeout: 60,
	}
	coordinator.Handle(signal)
	coordinator.Handle(signal)

	if targetOffloader.Count() != 1 {
		t.Fatalf("expected duplicate signal to be suppressed, got %d submissions", targetOffloader.Count())
	}
	if snapshot := profiles.Snapshot(target); snapshot.Found {
		t.Fatalf("target must not be projected as offloaded before apply completes: %+v", snapshot)
	}
	targetOffloader.Complete(nil)
	if snapshot := profiles.Snapshot(target); !snapshot.Found || snapshot.TargetOffloadUntil.IsZero() {
		t.Fatalf("expected successful apply result to update profile, got %+v", snapshot)
	}
}

func TestCoordinatorProjectsApplyFailure(t *testing.T) {
	t.Parallel()

	profiles := state.NewStore(state.Config{})
	collector := &collectorSink{}
	targetOffloader := &targetOffloaderStub{result: errors.New("apply failed")}
	dispatcher := events.NewDispatcher()
	coordinator := NewCoordinator(
		nilLogger(),
		profiles,
		policy.NewTargetPolicy(60, 3600, 2, true),
		targetOffloader,
		NoopFlowOffloader{},
		dispatcher,
	)
	dispatcher.Add(profiles, collector, coordinator)
	target := model.Target{IP: "1.1.1.1", Port: 80}

	coordinator.Handle(model.OffloadSignal{
		Kind:    model.SignalTryTargetOffload,
		Target:  target,
		Timeout: 60,
	})

	if !collector.HasType(model.EventTargetOffloadFailed) {
		t.Fatal("expected actual apply failure to be emitted")
	}
	if snapshot := profiles.Snapshot(target); !snapshot.TargetOffloadUntil.IsZero() {
		t.Fatalf("failed apply must not suppress retries: %+v", snapshot)
	}
}

func TestCoordinatorStopCancelsPendingTargetEvaluation(t *testing.T) {
	t.Parallel()

	runtimeClock := manualclock.New(time.Unix(100, 0))
	profiles := state.NewStoreWithClock(state.Config{
		NonHTTPEnabled:   true,
		NonHTTPThreshold: 1,
		DecisionDelay:    30 * time.Second,
	}, runtimeClock)
	targetOffloader := &delayedTargetOffloaderStub{}
	dispatcher := events.NewDispatcher()
	coordinator := NewCoordinatorWithClock(
		nilLogger(),
		profiles,
		policy.NewTargetPolicyWithClock(60, 3600, 1, true, runtimeClock),
		targetOffloader,
		NoopFlowOffloader{},
		dispatcher,
		runtimeClock,
	)
	target := model.Target{IP: "1.1.1.1", Port: 80}
	event := model.SessionEvent{
		Type:      model.EventNonHTTPObserved,
		Target:    target,
		Timestamp: runtimeClock.Now(),
	}
	profiles.Append(event)
	coordinator.Append(event)

	coordinator.Stop()
	runtimeClock.Advance(30 * time.Second)
	if targetOffloader.Count() != 0 {
		t.Fatalf("expected stopped coordinator to suppress timer submission, got %d", targetOffloader.Count())
	}
}

func TestCoordinatorStopWaitsForActiveSubmissionAndRejectsNewWork(t *testing.T) {
	t.Parallel()

	targetOffloader := &blockingTargetOffloaderStub{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	coordinator := NewCoordinator(
		nilLogger(),
		state.NewStore(state.Config{}),
		policy.NewTargetPolicy(60, 3600, 2, true),
		targetOffloader,
		NoopFlowOffloader{},
		events.NewDispatcher(),
	)
	signal := model.OffloadSignal{
		Kind:    model.SignalTryTargetOffload,
		Target:  model.Target{IP: "1.1.1.1", Port: 80},
		Timeout: 60,
	}
	handleDone := make(chan struct{})
	go func() {
		coordinator.Handle(signal)
		close(handleDone)
	}()

	select {
	case <-targetOffloader.started:
	case <-time.After(time.Second):
		t.Fatal("target submission did not start")
	}
	stopDone := make(chan struct{})
	go func() {
		coordinator.Stop()
		close(stopDone)
	}()
	select {
	case <-stopDone:
		t.Fatal("Stop returned while target submission was active")
	case <-time.After(20 * time.Millisecond):
	}

	close(targetOffloader.release)
	select {
	case <-handleDone:
	case <-time.After(time.Second):
		t.Fatal("target submission did not finish")
	}
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("Stop did not finish after active submission completed")
	}

	coordinator.Handle(signal)
	if targetOffloader.Count() != 1 {
		t.Fatalf("expected stopped coordinator to reject new work, got %d submissions", targetOffloader.Count())
	}
}

func nilLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return logger
}
