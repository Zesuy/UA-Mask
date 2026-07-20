package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type fileConfig struct {
	SchemaVersion int                   `json:"schema_version"`
	Listen        fileListenConfig      `json:"listen"`
	Rewrite       fileRewriteConfig     `json:"rewrite"`
	Firewall      fileFirewallConfig    `json:"firewall"`
	Observe       fileObserveConfig     `json:"observe"`
	Performance   filePerformanceConfig `json:"performance"`
}

type fileListenConfig struct {
	Port              int    `json:"port"`
	DialTimeout       string `json:"dial_timeout"`
	ClientKeepAlive   string `json:"client_keep_alive"`
	UpstreamKeepAlive string `json:"upstream_keep_alive"`
}

type fileRewriteConfig struct {
	UserAgent      string   `json:"user_agent"`
	Mode           string   `json:"mode"`
	Keywords       []string `json:"keywords"`
	Whitelist      []string `json:"whitelist"`
	Pattern        string   `json:"pattern"`
	PartialReplace bool     `json:"partial_replace"`
}

type fileFirewallConfig struct {
	Backend          string                    `json:"backend"`
	SetName          string                    `json:"set_name"`
	UAWhitelist      []string                  `json:"ua_whitelist"`
	DropOnMatch      bool                      `json:"drop_on_match"`
	WhitelistTimeout string                    `json:"whitelist_timeout"`
	NonHTTP          fileNonHTTPFirewallConfig `json:"non_http"`
}

type fileNonHTTPFirewallConfig struct {
	Enabled         bool   `json:"enabled"`
	Threshold       int    `json:"threshold"`
	DecisionDelay   string `json:"decision_delay"`
	HTTPCooldown    string `json:"http_cooldown"`
	Timeout         string `json:"timeout"`
	CleanupInterval string `json:"cleanup_interval"`
}

type fileObserveConfig struct {
	LogLevel      string `json:"log_level"`
	LogFile       string `json:"log_file"`
	StatsFilePath string `json:"stats_file"`
	StatsInterval string `json:"stats_interval"`
}

type filePerformanceConfig struct {
	Profile    string `json:"profile"`
	CacheSize  int    `json:"cache_size"`
	BufferSize int    `json:"buffer_size"`
	PoolSize   int    `json:"pool_size"`
	GCPercent  int    `json:"gc_percent"`
}

func LoadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %q: %w", path, err)
	}
	if err := validateJSONDocument(data, fileConfig{}); err != nil {
		return nil, fmt.Errorf("validate config file %q: %w", path, err)
	}

	raw := defaultFileConfig()
	raw.SchemaVersion = 0
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode config file %q: %w", path, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("decode config file %q: %w", path, err)
	}
	if raw.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("config file %q: unsupported schema_version %d, expected %d", path, raw.SchemaVersion, SchemaVersion)
	}

	cfg, err := raw.toConfig()
	if err != nil {
		return nil, fmt.Errorf("config file %q: %w", path, err)
	}
	if err := finalize(cfg); err != nil {
		return nil, fmt.Errorf("config file %q: %w", path, err)
	}
	return cfg, nil
}

func WriteEffective(w io.Writer, cfg *Config) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(effectiveFileConfig(cfg)); err != nil {
		return fmt.Errorf("encode effective config: %w", err)
	}
	return nil
}

func defaultFileConfig() fileConfig {
	return effectiveFileConfig(DefaultConfig())
}

