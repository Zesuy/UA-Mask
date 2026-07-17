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

	"github.com/sirupsen/logrus"
)

type targetOffloaderStub struct {
	mu      sync.Mutex
	signals []model.OffloadSignal
	result  error
}

func (s *targetOffloaderStub) Handle(signal model.OffloadSignal, completion func(error)) error {
	s.mu.Lock()
	s.signals = append(s.signals, signal)
	s.mu.Unlock()
	completion(s.result)
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

	profiles := state.NewStore(state.Config{
		NonHTTPEnabled:         true,
		NonHTTPThreshold:       2,
		HTTPCooldownPeriod:     time.Minute,
		DecisionDelay:          10 * time.Millisecond,
		ProfileCleanupInterval: time.Minute,
	})

	targetOffloader := &targetOffloaderStub{}
	collector := &collectorSink{}
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
	event := model.SessionEvent{
		Type:        model.EventRewriteSkipped,
		Target:      target,
		Reason:      "Hit Firewall UA Whitelist",
		FirewallHit: true,
		Timestamp:   time.Now(),
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

	profiles := state.NewStore(state.Config{
		NonHTTPEnabled:   true,
		NonHTTPThreshold: 1,
		DecisionDelay:    30 * time.Millisecond,
	})
	targetOffloader := &delayedTargetOffloaderStub{}
	dispatcher := events.NewDispatcher()
	coordinator := NewCoordinator(
		nilLogger(),
		profiles,
		policy.NewTargetPolicy(60, 3600, 1, true),
		targetOffloader,
		NoopFlowOffloader{},
		dispatcher,
	)
	target := model.Target{IP: "1.1.1.1", Port: 80}
	event := model.SessionEvent{
		Type:      model.EventNonHTTPObserved,
		Target:    target,
		Timestamp: time.Now(),
	}
	profiles.Append(event)
	coordinator.Append(event)

	coordinator.Stop()
	time.Sleep(60 * time.Millisecond)
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
