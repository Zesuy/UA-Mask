package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"UAmask/internal/model"
	"UAmask/internal/testkit/scenario"
)

const (
	decisionDelay = 10 * time.Second
	cooldown      = 30 * time.Second
)

var target = model.Target{
	Address: "203.0.113.10:443",
	IP:      "203.0.113.10",
	Port:    443,
}

func TestDeterministicTargetFlow(t *testing.T) {
	t.Run("FLOW-NONHTTP-101 threshold waits for decision delay", func(t *testing.T) {
		s := newTargetScenario(t)
		ctx := testContext(t)

		emit(t, ctx, s, model.EventNonHTTPObserved)
		if got := s.Signals.Len(); got != 0 {
			t.Fatalf("signals before threshold = %d, want 0", got)
		}

		emit(t, ctx, s, model.EventNonHTTPObserved)
		pendingAt := s.Snapshot(target).PendingTargetOffloadAt
		if want := s.Clock.Now().Add(decisionDelay); !pendingAt.Equal(want) {
			t.Fatalf("pending target offload = %v, want %v", pendingAt, want)
		}

		advance(t, ctx, s, decisionDelay-time.Nanosecond)
		if got := s.Signals.Len(); got != 0 {
			t.Fatalf("signals before deadline = %d, want 0", got)
		}
		advance(t, ctx, s, time.Nanosecond)
		assertSignalCount(t, s, 1)
	})

	t.Run("FLOW-HTTP-101 HTTP cancels pending offload and starts cooldown", func(t *testing.T) {
		s := newTargetScenario(t)
		ctx := testContext(t)

		emit(t, ctx, s, model.EventNonHTTPObserved)
		emit(t, ctx, s, model.EventNonHTTPObserved)
		advance(t, ctx, s, decisionDelay/2)
		emit(t, ctx, s, model.EventHTTPRequestObserved)

		snapshot := s.Snapshot(target)
		if snapshot.NonHTTPScore != 0 || !snapshot.PendingTargetOffloadAt.IsZero() {
			t.Fatalf("HTTP snapshot = %+v, want reset score and no pending offload", snapshot)
		}
		advance(t, ctx, s, decisionDelay)
		emit(t, ctx, s, model.EventNonHTTPObserved)
		if got := s.Snapshot(target).NonHTTPScore; got != 0 {
			t.Fatalf("score during cooldown = %d, want 0", got)
		}
		assertSignalCount(t, s, 0)
	})

	t.Run("FLOW-HTTP-102 cooldown expiry permits offload", func(t *testing.T) {
		s := newTargetScenario(t)
		ctx := testContext(t)

		emit(t, ctx, s, model.EventHTTPRequestObserved)
		advance(t, ctx, s, cooldown)
		emit(t, ctx, s, model.EventNonHTTPObserved)
		emit(t, ctx, s, model.EventNonHTTPObserved)
		advance(t, ctx, s, decisionDelay)

		assertSignalCount(t, s, 1)
	})

	t.Run("FLOW-OFFLOAD-101 success projects fact and validity", func(t *testing.T) {
		s := newTargetScenario(t)
		ctx := testContext(t)
		triggerOffload(t, ctx, s)

		complete(t, ctx, s, nil)

		if got := s.Events.Count(model.EventTargetOffloadSucceeded); got != 1 {
			t.Fatalf("success events = %d, want 1", got)
		}
		snapshot := s.Snapshot(target)
		if snapshot.NonHTTPScore != 0 {
			t.Fatalf("score after success = %d, want 0", snapshot.NonHTTPScore)
		}
		wantUntil := s.Clock.Now().Add(time.Minute)
		if !snapshot.TargetOffloadUntil.Equal(wantUntil) {
			t.Fatalf("offload validity = %v, want %v", snapshot.TargetOffloadUntil, wantUntil)
		}
		emit(t, ctx, s, model.EventNonHTTPObserved)
		emit(t, ctx, s, model.EventNonHTTPObserved)
		advance(t, ctx, s, decisionDelay)
		assertSignalCount(t, s, 1)
	})

	t.Run("FLOW-OFFLOAD-102 failure permits retry", func(t *testing.T) {
		s := newTargetScenario(t)
		ctx := testContext(t)
		triggerOffload(t, ctx, s)

		complete(t, ctx, s, errors.New("apply failed"))

		if got := s.Events.Count(model.EventTargetOffloadFailed); got != 1 {
			t.Fatalf("failure events = %d, want 1", got)
		}
		if until := s.Snapshot(target).TargetOffloadUntil; !until.IsZero() {
			t.Fatalf("offload validity after failure = %v, want zero", until)
		}
		emit(t, ctx, s, model.EventNonHTTPObserved)
		advance(t, ctx, s, decisionDelay)
		assertSignalCount(t, s, 2)
	})

	t.Run("FLOW-OFFLOAD-103 in-flight target is deduplicated", func(t *testing.T) {
		s := newTargetScenario(t)
		ctx := testContext(t)
		triggerOffload(t, ctx, s)

		emit(t, ctx, s, model.EventNonHTTPObserved)
		advance(t, ctx, s, decisionDelay)

		assertSignalCount(t, s, 1)
		complete(t, ctx, s, nil)
		if got := s.Batches.Len(); got != 1 {
			t.Fatalf("firewall batches = %d, want 1", got)
		}
	})

	t.Run("FLOW-SHUTDOWN-101 coordinator stop cancels decision timer", func(t *testing.T) {
		s := newTargetScenario(t)
		ctx := testContext(t)

		emit(t, ctx, s, model.EventNonHTTPObserved)
		emit(t, ctx, s, model.EventNonHTTPObserved)
		s.Coordinator.Stop()
		advance(t, ctx, s, decisionDelay)

		assertSignalCount(t, s, 0)
	})

	t.Run("FLOW-SHUTDOWN-102 close projects queued executor result", func(t *testing.T) {
		s := scenario.NewTarget(testTargetConfig())
		ctx := testContext(t)
		triggerOffload(t, ctx, s)

		if err := s.Close(ctx); err != nil {
			t.Fatalf("close scenario: %v", err)
		}
		if got := s.Events.Count(model.EventTargetOffloadSucceeded); got != 1 {
			t.Fatalf("success events after close = %d, want 1", got)
		}
		if got := s.Snapshot(target).NonHTTPScore; got != 0 {
			t.Fatalf("score after close = %d, want 0", got)
		}
	})
}

