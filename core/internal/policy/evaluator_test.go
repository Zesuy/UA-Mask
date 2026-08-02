package policy

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"UAmask/internal/model"
	"UAmask/internal/testkit/manualclock"
)

func TestEvaluateSessionUsesRewriteInputWithoutSideEffects(t *testing.T) {
	t.Parallel()

	evaluator := NewSessionPolicy()
	sessionState := &model.SessionState{Target: model.Target{IP: "1.1.1.1", Port: 80}}
	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("User-Agent", "Original")

	decision := evaluator.Evaluate(sessionState, model.SessionInput{
		Kind:           model.SessionInputRequest,
		Protocol:       model.ProtocolHTTP,
		Request:        req,
		Modified:       true,
		FromCache:      true,
		Reason:         "rewrite input",
		FirewallHit:    true,
		DropConnection: false,
	}, model.ProfileSnapshot{})

	if decision.Action != model.ActionContinue {
		t.Fatalf("expected continue action, got %s", decision.Action)
	}
	if !decision.Modified {
		t.Fatalf("expected modified request")
	}
	if !decision.FromCache {
		t.Fatalf("expected cache flag to be propagated")
	}
	if got := decision.Reason; got != "rewrite input" {
		t.Fatalf("expected rewrite reason, got %q", got)
	}
	if !decision.FirewallHit {
		t.Fatalf("expected firewall hit flag to be propagated")
	}
	if got := req.Header.Get("User-Agent"); got != "Original" {
		t.Fatalf("policy must not rewrite UA, got %q", got)
	}
	if sessionState.ConsecutiveRewrite != 0 || sessionState.ConsecutiveNoRewrite != 0 {
		t.Fatalf("policy must not mutate session counters: %+v", sessionState)
	}
}

func TestEvaluateTargetUsesSnapshotOnly(t *testing.T) {
	t.Parallel()

	runtimeClock := manualclock.New(time.Unix(100, 0))
	evaluator := NewTargetPolicyWithClock(60, 3600, 2, true, runtimeClock)
	target := model.Target{IP: "1.1.1.1", Port: 80}

	whitelistSignal := evaluator.Evaluate(model.ProfileSnapshot{
		Found:           true,
		Target:          target,
		LastEventType:   model.EventRewriteSkipped,
		LastFirewallHit: true,
	})
	if whitelistSignal.Kind != model.SignalTryTargetOffload {
		t.Fatalf("expected whitelist snapshot to trigger offload, got %s", whitelistSignal.Kind)
	}

	nonHTTPSignal := evaluator.Evaluate(model.ProfileSnapshot{
		Found:                  true,
		Target:                 target,
		NonHTTPScore:           2,
		PendingTargetOffloadAt: time.Unix(90, 0),
	})
	if nonHTTPSignal.Kind != model.SignalTryTargetOffload {
		t.Fatalf("expected matured non-http snapshot to trigger offload, got %s", nonHTTPSignal.Kind)
	}

	noAction := evaluator.Evaluate(model.ProfileSnapshot{
		Found:                  true,
		Target:                 target,
		NonHTTPScore:           1,
		PendingTargetOffloadAt: time.Unix(200, 0),
	})
	if noAction.Kind != model.SignalNoAction {
		t.Fatalf("expected no action for immature snapshot, got %s", noAction.Kind)
	}
}

func TestEvaluateTargetKeepsWhitelistEnabledWhenNonHTTPBypassIsDisabled(t *testing.T) {
	t.Parallel()

	runtimeClock := manualclock.New(time.Unix(100, 0))
	evaluator := NewTargetPolicyWithClock(60, 3600, 2, false, runtimeClock)
	target := model.Target{IP: "1.1.1.1", Port: 80}

	whitelistSignal := evaluator.Evaluate(model.ProfileSnapshot{
		Found:           true,
		Target:          target,
		LastEventType:   model.EventRewriteSkipped,
		LastFirewallHit: true,
	})
	if whitelistSignal.Kind != model.SignalTryTargetOffload {
		t.Fatalf("expected whitelist offload to remain enabled, got %s", whitelistSignal.Kind)
	}

	nonHTTPSignal := evaluator.Evaluate(model.ProfileSnapshot{
		Found:                  true,
		Target:                 target,
		NonHTTPScore:           2,
		PendingTargetOffloadAt: runtimeClock.Now().Add(-time.Second),
	})
	if nonHTTPSignal.Kind != model.SignalNoAction {
		t.Fatalf("expected non-http offload to be disabled, got %s", nonHTTPSignal.Kind)
	}
}

func TestEvaluateSessionNonHTTPPassthrough(t *testing.T) {
	t.Parallel()

	evaluator := NewSessionPolicy()
	sessionState := &model.SessionState{}

	decision := evaluator.Evaluate(sessionState, model.SessionInput{
		Kind:     model.SessionInputClassification,
		Protocol: model.ProtocolNonHTTP,
	}, model.ProfileSnapshot{})

	if decision.Action != model.ActionPassthrough {
		t.Fatalf("expected passthrough action, got %s", decision.Action)
	}
	if strings.TrimSpace(decision.Reason) != "" {
		t.Fatalf("expected empty reason for non-http passthrough, got %q", decision.Reason)
	}
}
