package rewrite

import (
	"regexp"
	"testing"

	"UAmask/internal/config"
)

func TestEngineEvaluate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		engine *Engine
		ua     string
		want   Decision
	}{
		{
			name: "force replace",
			engine: NewEngine(
				config.RewriteConfig{UserAgent: "FFF", ForceReplace: true},
				config.RewriteRuntimeConfig{},
				config.FirewallConfig{},
			),
			ua: "Mozilla/5.0",
			want: Decision{
				Matched:   true,
				Replace:   true,
				FinalUA:   "FFF",
				Reason:    ReasonForceReplace,
				Cacheable: true,
			},
		},
		{
			name: "keyword replace",
			engine: NewEngine(
				config.RewriteConfig{UserAgent: "FFF", Keywords: []string{"Android"}},
				config.RewriteRuntimeConfig{},
				config.FirewallConfig{},
			),
			ua: "Mozilla Android",
			want: Decision{
				Matched:   true,
				Replace:   true,
				FinalUA:   "FFF",
				Reason:    ReasonKeywordMatch,
				Cacheable: true,
			},
		},
		{
			name: "regex partial replace",
			engine: NewEngine(
				config.RewriteConfig{UserAgent: "Mask", EnableRegex: true, EnablePartialReplace: true},
				config.RewriteRuntimeConfig{UARegexp: regexp.MustCompile("Android")},
				config.FirewallConfig{},
			),
			ua: "Mozilla Android",
			want: Decision{
				Matched:   true,
				Replace:   true,
				FinalUA:   "Mozilla Mask",
				Reason:    ReasonRegexMatch,
				Cacheable: true,
			},
		},
		{
			name: "firewall whitelist has highest priority",
			engine: NewEngine(
				config.RewriteConfig{UserAgent: "FFF", ForceReplace: true},
				config.RewriteRuntimeConfig{},
				config.FirewallConfig{UAWhitelist: []string{"Steam"}, DropOnMatch: true},
			),
			ua: "Steam Client",
			want: Decision{
				Matched:           true,
				Replace:           false,
				FinalUA:           "Steam Client",
				Reason:            ReasonFirewallWhitelist,
				Cacheable:         false,
				FirewallBypassHit: true,
				DropConnection:    true,
			},
		},
		{
			name: "exact whitelist pass through",
			engine: NewEngine(
				config.RewriteConfig{UserAgent: "FFF", Whitelist: []string{"UA-A"}},
				config.RewriteRuntimeConfig{},
				config.FirewallConfig{},
			),
			ua: "UA-A",
			want: Decision{
				Matched:   true,
				Replace:   false,
				FinalUA:   "UA-A",
				Reason:    ReasonWhitelist,
				Cacheable: true,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.engine.Evaluate(tt.ua)
			if got != tt.want {
				t.Fatalf("unexpected decision: got %+v want %+v", got, tt.want)
			}
		})
	}
}
