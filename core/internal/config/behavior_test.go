package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPerformanceProfileMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		profile    string
		cacheSize  int
		bufferSize int
		poolSize   int
		gcPercent  int
		wantError  string
	}{
		{name: "CFG-PROFILE-001/low", profile: "low", cacheSize: 2000, bufferSize: 8192, poolSize: 200, gcPercent: 100},
		{name: "CFG-PROFILE-002/medium", profile: "Medium", cacheSize: 3000, bufferSize: 8192, poolSize: 500, gcPercent: 100},
		{name: "CFG-PROFILE-003/high", profile: "HIGH", cacheSize: 5000, bufferSize: 8192, poolSize: 1000, gcPercent: 100},
		{name: "CFG-PROFILE-004/high throughput alias", profile: "high_throughput", cacheSize: 5000, bufferSize: 8192, poolSize: 1000, gcPercent: 100},
		{name: "CFG-PROFILE-005/custom", profile: "custom", cacheSize: 71, bufferSize: 4096, poolSize: 9, gcPercent: 80},
		{name: "CFG-PROFILE-006/invalid", profile: "turbo", wantError: `invalid performance profile "turbo"`},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := writeConfigFile(t, fmt.Sprintf(`{
  "schema_version": 1,
  "performance": {
    "profile": %q,
    "cache_size": 71,
    "buffer_size": 4096,
    "pool_size": 9,
    "gc_percent": 80
  }
}`, test.profile))
			cfg, err := LoadFile(path)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("LoadFile() error = %v, want substring %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadFile(): %v", err)
			}
			if cfg.Rewrite.CacheSize != test.cacheSize || cfg.Listen.BufferSize != test.bufferSize ||
				cfg.Listen.PoolSize != test.poolSize || cfg.Performance.GCPercent != test.gcPercent {
				t.Fatalf("profile %q resolved to rewrite=%+v listen=%+v performance=%+v", test.profile, cfg.Rewrite, cfg.Listen, cfg.Performance)
			}
		})
	}
}

func TestCustomProfileFixture(t *testing.T) {
	t.Parallel()

	path := filepath.Join("testdata", "valid", "schema-v1-custom-profile.json")
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile(%q): %v", path, err)
	}
	if cfg.Performance.Profile != "custom" || cfg.Rewrite.CacheSize != 42 ||
		cfg.Listen.BufferSize != 4096 || cfg.Listen.PoolSize != 3 || cfg.Performance.GCPercent != -1 {
		t.Fatalf("unexpected custom profile: rewrite=%+v listen=%+v performance=%+v", cfg.Rewrite, cfg.Listen, cfg.Performance)
	}
}

