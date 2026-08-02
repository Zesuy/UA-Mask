package firewall

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/sirupsen/logrus"
)

type applierStub struct {
	mu      sync.Mutex
	batches [][]BypassTarget
	err     error
}

func (a *applierStub) ApplyBatch(items []BypassTarget) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	copied := make([]BypassTarget, len(items))
	copy(copied, items)
	a.batches = append(a.batches, copied)
	return a.err
}

func TestExecutorReportsActualApplyResult(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("apply failed")
	applier := &applierStub{err: wantErr}
	executor := NewExecutor(logrus.New(), 8, applier)
	result := make(chan error, 2)

	executor.Start()
	if !executor.TryEnqueueWithResult(BypassTarget{
		IP:      "1.1.1.1",
		Port:    80,
		SetName: "set",
		Backend: "ipt",
		Timeout: 60,
	}, func(err error) {
		result <- err
	}) {
		t.Fatal("expected target to be queued")
	}
	if !executor.TryEnqueueWithResult(BypassTarget{
		IP:      "1.1.1.1",
		Port:    80,
		SetName: "set",
		Backend: "ipt",
		Timeout: 60,
	}, func(err error) {
		result <- err
	}) {
		t.Fatal("expected duplicate target completion to be queued")
	}

	if err := executor.Flush(context.Background()); err != nil {
		t.Fatalf("QUEUE-EXEC-002 Flush: %v", err)
	}
	for index := 0; index < 2; index++ {
		gotErr := <-result
		if !errors.Is(gotErr, wantErr) {
			t.Fatalf("completion %d error = %v, want %v", index, gotErr, wantErr)
		}
	}
	executor.Stop()
}

func TestExecutorRejectsTargetsAfterStop(t *testing.T) {
	t.Parallel()

	executor := NewExecutor(logrus.New(), 1, &applierStub{})
	executor.Start()
	executor.Stop()

	if executor.TryEnqueue(BypassTarget{
		IP:      "1.1.1.1",
		Port:    80,
		SetName: "set",
		Backend: "ipt",
		Timeout: 60,
	}) {
		t.Fatal("expected stopped executor to reject new targets")
	}
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
	executor.Start()
	defer executor.Stop()

	target := BypassTarget{IP: "1.1.1.1", Port: 80, SetName: "set", Backend: "ipt", Timeout: 60}
	executor.Enqueue(target)
	executor.Enqueue(target)
	executor.Enqueue(BypassTarget{IP: "2.2.2.2", Port: 80, SetName: "set", Backend: "ipt", Timeout: 60})

	if err := executor.Flush(context.Background()); err != nil {
		t.Fatalf("QUEUE-EXEC-001 Flush: %v", err)
	}

	if applier.BatchCount() != 1 {
		t.Fatalf("expected one batch, got %d", applier.BatchCount())
	}
	if applier.FirstBatchSize() != 2 {
		t.Fatalf("expected deduplicated batch of size 2, got %d", applier.FirstBatchSize())
	}
}

func TestExecutorStopDrainsAndRejectsNewWork(t *testing.T) {
	t.Parallel()

	applier := &applierStub{}
	executor := NewExecutor(logrus.New(), 4, applier)
	target := BypassTarget{IP: "1.1.1.1", Port: 80, SetName: "set", Backend: "ipt", Timeout: 60}
	if !executor.TryEnqueue(target) {
		t.Fatal("QUEUE-EXEC-003 expected target to be queued before Start")
	}
	executor.Start()
	executor.Stop()

	if applier.BatchCount() != 1 {
		t.Fatalf("QUEUE-EXEC-003 Stop applied %d batches, want 1", applier.BatchCount())
	}
	if executor.TryEnqueue(target) {
		t.Fatal("QUEUE-EXEC-003 stopped executor accepted target")
	}
	if err := executor.Flush(context.Background()); !errors.Is(err, ErrExecutorStopped) {
		t.Fatalf("QUEUE-EXEC-003 Flush error = %v, want ErrExecutorStopped", err)
	}
}

func TestExecutorFlushHonorsContext(t *testing.T) {
	t.Parallel()

	executor := NewExecutor(logrus.New(), 1, &applierStub{})
	if !executor.TryEnqueue(BypassTarget{IP: "1.1.1.1", Port: 80, SetName: "set", Backend: "ipt", Timeout: 60}) {
		t.Fatal("expected queue setup to succeed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := executor.Flush(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Flush error = %v, want context.Canceled", err)
	}
}
