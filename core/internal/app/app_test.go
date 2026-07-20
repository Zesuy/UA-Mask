package app

import (
	"runtime/debug"
	"testing"

	"UAmask/internal/config"
)

func TestApplyRuntimeTuning(t *testing.T) {
	original := debug.SetGCPercent(73)
	defer debug.SetGCPercent(original)

	t.Run("CFG-GOGC-001/flag-only launch inherits runtime setting", func(t *testing.T) {
		applyRuntimeTuning(&config.Command{Config: configWithGCPercent(25)})
		if got := currentGCPercent(); got != 73 {
			t.Fatalf("GC percent = %d, want inherited value 73", got)
		}
	})

	t.Run("CFG-GOGC-002/JSON launch applies configured setting", func(t *testing.T) {
		applyRuntimeTuning(&config.Command{
			Config:     configWithGCPercent(25),
			ConfigPath: "/tmp/UAmask/config.json",
		})
		if got := currentGCPercent(); got != 25 {
			t.Fatalf("GC percent = %d, want configured value 25", got)
		}
	})
}

func configWithGCPercent(percent int) *config.Config {
	cfg := config.DefaultConfig()
	cfg.Performance.GCPercent = percent
	return cfg
}

func currentGCPercent() int {
	current := debug.SetGCPercent(-1)
	debug.SetGCPercent(current)
	return current
}