func TestLegacyFlagOverrideMatrix(t *testing.T) {
	t.Parallel()

	fixture := filepath.Join("testdata", "valid", "schema-v1-full.json")
	tests := []struct {
		name  string
		args  []string
		check func(*testing.T, *Config)
	}{
		{name: "CFG-LEGACY-001/user agent", args: []string{"-u", "LegacyUA"}, check: func(t *testing.T, cfg *Config) { assertEqual(t, cfg.Rewrite.UserAgent, "LegacyUA") }},
		{name: "CFG-LEGACY-002/port", args: []string{"-port", "18080"}, check: func(t *testing.T, cfg *Config) { assertEqual(t, cfg.Listen.Port, 18080) }},
		{name: "CFG-LEGACY-003/log level", args: []string{"-loglevel", "DEBUG"}, check: func(t *testing.T, cfg *Config) { assertEqual(t, cfg.Observe.LogLevel, "debug") }},
		{name: "CFG-LEGACY-004/log file", args: []string{"-log", "/tmp/legacy.log"}, check: func(t *testing.T, cfg *Config) { assertEqual(t, cfg.Observe.LogFile, "/tmp/legacy.log") }},
		{name: "CFG-LEGACY-005/whitelist", args: []string{"-w", "Client A, Client B"}, check: func(t *testing.T, cfg *Config) { assertStringList(t, cfg.Rewrite.Whitelist, "Client A", "Client B") }},
		{name: "CFG-LEGACY-006/force", args: []string{"-force"}, check: func(t *testing.T, cfg *Config) {
			assertEqual(t, cfg.Rewrite.ForceReplace, true)
			assertEqual(t, cfg.Rewrite.EnableRegex, false)
		}},
		{name: "CFG-LEGACY-007/regex", args: []string{"-enable-regex"}, check: func(t *testing.T, cfg *Config) {
			assertEqual(t, cfg.Rewrite.EnableRegex, true)
			assertEqual(t, cfg.Rewrite.ForceReplace, false)
		}},
		{name: "CFG-LEGACY-008/partial replace", args: []string{"-s"}, check: func(t *testing.T, cfg *Config) { assertEqual(t, cfg.Rewrite.EnablePartialReplace, true) }},
		{name: "CFG-LEGACY-009/keywords", args: []string{"-keywords", "Android,Linux"}, check: func(t *testing.T, cfg *Config) { assertStringList(t, cfg.Rewrite.Keywords, "Android", "Linux") }},
		{name: "CFG-LEGACY-010/pattern", args: []string{"-r", "Android|Linux"}, check: func(t *testing.T, cfg *Config) { assertEqual(t, cfg.Rewrite.Pattern, "Android|Linux") }},
		{name: "CFG-LEGACY-011/cache size", args: []string{"-cache-size", "55"}, check: func(t *testing.T, cfg *Config) {
			assertEqual(t, cfg.Rewrite.CacheSize, 55)
			assertEqual(t, cfg.Performance.Profile, "custom")
		}},
		{name: "CFG-LEGACY-012/buffer size", args: []string{"-buffer-size", "2048"}, check: func(t *testing.T, cfg *Config) {
			assertEqual(t, cfg.Listen.BufferSize, 2048)
			assertEqual(t, cfg.Performance.Profile, "custom")
		}},
		{name: "CFG-LEGACY-013/pool size", args: []string{"-p", "3"}, check: func(t *testing.T, cfg *Config) {
			assertEqual(t, cfg.Listen.PoolSize, 3)
			assertEqual(t, cfg.Performance.Profile, "custom")
		}},
		{name: "CFG-LEGACY-014/firewall UA whitelist", args: []string{"-fw-ua-w", "Steam,Valve"}, check: func(t *testing.T, cfg *Config) { assertStringList(t, cfg.Firewall.UAWhitelist, "Steam", "Valve") }},
		{name: "CFG-LEGACY-015/firewall bypass", args: []string{"-fw-bypass"}, check: func(t *testing.T, cfg *Config) { assertEqual(t, cfg.Firewall.EnableBypass, true) }},
		{name: "CFG-LEGACY-016/firewall set name", args: []string{"-fw-set-name", "legacy_set"}, check: func(t *testing.T, cfg *Config) { assertEqual(t, cfg.Firewall.SetName, "legacy_set") }},
		{name: "CFG-LEGACY-017/firewall backend", args: []string{"-fw-type", "IPT"}, check: func(t *testing.T, cfg *Config) { assertEqual(t, cfg.Firewall.Backend, "ipt") }},
		{name: "CFG-LEGACY-018/firewall drop", args: []string{"-fw-drop"}, check: func(t *testing.T, cfg *Config) { assertEqual(t, cfg.Firewall.DropOnMatch, true) }},
		{name: "CFG-LEGACY-019/non-HTTP threshold", args: []string{"-fw-nonhttp-threshold", "9"}, check: func(t *testing.T, cfg *Config) { assertEqual(t, cfg.Firewall.NonHTTPThreshold, 9) }},
		{name: "CFG-LEGACY-020/firewall timeout", args: []string{"-fw-timeout", "90"}, check: func(t *testing.T, cfg *Config) { assertEqual(t, cfg.Firewall.Timeout, 90) }},
		{name: "CFG-LEGACY-021/decision delay", args: []string{"-fw-decision-delay", "12s"}, check: func(t *testing.T, cfg *Config) { assertEqual(t, cfg.Firewall.DecisionDelay, 12*time.Second) }},
		{name: "CFG-LEGACY-022/HTTP cooldown", args: []string{"-fw-http-cooldown", "45m"}, check: func(t *testing.T, cfg *Config) { assertEqual(t, cfg.Firewall.HTTPCooldownPeriod, 45*time.Minute) }},
		{name: "CFG-LEGACY-023/stats file", args: []string{"-stats-file", "/tmp/legacy.stats"}, check: func(t *testing.T, cfg *Config) { assertEqual(t, cfg.Observe.StatsFilePath, "/tmp/legacy.stats") }},
		{name: "CFG-LEGACY-024/stats interval", args: []string{"-stats-interval", "11s"}, check: func(t *testing.T, cfg *Config) { assertEqual(t, cfg.Observe.StatsInterval, 11*time.Second) }},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			args := append([]string{"-config", fixture}, test.args...)
			command, err := LoadFromArgs(args)
			if err != nil {
				t.Fatalf("LoadFromArgs(%v): %v", args, err)
			}
			test.check(t, command.Config)
		})
	}
}