func effectiveFileConfig(cfg *Config) fileConfig {
	return fileConfig{
		SchemaVersion: SchemaVersion,
		Listen: fileListenConfig{
			Port:              cfg.Listen.Port,
			DialTimeout:       cfg.Listen.DialTimeout.String(),
			ClientKeepAlive:   cfg.Listen.ClientKeepAlive.String(),
			UpstreamKeepAlive: cfg.Listen.UpstreamKeepAlive.String(),
		},
		Rewrite: fileRewriteConfig{
			UserAgent:      cfg.Rewrite.UserAgent,
			Mode:           rewriteMode(cfg),
			Keywords:       cloneList(cfg.Rewrite.Keywords),
			Whitelist:      cloneList(cfg.Rewrite.Whitelist),
			Pattern:        cfg.Rewrite.Pattern,
			PartialReplace: cfg.Rewrite.EnablePartialReplace,
		},
		Firewall: fileFirewallConfig{
			Backend:          cfg.Firewall.Backend,
			SetName:          cfg.Firewall.SetName,
			UAWhitelist:      cloneList(cfg.Firewall.UAWhitelist),
			DropOnMatch:      cfg.Firewall.DropOnMatch,
			WhitelistTimeout: secondsDuration(cfg.Firewall.ImmediateBypassTimeout).String(),
			NonHTTP: fileNonHTTPFirewallConfig{
				Enabled:         cfg.Firewall.EnableBypass,
				Threshold:       cfg.Firewall.NonHTTPThreshold,
				DecisionDelay:   cfg.Firewall.DecisionDelay.String(),
				HTTPCooldown:    cfg.Firewall.HTTPCooldownPeriod.String(),
				Timeout:         secondsDuration(cfg.Firewall.Timeout).String(),
				CleanupInterval: cfg.Firewall.ProfileCleanupInterval.String(),
			},
		},
		Observe: fileObserveConfig{
			LogLevel:      cfg.Observe.LogLevel,
			LogFile:       cfg.Observe.LogFile,
			StatsFilePath: cfg.Observe.StatsFilePath,
			StatsInterval: cfg.Observe.StatsInterval.String(),
		},
		Performance: filePerformanceConfig{
			Profile:    cfg.Performance.Profile,
			CacheSize:  cfg.Rewrite.CacheSize,
			BufferSize: cfg.Listen.BufferSize,
			PoolSize:   cfg.Listen.PoolSize,
			GCPercent:  cfg.Performance.GCPercent,
		},
	}
}

func (raw fileConfig) toConfig() (*Config, error) {
	dialTimeout, err := parseDuration("listen.dial_timeout", raw.Listen.DialTimeout)
	if err != nil {
		return nil, err
	}
	clientKeepAlive, err := parseDuration("listen.client_keep_alive", raw.Listen.ClientKeepAlive)
	if err != nil {
		return nil, err
	}
	upstreamKeepAlive, err := parseDuration("listen.upstream_keep_alive", raw.Listen.UpstreamKeepAlive)
	if err != nil {
		return nil, err
	}
	decisionDelay, err := parseDuration("firewall.non_http.decision_delay", raw.Firewall.NonHTTP.DecisionDelay)
	if err != nil {
		return nil, err
	}
	httpCooldown, err := parseDuration("firewall.non_http.http_cooldown", raw.Firewall.NonHTTP.HTTPCooldown)
	if err != nil {
		return nil, err
	}
	cleanupInterval, err := parseDuration("firewall.non_http.cleanup_interval", raw.Firewall.NonHTTP.CleanupInterval)
	if err != nil {
		return nil, err
	}
	statsInterval, err := parseDuration("observe.stats_interval", raw.Observe.StatsInterval)
	if err != nil {
		return nil, err
	}
	targetTimeout, err := parseSeconds("firewall.non_http.timeout", raw.Firewall.NonHTTP.Timeout)
	if err != nil {
		return nil, err
	}
	whitelistTimeout, err := parseSeconds("firewall.whitelist_timeout", raw.Firewall.WhitelistTimeout)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Listen: ListenConfig{
			Port:              raw.Listen.Port,
			DialTimeout:       dialTimeout,
			ClientKeepAlive:   clientKeepAlive,
			UpstreamKeepAlive: upstreamKeepAlive,
		},
		Rewrite: RewriteConfig{
			UserAgent:            raw.Rewrite.UserAgent,
			Whitelist:            normalizeList(raw.Rewrite.Whitelist),
			EnablePartialReplace: raw.Rewrite.PartialReplace,
			Keywords:             normalizeList(raw.Rewrite.Keywords),
			Pattern:              raw.Rewrite.Pattern,
		},
		Firewall: FirewallConfig{
			UAWhitelist:            normalizeList(raw.Firewall.UAWhitelist),
			EnableBypass:           raw.Firewall.NonHTTP.Enabled,
			SetName:                raw.Firewall.SetName,
			Backend:                strings.ToLower(raw.Firewall.Backend),
			DropOnMatch:            raw.Firewall.DropOnMatch,
			NonHTTPThreshold:       raw.Firewall.NonHTTP.Threshold,
			Timeout:                targetTimeout,
			DecisionDelay:          decisionDelay,
			HTTPCooldownPeriod:     httpCooldown,
			ProfileCleanupInterval: cleanupInterval,
			ImmediateBypassTimeout: whitelistTimeout,
		},
		Observe: ObserveConfig{
			LogLevel:      strings.ToLower(raw.Observe.LogLevel),
			LogFile:       raw.Observe.LogFile,
			StatsFilePath: raw.Observe.StatsFilePath,
			StatsInterval: statsInterval,
		},
		Performance: PerformanceConfig{
			Profile:   normalizeProfile(raw.Performance.Profile),
			GCPercent: raw.Performance.GCPercent,
		},
	}

	if err := applyRewriteMode(cfg, raw.Rewrite.Mode); err != nil {
		return nil, err
	}
	if err := applyPerformanceProfile(cfg, raw.Performance); err != nil {
		return nil, err
	}
	return cfg, nil
}

