package rewrite

type Reason string

const (
	ReasonNoUserAgent       Reason = "No User-Agent Header"
	ReasonFirewallWhitelist Reason = "Hit Firewall UA Whitelist"
	ReasonWhitelist         Reason = "Hit User-Agent Whitelist"
	ReasonForceReplace      Reason = "Force Replace Mode"
	ReasonRegexMatch        Reason = "Hit User-Agent Pattern"
	ReasonRegexMiss         Reason = "Not Hit User-Agent Pattern"
	ReasonKeywordMatch      Reason = "Hit User-Agent Keyword"
	ReasonKeywordMiss       Reason = "Not Hit User-Agent Keywords"
)

type Decision struct {
	Matched           bool
	Replace           bool
	FinalUA           string
	Reason            Reason
	Cacheable         bool
	FirewallBypassHit bool
	DropConnection    bool
}