func TestLegacyBooleanFlagsCanDisableJSONValues(t *testing.T) {
	t.Parallel()

	path := writeConfigFile(t, `{
  "schema_version": 1,
  "rewrite": {"mode": "all", "partial_replace": true},
  "firewall": {"drop_on_match": true, "non_http": {"enabled": true}}
}`)
	command, err := LoadFromArgs([]string{
		"-config", path,
		"-force=false",
		"-s=false",
		"-fw-bypass=false",
		"-fw-drop=false",
	})
	if err != nil {
		t.Fatalf("LoadFromArgs(): %v", err)
	}
	if command.Config.Rewrite.ForceReplace || command.Config.Rewrite.EnablePartialReplace ||
		command.Config.Firewall.EnableBypass || command.Config.Firewall.DropOnMatch {
		t.Fatalf("explicit false flags were not applied: %+v", command.Config)
	}
}

func TestLegacyRewriteModePrecedence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mode      string
		args      []string
		wantForce bool
		wantRegex bool
	}{
		{name: "CFG-LEGACY-025/force overrides JSON regex", mode: "regex", args: []string{"-force"}, wantForce: true},
		{name: "CFG-LEGACY-026/regex overrides JSON force", mode: "all", args: []string{"-enable-regex"}, wantRegex: true},
		{name: "CFG-LEGACY-027/force wins conflicting flags", mode: "keywords", args: []string{"-enable-regex", "-force"}, wantForce: true},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := writeConfigFile(t, fmt.Sprintf(`{"schema_version":1,"rewrite":{"mode":%q}}`, test.mode))
			command, err := LoadFromArgs(append([]string{"-config", path}, test.args...))
			if err != nil {
				t.Fatalf("LoadFromArgs(): %v", err)
			}
			if command.Config.Rewrite.ForceReplace != test.wantForce || command.Config.Rewrite.EnableRegex != test.wantRegex {
				t.Fatalf("rewrite mode = force:%t regex:%t, want force:%t regex:%t", command.Config.Rewrite.ForceReplace, command.Config.Rewrite.EnableRegex, test.wantForce, test.wantRegex)
			}
		})
	}
}

func TestEffectiveConfigGoldenFixtures(t *testing.T) {
	t.Parallel()

	fixtures := []string{"schema-v1-minimal", "schema-v1-full"}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run("CFG-GOLDEN/"+fixture, func(t *testing.T) {
			t.Parallel()
			inputPath := filepath.Join("testdata", "valid", fixture+".json")
			goldenPath := filepath.Join("testdata", "effective", fixture+".golden.json")
			cfg, err := LoadFile(inputPath)
			if err != nil {
				t.Fatalf("LoadFile(%q): %v", inputPath, err)
			}
			var output bytes.Buffer
			if err := WriteEffective(&output, cfg); err != nil {
				t.Fatalf("WriteEffective(): %v", err)
			}
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(goldenPath, output.Bytes(), 0o644); err != nil {
					t.Fatalf("update golden %q: %v", goldenPath, err)
				}
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden %q: %v", goldenPath, err)
			}
			if !bytes.Equal(output.Bytes(), want) {
				t.Fatalf("effective config differs from %s\n--- got ---\n%s\n--- want ---\n%s", goldenPath, output.Bytes(), want)
			}
		})
	}
}

func assertEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func assertStringList(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("got list %q, want %q", got, want)
	}
}