func applyRewriteMode(cfg *Config, mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "keywords":
		cfg.Rewrite.ForceReplace = false
		cfg.Rewrite.EnableRegex = false
	case "regex":
		cfg.Rewrite.ForceReplace = false
		cfg.Rewrite.EnableRegex = true
	case "all":
		cfg.Rewrite.ForceReplace = true
		cfg.Rewrite.EnableRegex = false
	default:
		return fmt.Errorf("invalid rewrite mode %q", mode)
	}
	return nil
}

func applyPerformanceProfile(cfg *Config, performance filePerformanceConfig) error {
	profile := normalizeProfile(performance.Profile)
	cfg.Performance.Profile = profile
	switch profile {
	case "low":
		cfg.Rewrite.CacheSize = 2000
		cfg.Listen.BufferSize = 8192
		cfg.Listen.PoolSize = 200
		cfg.Performance.GCPercent = 100
	case "medium":
		cfg.Rewrite.CacheSize = 3000
		cfg.Listen.BufferSize = 8192
		cfg.Listen.PoolSize = 500
		cfg.Performance.GCPercent = 100
	case "high":
		cfg.Rewrite.CacheSize = 5000
		cfg.Listen.BufferSize = 8192
		cfg.Listen.PoolSize = 1000
		cfg.Performance.GCPercent = 100
	case "custom":
		cfg.Rewrite.CacheSize = performance.CacheSize
		cfg.Listen.BufferSize = performance.BufferSize
		cfg.Listen.PoolSize = performance.PoolSize
		cfg.Performance.GCPercent = performance.GCPercent
	default:
		return fmt.Errorf("invalid performance profile %q", performance.Profile)
	}
	return nil
}

func normalizeProfile(profile string) string {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "low":
		return "low"
	case "medium":
		return "medium"
	case "high", "high_throughput":
		return "high"
	case "custom", "":
		return "custom"
	default:
		return strings.ToLower(strings.TrimSpace(profile))
	}
}

func rewriteMode(cfg *Config) string {
	if cfg.Rewrite.ForceReplace {
		return "all"
	}
	if cfg.Rewrite.EnableRegex {
		return "regex"
	}
	return "keywords"
}

func parseDuration(field, value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s duration %q: %w", field, value, err)
	}
	return duration, nil
}

func parseSeconds(field, value string) (int, error) {
	duration, err := parseDuration(field, value)
	if err != nil {
		return 0, err
	}
	if duration%time.Second != 0 {
		return 0, fmt.Errorf("%s must use whole seconds: %q", field, value)
	}
	seconds := duration / time.Second
	maxInt := int64(^uint(0) >> 1)
	if int64(seconds) > maxInt || seconds < 0 {
		return 0, fmt.Errorf("%s is out of range: %q", field, value)
	}
	return int(seconds), nil
}

func secondsDuration(seconds int) time.Duration {
	return time.Duration(seconds) * time.Second
}

func normalizeList(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func cloneList(values []string) []string {
	result := make([]string, len(values))
	copy(result, values)
	return result
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("config file contains multiple JSON values")
		}
		return fmt.Errorf("decode trailing config data: %w", err)
	}
	return nil
}