func newTargetScenario(t *testing.T) *scenario.Target {
	t.Helper()
	s := scenario.NewTarget(testTargetConfig())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Close(ctx); err != nil {
			t.Errorf("close scenario: %v", err)
		}
	})
	return s
}

func testTargetConfig() scenario.TargetConfig {
	return scenario.TargetConfig{
		Start:                time.Unix(1_700_000_000, 0),
		NonHTTPThreshold:     2,
		DecisionDelay:        decisionDelay,
		HTTPCooldownPeriod:   cooldown,
		TargetOffloadTimeout: 60,
	}
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func emit(t *testing.T, ctx context.Context, s *scenario.Target, eventType model.SessionEventType) {
	t.Helper()
	if err := s.Emit(ctx, model.SessionEvent{Type: eventType, Target: target}); err != nil {
		t.Fatalf("emit %s: %v", eventType, err)
	}
}

func advance(t *testing.T, ctx context.Context, s *scenario.Target, duration time.Duration) {
	t.Helper()
	if err := s.Advance(ctx, duration); err != nil {
		t.Fatalf("advance %v: %v", duration, err)
	}
}

func triggerOffload(t *testing.T, ctx context.Context, s *scenario.Target) {
	t.Helper()
	emit(t, ctx, s, model.EventNonHTTPObserved)
	emit(t, ctx, s, model.EventNonHTTPObserved)
	advance(t, ctx, s, decisionDelay)
	assertSignalCount(t, s, 1)
}

func complete(t *testing.T, ctx context.Context, s *scenario.Target, result error) {
	t.Helper()
	if err := s.CompleteNext(ctx, result); err != nil {
		t.Fatalf("complete target offload: %v", err)
	}
}

func assertSignalCount(t *testing.T, s *scenario.Target, want int) {
	t.Helper()
	if got := s.Signals.Len(); got != want {
		t.Fatalf("target signals = %d, want %d", got, want)
	}
}
