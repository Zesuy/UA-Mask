package rewrite

import (
	"regexp"
	"strings"

	"UAmask/internal/config"
)

type Engine struct {
	rewrite  config.RewriteConfig
	runtime  config.RewriteRuntimeConfig
	firewall config.FirewallConfig
}

func NewEngine(rewriteConfig config.RewriteConfig, runtimeConfig config.RewriteRuntimeConfig, firewallConfig config.FirewallConfig) *Engine {
	return &Engine{
		rewrite:  rewriteConfig,
		runtime:  runtimeConfig,
		firewall: firewallConfig,
	}
}

func (e *Engine) Evaluate(originUA string) Decision {
	if originUA == "" {
		return Decision{
			FinalUA:   originUA,
			Reason:    ReasonNoUserAgent,
			Cacheable: false,
		}
	}

	if e.containsFirewallWhitelist(originUA) {
		return Decision{
			Matched:           true,
			Replace:           false,
			FinalUA:           originUA,
			Reason:            ReasonFirewallWhitelist,
			Cacheable:         false,
			FirewallBypassHit: true,
			DropConnection:    e.firewall.DropOnMatch,
		}
	}

	if e.isUserAgentWhitelisted(originUA) {
		return Decision{
			Matched:   true,
			Replace:   false,
			FinalUA:   originUA,
			Reason:    ReasonWhitelist,
			Cacheable: true,
		}
	}

	if e.rewrite.ForceReplace {
		return Decision{
			Matched:   true,
			Replace:   true,
			FinalUA:   buildNewUA(originUA, e.rewrite.UserAgent, e.runtime.UARegexp, e.rewrite.EnablePartialReplace),
			Reason:    ReasonForceReplace,
			Cacheable: true,
		}
	}

	if e.rewrite.EnableRegex {
		if e.runtime.UARegexp != nil && e.runtime.UARegexp.MatchString(originUA) {
			return Decision{
				Matched:   true,
				Replace:   true,
				FinalUA:   buildNewUA(originUA, e.rewrite.UserAgent, e.runtime.UARegexp, e.rewrite.EnablePartialReplace),
				Reason:    ReasonRegexMatch,
				Cacheable: true,
			}
		}
		return Decision{
			Matched:   false,
			Replace:   false,
			FinalUA:   originUA,
			Reason:    ReasonRegexMiss,
			Cacheable: true,
		}
	}

	for _, keyword := range e.rewrite.Keywords {
		if strings.Contains(originUA, keyword) {
			return Decision{
				Matched:   true,
				Replace:   true,
				FinalUA:   buildNewUA(originUA, e.rewrite.UserAgent, e.runtime.UARegexp, e.rewrite.EnablePartialReplace),
				Reason:    ReasonKeywordMatch,
				Cacheable: true,
			}
		}
	}

	return Decision{
		Matched:   false,
		Replace:   false,
		FinalUA:   originUA,
		Reason:    ReasonKeywordMiss,
		Cacheable: true,
	}
}

func (e *Engine) containsFirewallWhitelist(originUA string) bool {
	for _, keyword := range e.firewall.UAWhitelist {
		if strings.Contains(originUA, keyword) {
			return true
		}
	}
	return false
}

func (e *Engine) isUserAgentWhitelisted(originUA string) bool {
	for _, whitelistUA := range e.rewrite.Whitelist {
		if whitelistUA == originUA {
			return true
		}
	}
	return false
}

func buildNewUA(originUA, replacementUA string, uaRegexp *regexp.Regexp, enablePartialReplace bool) string {
	if enablePartialReplace && uaRegexp != nil {
		return uaRegexp.ReplaceAllString(originUA, replacementUA)
	}
	return replacementUA
}
