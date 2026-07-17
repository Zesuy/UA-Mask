package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadJSONConfigNormalizesDurationsListsAndProfile(t *testing.T) {
	t.Parallel()

	path := writeConfigFile(t, `{
  "schema_version": 1,
  "listen": {
    "port": 12032,
    "dial_timeout": "15s"
  },
  "rewrite": {
    "user_agent": "MaskUA",
    "mode": "regex",
    "pattern": "Android|Linux",
    "partial_replace": true,
    "whitelist": [" KeepMe ", ""]
  },
  "firewall": {
    "backend": "nft",
    "ua_whitelist": ["Steam"],
    "whitelist_timeout": "12h",
    "non_http": {
      "enabled": true,
      "threshold": 7,
      "decision_delay": "45s",
      "http_cooldown": "30m",
      "timeout": "4h"
    }
  },
  "performance": {
    "profile": "Medium"
  }
}`)

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.Listen.Port != 12032 || cfg.Listen.DialTimeout != 15*time.Second {
		t.Fatalf("unexpected listen config: %+v", cfg.Listen)
	}
	if !cfg.Rewrite.EnableRegex || cfg.Rewrite.ForceReplace || cfg.RewriteRuntime.UARegexp == nil {
		t.Fatalf("expected compiled regex mode, got %+v", cfg.Rewrite)
	}
	if len(cfg.Rewrite.Whitelist) != 1 || cfg.Rewrite.Whitelist[0] != "KeepMe" {
		t.Fatalf("unexpected whitelist: %v", cfg.Rewrite.Whitelist)
	}
	if cfg.Firewall.Backend != "nft" || cfg.Firewall.Timeout != 4*3600 || cfg.Firewall.ImmediateBypassTimeout != 12*3600 {
		t.Fatalf("unexpected firewall config: %+v", cfg.Firewall)
	}
	if cfg.Performance.Profile != "medium" || cfg.Rewrite.CacheSize != 3000 || cfg.Listen.PoolSize != 500 {
		t.Fatalf("performance profile was not applied: %+v listen=%+v rewrite=%+v", cfg.Performance, cfg.Listen, cfg.Rewrite)
	}
}

func TestLoadJSONConfigRejectsUnknownFieldsAndMissingVersion(t *testing.T) {
	t.Parallel()

	unknown := writeConfigFile(t, `{"schema_version":1,"listen":{"unknown":true}}`)
	if _, err := LoadFile(unknown); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}

	missingVersion := writeConfigFile(t, `{"listen":{"port":12032}}`)
	if _, err := LoadFile(missingVersion); err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("expected schema version error, got %v", err)
	}
}

func TestLoadJSONConfigErrorsIncludePath(t *testing.T) {
	t.Parallel()

	path := writeConfigFile(t, `{"schema_version":1,"listen":{"port":0}}`)
	if _, err := LoadFile(path); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("expected config path in error, got %v", err)
	}
}

func TestLegacyFlagsOverrideJSONConfig(t *testing.T) {
	t.Parallel()

	path := writeConfigFile(t, `{
  "schema_version": 1,
  "listen": {"port": 12032},
  "rewrite": {"mode": "regex", "pattern": "Android"},
  "performance": {"profile": "high"}
}`)
	command, err := LoadFromArgs([]string{
		"-config", path,
		"-port", "18080",
		"-force",
		"-cache-size", "42",
		"-check-config",
	})
	if err != nil {
		t.Fatalf("LoadFromArgs: %v", err)
	}
	if !command.CheckConfig || command.ConfigPath != path {
		t.Fatalf("unexpected command: %+v", command)
	}
	if command.Config.Listen.Port != 18080 || command.Config.Rewrite.CacheSize != 42 {
		t.Fatalf("legacy overrides were not applied: %+v", command.Config)
	}
	if !command.Config.Rewrite.ForceReplace || command.Config.Rewrite.EnableRegex {
		t.Fatalf("force flag must override JSON rewrite mode: %+v", command.Config.Rewrite)
	}
	if command.Config.Performance.Profile != "custom" {
		t.Fatalf("tuning override must switch to custom profile: %+v", command.Config.Performance)
	}
}

func TestWriteEffectiveProducesReloadableJSON(t *testing.T) {
	t.Parallel()

	cfg, err := NewFromArgs([]string{"-port", "12345", "-keywords", "Android,Linux"})
	if err != nil {
		t.Fatalf("NewFromArgs: %v", err)
	}
	var output bytes.Buffer
	if err := WriteEffective(&output, cfg); err != nil {
		t.Fatalf("WriteEffective: %v", err)
	}
	path := filepath.Join(t.TempDir(), "effective.json")
	if err := os.WriteFile(path, output.Bytes(), 0o600); err != nil {
		t.Fatalf("write effective config: %v", err)
	}
	reloaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("reload effective config: %v\n%s", err, output.String())
	}
	if reloaded.Listen.Port != cfg.Listen.Port || strings.Join(reloaded.Rewrite.Keywords, ",") != "Android,Linux" {
		t.Fatalf("effective config changed after reload: original=%+v reloaded=%+v", cfg, reloaded)
	}
}

func TestJSONDurationMustUseWholeSecondsForFirewallTimeout(t *testing.T) {
	t.Parallel()

	path := writeConfigFile(t, `{
  "schema_version": 1,
  "firewall": {"non_http": {"timeout": "1500ms"}}
}`)
	if _, err := LoadFile(path); err == nil || !strings.Contains(err.Error(), "whole seconds") {
		t.Fatalf("expected whole-second validation error, got %v", err)
	}
}

func TestDocumentedExampleConfigLoads(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "..", "docs", "config.example.json")
	if _, err := LoadFile(path); err != nil {
		t.Fatalf("documented example config must remain valid: %v", err)
	}
}

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	return path
}
