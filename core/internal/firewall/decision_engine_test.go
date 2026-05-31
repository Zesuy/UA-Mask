package firewall

import (
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

type targetCollector struct {
	mu      sync.Mutex
	targets []BypassTarget
}

func (c *targetCollector) Enqueue(target BypassTarget) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.targets = append(c.targets, target)
}

func (c *targetCollector) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.targets)
}

func TestDecisionEngineTriggersBypass(t *testing.T) {
	t.Parallel()

	collector := &targetCollector{}
	engine := NewDecisionEngine(logrus.New(), 8, DecisionConfig{
		NonHTTPThreshold:       2,
		HTTPCooldownPeriod:     20 * time.Millisecond,
		DecisionDelay:          15 * time.Millisecond,
		ProfileCleanupInterval: time.Minute,
		TargetTemplate: BypassTarget{
			SetName: "set",
			Backend: "ipt",
			Timeout: 60,
		},
	}, collector)

	engine.Start()
	defer engine.Stop()

	engine.ReportNonHTTP("1.1.1.1", 80)
	engine.ReportNonHTTP("1.1.1.1", 80)

	time.Sleep(50 * time.Millisecond)

	if collector.Count() != 1 {
		t.Fatalf("expected one bypass target, got %d", collector.Count())
	}
}

func TestDecisionEngineHTTPCancelsPendingDecision(t *testing.T) {
	t.Parallel()

	collector := &targetCollector{}
	engine := NewDecisionEngine(logrus.New(), 8, DecisionConfig{
		NonHTTPThreshold:       2,
		HTTPCooldownPeriod:     50 * time.Millisecond,
		DecisionDelay:          30 * time.Millisecond,
		ProfileCleanupInterval: time.Minute,
		TargetTemplate: BypassTarget{
			SetName: "set",
			Backend: "ipt",
			Timeout: 60,
		},
	}, collector)

	engine.Start()
	defer engine.Stop()

	engine.ReportNonHTTP("1.1.1.1", 80)
	engine.ReportNonHTTP("1.1.1.1", 80)
	time.Sleep(10 * time.Millisecond)
	engine.ReportHTTP("1.1.1.1", 80)
	time.Sleep(50 * time.Millisecond)

	if collector.Count() != 0 {
		t.Fatalf("expected no bypass targets after HTTP cancellation, got %d", collector.Count())
	}
}
