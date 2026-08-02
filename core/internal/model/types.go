package model

import (
	"net"
	"net/http"
	"time"
)

type Target struct {
	Address string
	IP      string
	Port    int
}

type IngressKind string

const (
	IngressKindRedirect IngressKind = "redirect"
)

type AcceptedSession struct {
	ClientConn  *net.TCPConn
	Target      Target
	IngressKind IngressKind
	Metadata    map[string]string
}

type ProtocolKind string

const (
	ProtocolUnknown ProtocolKind = "unknown"
	ProtocolHTTP    ProtocolKind = "http"
	ProtocolNonHTTP ProtocolKind = "non_http"
)

type FlowOffloadStatus string

const (
	FlowOffloadNone   FlowOffloadStatus = "none"
	FlowOffloadActive FlowOffloadStatus = "active"
	FlowOffloadFailed FlowOffloadStatus = "failed"
)

type SessionState struct {
	ID                   uint64
	IngressKind          IngressKind
	ClientAddr           string
	UpstreamAddr         string
	Target               Target
	Protocol             ProtocolKind
	ConsecutiveRewrite   int
	ConsecutiveNoRewrite int
	FlowOffloadStatus    FlowOffloadStatus
	OpenedAt             time.Time
}

type SessionEventType string

const (
	EventSessionOpened          SessionEventType = "session_opened"
	EventUpstreamConnected      SessionEventType = "upstream_connected"
	EventProtocolClassified     SessionEventType = "protocol_classified"
	EventHTTPRequestObserved    SessionEventType = "http_request_observed"
	EventRewriteApplied         SessionEventType = "rewrite_applied"
	EventRewriteSkipped         SessionEventType = "rewrite_skipped"
	EventNonHTTPObserved        SessionEventType = "non_http_observed"
	EventSessionForwardFallback SessionEventType = "session_forward_fallback"
	EventSessionClosed          SessionEventType = "session_closed"
	EventFlowOffloadSucceeded   SessionEventType = "flow_offload_succeeded"
	EventFlowOffloadFailed      SessionEventType = "flow_offload_failed"
	EventTargetOffloadSucceeded SessionEventType = "target_offload_succeeded"
	EventTargetOffloadFailed    SessionEventType = "target_offload_failed"
)

type SessionEvent struct {
	Type        SessionEventType
	SessionID   uint64
	Target      Target
	IngressKind IngressKind
	Protocol    ProtocolKind
	Reason      string
	Modified    bool
	FromCache   bool
	FirewallHit bool
	Timeout     int
	Timestamp   time.Time
}

type TargetProfile struct {
	Target                 Target
	NonHTTPScore           int
	HTTPCooldownUntil      time.Time
	LastActivity           time.Time
	PendingTargetOffloadAt time.Time
	TargetOffloadUntil     time.Time
	FirewallWhitelistHits  int
	LastFirewallHit        bool
	LastEventType          SessionEventType
	LastReason             string
}

type ProfileSnapshot struct {
	Found                  bool
	Target                 Target
	NonHTTPScore           int
	HTTPCooldownUntil      time.Time
	LastActivity           time.Time
	PendingTargetOffloadAt time.Time
	TargetOffloadUntil     time.Time
	FirewallWhitelistHits  int
	LastFirewallHit        bool
	LastEventType          SessionEventType
	LastReason             string
}

type SessionInputKind string

const (
	SessionInputClassification SessionInputKind = "classification"
	SessionInputRequest        SessionInputKind = "request"
)

type SessionInput struct {
	Kind           SessionInputKind
	Protocol       ProtocolKind
	Request        *http.Request
	Modified       bool
	FromCache      bool
	Reason         string
	FirewallHit    bool
	DropConnection bool
}

type ActionKind string

const (
	ActionContinue    ActionKind = "continue"
	ActionPassthrough ActionKind = "passthrough"
	ActionDrop        ActionKind = "drop"
)

type OffloadSignalKind string

const (
	SignalNoAction            OffloadSignalKind = "no_action"
	SignalTryFlowOffload      OffloadSignalKind = "try_flow_offload"
	SignalTryTargetOffload    OffloadSignalKind = "try_target_offload"
	SignalCancelTargetOffload OffloadSignalKind = "cancel_target_offload"
)

type OffloadSignal struct {
	Kind      OffloadSignalKind
	SessionID uint64
	Target    Target
	Timeout   int
	Reason    string
}

type PolicyDecision struct {
	Protocol    ProtocolKind
	Action      ActionKind
	Modified    bool
	FromCache   bool
	Reason      string
	FirewallHit bool
	FlowSignal  OffloadSignal
}
