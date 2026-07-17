package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"
)

var validSetName = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

func finalize(cfg *Config) error {
	cfg.Rewrite.Whitelist = normalizeList(cfg.Rewrite.Whitelist)
	cfg.Rewrite.Keywords = normalizeList(cfg.Rewrite.Keywords)
	cfg.Firewall.UAWhitelist = normalizeList(cfg.Firewall.UAWhitelist)
	if err := validate(cfg); err != nil {
		return err
	}
	cfg.RewriteRuntime = RewriteRuntimeConfig{}
	if cfg.Rewrite.EnableRegex && !cfg.Rewrite.ForceReplace {
		compiled, err := regexp.Compile("(?i)" + cfg.Rewrite.Pattern)
		if err != nil {
			return fmt.Errorf("invalid User-Agent regex pattern: %w", err)
		}
		cfg.RewriteRuntime.UARegexp = compiled
	}
	return nil
}

func validate(cfg *Config) error {
	if cfg.Listen.Port < 1 || cfg.Listen.Port > 65535 {
		return fmt.Errorf("invalid port: %d", cfg.Listen.Port)
	}
	if cfg.Listen.BufferSize < 1024 || cfg.Listen.BufferSize > 65536 {
		return fmt.Errorf("invalid buffer size: %d", cfg.Listen.BufferSize)
	}
	if cfg.Listen.PoolSize < 0 {
		return fmt.Errorf("invalid pool size: %d", cfg.Listen.PoolSize)
	}
	if cfg.Listen.DialTimeout <= 0 {
		return fmt.Errorf("invalid dial timeout: %s", cfg.Listen.DialTimeout)
	}
	if cfg.Listen.ClientKeepAlive < 0 || cfg.Listen.UpstreamKeepAlive < 0 {
		return fmt.Errorf("keepalive durations cannot be negative")
	}
	if cfg.Rewrite.CacheSize < 0 {
		return fmt.Errorf("invalid cache size: %d", cfg.Rewrite.CacheSize)
	}
	if cfg.Rewrite.EnableRegex && !cfg.Rewrite.ForceReplace && strings.TrimSpace(cfg.Rewrite.Pattern) == "" {
		return fmt.Errorf("regex mode requires a pattern")
	}
	if cfg.Firewall.Backend != "ipt" && cfg.Firewall.Backend != "nft" {
		return fmt.Errorf("invalid firewall type: %s", cfg.Firewall.Backend)
	}
	if !validSetName.MatchString(cfg.Firewall.SetName) {
		return fmt.Errorf("invalid firewall set name: %s", cfg.Firewall.SetName)
	}
	if cfg.Firewall.NonHTTPThreshold < 1 {
		return fmt.Errorf("invalid non-http threshold: %d", cfg.Firewall.NonHTTPThreshold)
	}
	if cfg.Firewall.Timeout <= 0 || cfg.Firewall.ImmediateBypassTimeout <= 0 {
		return fmt.Errorf("firewall timeouts must be positive")
	}
	if cfg.Firewall.DecisionDelay < 0 || cfg.Firewall.HTTPCooldownPeriod < 0 {
		return fmt.Errorf("firewall decision durations cannot be negative")
	}
	if cfg.Firewall.ProfileCleanupInterval <= 0 {
		return fmt.Errorf("invalid profile cleanup interval: %s", cfg.Firewall.ProfileCleanupInterval)
	}
	if cfg.Observe.StatsInterval <= 0 {
		return fmt.Errorf("invalid stats interval: %s", cfg.Observe.StatsInterval)
	}
	if _, err := logrus.ParseLevel(cfg.Observe.LogLevel); err != nil {
		return fmt.Errorf("invalid log level %q", cfg.Observe.LogLevel)
	}
	if cfg.Performance.GCPercent < -1 {
		return fmt.Errorf("invalid GC percent: %d", cfg.Performance.GCPercent)
	}
	return nil
}
