package firewall

import (
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

type applierStub struct {
	mu      sync.Mutex
	batches [][]BypassTarget
}

func (a *applierStub) ApplyBatch(items []BypassTarget) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	copied := make([]BypassTarget, len(items))
	copy(copied, items)
	a.batches = append(a.batches, copied)
	return nil
}

func (a *applierStub) BatchCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.batches)
}

func (a *applierStub) FirstBatchSize() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.batches) == 0 {
		return 0
	}
	return len(a.batches[0])
}

func TestExecutorDeduplicatesTargets(t *testing.T) {
	t.Parallel()

	applier := &applierStub{}
	executor := NewExecutor(logrus.New(), 8, applier)
	executor.maxBatchWait = 15 * time.Millisecond

	executor.Start()
	defer executor.Stop()

	target := BypassTarget{IP: "1.1.1.1", Port: 80, SetName: "set", Backend: "ipt", Timeout: 60}
	executor.Enqueue(target)
	executor.Enqueue(target)
	executor.Enqueue(BypassTarget{IP: "2.2.2.2", Port: 80, SetName: "set", Backend: "ipt", Timeout: 60})

	time.Sleep(50 * time.Millisecond)

	if applier.BatchCount() != 1 {
		t.Fatalf("expected one batch, got %d", applier.BatchCount())
	}
	if applier.FirstBatchSize() != 2 {
		t.Fatalf("expected deduplicated batch of size 2, got %d", applier.FirstBatchSize())
	}
}
