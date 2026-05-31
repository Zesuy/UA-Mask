package policy

import (
	"time"

	"UAmask/internal/model"
	"UAmask/internal/rewrite"
)

type SessionEvaluator interface {
	Evaluate(session *model.SessionState, input model.SessionInput, snapshot model.ProfileSnapshot) model.PolicyDecision
}

type TargetEvaluator interface {
	Evaluate(snapshot model.ProfileSnapshot) model.OffloadSignal
}

type SessionPolicy struct {
	rewriter *rewrite.RequestRewriter
}

func NewSessionPolicy(rewriter *rewrite.RequestRewriter) *SessionPolicy {
	return &SessionPolicy{rewriter: rewriter}
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

		result := p.rewriter.Rewrite(input.Request, session.Target.IP, session.Target.Port)
		decision := model.PolicyDecision{
			Protocol:   model.ProtocolHTTP,
			Action:     model.ActionContinue,
			Modified:   result.Modified,
			FromCache:  result.FromCache,
			Reason:     string(result.Decision.Reason),
			FlowSignal: model.OffloadSignal{Kind: model.SignalNoAction},
		}
		if result.Decision.DropConnection {
			decision.Action = model.ActionDrop
		}

		if result.Modified {
			session.ConsecutiveRewrite++
			session.ConsecutiveNoRewrite = 0
		} else {
			session.ConsecutiveNoRewrite++
			session.ConsecutiveRewrite = 0
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
}

func NewTargetPolicy(targetOffloadTimeout int, whitelistOffloadTimeout int, nonHTTPThreshold int) *TargetPolicy {
	return &TargetPolicy{
		targetOffloadTimeout:    targetOffloadTimeout,
		whitelistOffloadTimeout: whitelistOffloadTimeout,
		nonHTTPThreshold:        nonHTTPThreshold,
	}
}

func (p *TargetPolicy) Evaluate(snapshot model.ProfileSnapshot) model.OffloadSignal {
	if !snapshot.Found {
		return model.OffloadSignal{Kind: model.SignalNoAction}
	}

	now := nowFunc()
	if !snapshot.TargetOffloadUntil.IsZero() && now.Before(snapshot.TargetOffloadUntil) {
		return model.OffloadSignal{Kind: model.SignalNoAction}
	}

	if snapshot.LastEventType == model.EventRewriteSkipped && snapshot.LastReason == string(rewrite.ReasonFirewallWhitelist) {
		return model.OffloadSignal{
			Kind:    model.SignalTryTargetOffload,
			Target:  snapshot.Target,
			Timeout: p.whitelistOffloadTimeout,
			Reason:  snapshot.LastReason,
		}
	}

	if !snapshot.PendingTargetOffloadAt.IsZero() &&
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

var nowFunc = func() time.Time {
	return time.Now()
}
