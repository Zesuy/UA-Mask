package policy

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"UAmask/internal/config"
	"UAmask/internal/model"
	"UAmask/internal/rewrite"
)

func TestEvaluateSessionRewritesAndTracksCounters(t *testing.T) {
	t.Parallel()

	engine := rewrite.NewEngine(
		config.RewriteConfig{UserAgent: "MaskUA", ForceReplace: true},
		config.RewriteRuntimeConfig{},
		config.FirewallConfig{},
	)
	cache, err := rewrite.NewUACache(16)
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}

	evaluator := NewSessionPolicy(rewrite.NewRequestRewriter(engine, cache))
	sessionState := &model.SessionState{Target: model.Target{IP: "1.1.1.1", Port: 80}}
	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("User-Agent", "Original")

	decision := evaluator.Evaluate(sessionState, model.SessionInput{
		Kind:     model.SessionInputRequest,
		Protocol: model.ProtocolHTTP,
		Request:  req,
	}, model.ProfileSnapshot{})

	if decision.Action != model.ActionContinue {
		t.Fatalf("expected continue action, got %s", decision.Action)
	}
	if !decision.Modified {
		t.Fatalf("expected modified request")
	}
	if got := req.Header.Get("User-Agent"); got != "MaskUA" {
		t.Fatalf("expected rewritten UA, got %q", got)
	}
	if sessionState.ConsecutiveRewrite != 1 || sessionState.ConsecutiveNoRewrite != 0 {
		t.Fatalf("unexpected session counters: %+v", sessionState)
	}
}

func TestEvaluateTargetUsesSnapshotOnly(t *testing.T) {
	t.Parallel()

	originalNow := nowFunc
	nowFunc = func() time.Time { return time.Unix(100, 0) }
	defer func() { nowFunc = originalNow }()

	evaluator := NewTargetPolicy(60, 3600, 2)
	target := model.Target{IP: "1.1.1.1", Port: 80}

	whitelistSignal := evaluator.Evaluate(model.ProfileSnapshot{
		Found:         true,
		Target:        target,
		LastEventType: model.EventRewriteSkipped,
		LastReason:    string(rewrite.ReasonFirewallWhitelist),
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

func TestEvaluateSessionNonHTTPPassthrough(t *testing.T) {
	t.Parallel()

	evaluator := NewSessionPolicy(rewrite.NewRequestRewriter(
		rewrite.NewEngine(config.RewriteConfig{}, config.RewriteRuntimeConfig{}, config.FirewallConfig{}),
		&rewrite.UACache{},
	))
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
