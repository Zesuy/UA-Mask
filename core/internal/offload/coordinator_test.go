package offload

import (
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
}

func (s *targetOffloaderStub) Handle(signal model.OffloadSignal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.signals = append(s.signals, signal)
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
		policy.NewTargetPolicy(60, 3600, 2),
		targetOffloader,
		NoopFlowOffloader{},
		dispatcher,
	)
	dispatcher.Add(profiles, collector, coordinator)

	target := model.Target{IP: "1.1.1.1", Port: 80}
	event := model.SessionEvent{
		Type:      model.EventRewriteSkipped,
		Target:    target,
		Reason:    "Hit Firewall UA Whitelist",
		Timestamp: time.Now(),
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

func nilLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return logger
}
