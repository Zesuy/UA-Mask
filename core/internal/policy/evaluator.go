package policy

import (
	"UAmask/internal/clock"
	"UAmask/internal/model"
)

type SessionEvaluator interface {
	Evaluate(session *model.SessionState, input model.SessionInput, snapshot model.ProfileSnapshot) model.PolicyDecision
}

type TargetEvaluator interface {
	Evaluate(snapshot model.ProfileSnapshot) model.OffloadSignal
}

type SessionPolicy struct{}

func NewSessionPolicy() *SessionPolicy {
	return &SessionPolicy{}
}

func (p *SessionPolicy) Evaluate(session *model.SessionState, input model.SessionInput, _ model.ProfileSnapshot) model.PolicyDecision {
	switch input.Kind {
	case model.SessionInputClassification:
		if input.Protocol == model.ProtocolNonHTTP {
			return model.PolicyDecision{
				Protocol:   model.ProtocolNonHTTP,
				Action:     model.ActionPassthrough,
				FlowSignal: model.OffloadSignal{Kind: model.SignalNoAction},
			}
		}
	case model.SessionInputRequest:
		if input.Request == nil {
			return model.PolicyDecision{
				Protocol:   model.ProtocolHTTP,
				Action:     model.ActionDrop,
				Reason:     "nil request",
				FlowSignal: model.OffloadSignal{Kind: model.SignalNoAction},
			}
		}

		decision := model.PolicyDecision{
			Protocol:    model.ProtocolHTTP,
			Action:      model.ActionContinue,
			Modified:    input.Modified,
			FromCache:   input.FromCache,
			Reason:      input.Reason,
			FirewallHit: input.FirewallHit,
			FlowSignal:  model.OffloadSignal{Kind: model.SignalNoAction},
		}
		if input.DropConnection {
			decision.Action = model.ActionDrop
		}

		return decision
	}

	return model.PolicyDecision{
		Action:     model.ActionContinue,
		FlowSignal: model.OffloadSignal{Kind: model.SignalNoAction},
	}
}

type TargetPolicy struct {
	targetOffloadTimeout    int
	whitelistOffloadTimeout int
	nonHTTPThreshold        int
	nonHTTPEnabled          bool
	clock                   clock.Clock
}

func NewTargetPolicy(targetOffloadTimeout int, whitelistOffloadTimeout int, nonHTTPThreshold int, nonHTTPEnabled bool) *TargetPolicy {
	return NewTargetPolicyWithClock(targetOffloadTimeout, whitelistOffloadTimeout, nonHTTPThreshold, nonHTTPEnabled, clock.RealClock{})
}

func NewTargetPolicyWithClock(targetOffloadTimeout int, whitelistOffloadTimeout int, nonHTTPThreshold int, nonHTTPEnabled bool, runtimeClock clock.Clock) *TargetPolicy {
	return &TargetPolicy{
		targetOffloadTimeout:    targetOffloadTimeout,
		whitelistOffloadTimeout: whitelistOffloadTimeout,
		nonHTTPThreshold:        nonHTTPThreshold,
		nonHTTPEnabled:          nonHTTPEnabled,
		clock:                   clock.OrReal(runtimeClock),
	}
}

func (p *TargetPolicy) Evaluate(snapshot model.ProfileSnapshot) model.OffloadSignal {
	if !snapshot.Found {
		return model.OffloadSignal{Kind: model.SignalNoAction}
	}

	now := p.clock.Now()
	if !snapshot.TargetOffloadUntil.IsZero() && now.Before(snapshot.TargetOffloadUntil) {
		return model.OffloadSignal{Kind: model.SignalNoAction}
	}

	if snapshot.LastEventType == model.EventRewriteSkipped && snapshot.LastFirewallHit {
		return model.OffloadSignal{
			Kind:    model.SignalTryTargetOffload,
			Target:  snapshot.Target,
			Timeout: p.whitelistOffloadTimeout,
			Reason:  snapshot.LastReason,
		}
	}

	if p.nonHTTPEnabled &&
		!snapshot.PendingTargetOffloadAt.IsZero() &&
		!now.Before(snapshot.PendingTargetOffloadAt) &&
		(snapshot.HTTPCooldownUntil.IsZero() || !now.Before(snapshot.HTTPCooldownUntil)) &&
		snapshot.NonHTTPScore >= p.nonHTTPThreshold {
		return model.OffloadSignal{
			Kind:    model.SignalTryTargetOffload,
			Target:  snapshot.Target,
			Timeout: p.targetOffloadTimeout,
			Reason:  "non-http threshold reached",
		}
	}

	return model.OffloadSignal{Kind: model.SignalNoAction}
}
