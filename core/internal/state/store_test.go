package state

import (
	"testing"
	"time"

	"UAmask/internal/model"
	"UAmask/internal/testkit/manualclock"
)

func TestStoreAggregatesFactsIntoProfile(t *testing.T) {
	t.Parallel()

	runtimeClock := manualclock.New(time.Unix(100, 0))
	store := NewStoreWithClock(Config{
		NonHTTPEnabled:         true,
		NonHTTPThreshold:       2,
		HTTPCooldownPeriod:     30 * time.Second,
		DecisionDelay:          5 * time.Second,
		ProfileCleanupInterval: time.Minute,
	}, runtimeClock)

	target := model.Target{IP: "1.1.1.1", Port: 80}
	now := runtimeClock.Now()

	store.Append(model.SessionEvent{Type: model.EventNonHTTPObserved, Target: target, Timestamp: now})
	first := store.Snapshot(target)
	if first.NonHTTPScore != 1 {
		t.Fatalf("expected non-http score 1, got %d", first.NonHTTPScore)
	}
	if !first.PendingTargetOffloadAt.IsZero() {
		t.Fatalf("expected no pending offload after first observation")
	}

	store.Append(model.SessionEvent{Type: model.EventNonHTTPObserved, Target: target, Timestamp: now.Add(time.Second)})
	second := store.Snapshot(target)
	if second.NonHTTPScore != 2 {
		t.Fatalf("expected non-http score 2, got %d", second.NonHTTPScore)
	}
	if second.PendingTargetOffloadAt.IsZero() {
		t.Fatalf("expected pending offload after threshold is reached")
	}

	store.Append(model.SessionEvent{
		Type:        model.EventRewriteSkipped,
		Target:      target,
		Reason:      "Hit Firewall UA Whitelist",
		FirewallHit: true,
		Timestamp:   now.Add(2 * time.Second),
	})
	third := store.Snapshot(target)
	if third.FirewallWhitelistHits != 1 {
		t.Fatalf("expected one firewall whitelist hit, got %d", third.FirewallWhitelistHits)
	}

	store.Append(model.SessionEvent{Type: model.EventHTTPRequestObserved, Target: target, Timestamp: now.Add(3 * time.Second)})
	afterHTTP := store.Snapshot(target)
	if afterHTTP.NonHTTPScore != 0 {
		t.Fatalf("expected non-http score reset, got %d", afterHTTP.NonHTTPScore)
	}
	if afterHTTP.PendingTargetOffloadAt != (time.Time{}) {
		t.Fatalf("expected pending offload cleared by HTTP observation")
	}
	if afterHTTP.HTTPCooldownUntil.IsZero() {
		t.Fatalf("expected HTTP cooldown to be set")
	}
}

func TestStoreDoesNotScheduleNonHTTPWhenFeatureIsDisabled(t *testing.T) {
	t.Parallel()

	runtimeClock := manualclock.New(time.Unix(100, 0))
	store := NewStoreWithClock(Config{
		NonHTTPEnabled:         false,
		NonHTTPThreshold:       1,
		DecisionDelay:          time.Second,
		ProfileCleanupInterval: time.Minute,
	}, runtimeClock)
	target := model.Target{IP: "1.1.1.1", Port: 80}
	store.Append(model.SessionEvent{
		Type:      model.EventNonHTTPObserved,
		Target:    target,
		Timestamp: runtimeClock.Now(),
	})

	snapshot := store.Snapshot(target)
	if snapshot.NonHTTPScore != 0 || !snapshot.PendingTargetOffloadAt.IsZero() {
		t.Fatalf("expected disabled non-http aggregation to stay idle, got %+v", snapshot)
	}
}
