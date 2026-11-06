package config

import (
	"testing"
	"time"
)

// TestIdleTimeoutLegacyFlag 覆盖 -idle-timeout 的 CLI 参数路径。
//
// 背景：`idle_timeout` 最初只支持 JSON 配置文件（listen.idle_timeout）。
// 但 OpenWrt 的 procd init 脚本历史上用 CLI 参数组装启动命令
// （-p / -cache-size / ...），走的是 applyLegacyOverrides 分支，
// 不读 JSON，导致该选项在 OpenWrt 上实际不可用。本测试锁定 CLI 路径。
func TestIdleTimeoutLegacyFlag(t *testing.T) {
	cfg, err := NewFromArgs([]string{"-idle-timeout", "90s"})
	if err != nil {
		t.Fatalf("load args: %v", err)
	}
	if got := cfg.Listen.IdleTimeout; got != 90*time.Second {
		t.Fatalf("idle timeout = %v, want 90s", got)
	}
}

// TestIdleTimeoutLegacyFlagAbsentKeepsDefault CLI 未传参时应保留默认值，
// 不能把默认值覆盖成 0（0 表示禁用空闲超时）。
func TestIdleTimeoutLegacyFlagAbsentKeepsDefault(t *testing.T) {
	cfg, err := NewFromArgs([]string{"-p", "200"})
	if err != nil {
		t.Fatalf("load args: %v", err)
	}
	if got := cfg.Listen.IdleTimeout; got != defaultIdleTimeout {
		t.Fatalf("idle timeout = %v, want default %v", got, defaultIdleTimeout)
	}
}
