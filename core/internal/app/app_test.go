package app

import (
	"context"
	"errors"
	"runtime/debug"
	"sync"
	"testing"
	"time"

	"UAmask/internal/config"
	"UAmask/internal/events"
	"UAmask/internal/firewall"
	"UAmask/internal/model"
	"UAmask/internal/proxy"
	"UAmask/internal/state"
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

func TestNewRuntimeWiresEventProjection(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Firewall.EnableBypass = true
	cfg.Firewall.NonHTTPThreshold = 2

	runtime, err := NewRuntime(cfg, "test")
	if err != nil {
		t.Fatalf("RUNTIME-WIRE-201 NewRuntime: %v", err)
	}
	dispatcher, ok := runtime.dispatcher.(*events.Dispatcher)
	if !ok {
		t.Fatalf("RUNTIME-WIRE-201 dispatcher type = %T", runtime.dispatcher)
	}
	profiles, ok := runtime.profileStore.(*state.Store)
	if !ok {
		t.Fatalf("RUNTIME-WIRE-201 profile store type = %T", runtime.profileStore)
	}
	if _, ok := runtime.firewallExecutor.(*firewall.Executor); !ok {
		t.Fatalf("RUNTIME-WIRE-201 executor type = %T", runtime.firewallExecutor)
	}
	if _, ok := runtime.server.(*proxy.Server); !ok {
		t.Fatalf("RUNTIME-WIRE-201 server type = %T", runtime.server)
	}

	target := model.Target{IP: "203.0.113.20", Port: 80}
	dispatcher.Append(model.SessionEvent{
		Type:      model.EventHTTPRequestObserved,
		Target:    target,
		Timestamp: time.Unix(100, 0),
	})
	if snapshot := profiles.Snapshot(target); !snapshot.Found || snapshot.HTTPCooldownUntil.IsZero() {
		t.Fatalf("RUNTIME-WIRE-201 profile was not projected: %+v", snapshot)
	}
	if got := runtime.stats.Snapshot().HTTPRequests; got != 1 {
		t.Fatalf("RUNTIME-WIRE-201 HTTP requests = %d, want 1", got)
	}
}

type lifecycleLog struct {
	mu    sync.Mutex
	calls []string
}

func (log *lifecycleLog) Add(call string) {
	log.mu.Lock()
	log.calls = append(log.calls, call)
	log.mu.Unlock()
}

func (log *lifecycleLog) Values() []string {
	log.mu.Lock()
	defer log.mu.Unlock()
	return append([]string(nil), log.calls...)
}

type lifecycleComponent struct {
	log  *lifecycleLog
	name string
}

func (component *lifecycleComponent) Start() { component.log.Add(component.name + ".start") }
func (component *lifecycleComponent) Stop()  { component.log.Add(component.name + ".stop") }

type executorComponent struct {
	*lifecycleComponent
	flushErr error
}

func (component *executorComponent) Flush(context.Context) error {
	component.log.Add(component.name + ".flush")
	return component.flushErr
}

type dispatcherComponent struct {
	*lifecycleComponent
	idleErr error
}

func (component *dispatcherComponent) WaitIdle(context.Context) error {
	component.log.Add(component.name + ".idle")
	return component.idleErr
}

type offloadComponent struct {
	log *lifecycleLog
}

func (component *offloadComponent) Stop() { component.log.Add("offload.stop") }

type serverComponent struct {
	log       *lifecycleLog
	started   chan struct{}
	runErr    error
	waitFor   bool
	startOnce sync.Once
}

func (server *serverComponent) RunContext(ctx context.Context) error {
	server.log.Add("server.run")
	server.startOnce.Do(func() { close(server.started) })
	if server.waitFor {
		<-ctx.Done()
		server.log.Add("server.done")
		return ctx.Err()
	}
	server.log.Add("server.done")
	return server.runErr
}

func TestRuntimeRunContextUsesCausalShutdownOrder(t *testing.T) {
	log := &lifecycleLog{}
	runtime := runtimeWithFakes(log)
	runtime.server = &serverComponent{log: log, started: make(chan struct{}), waitFor: true}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runtime.RunContext(ctx) }()

	waitAppClosed(t, runtime.server.(*serverComponent).started)
	cancel()
	if err := waitAppValue(t, done); err != nil {
		t.Fatalf("LIFE-SHUTDOWN-201 RunContext: %v", err)
	}

	want := []string{
		"executor.start", "store.start", "stats.start", "dispatcher.start",
		"server.run", "server.done", "offload.stop", "executor.flush",
		"executor.stop", "dispatcher.idle", "dispatcher.stop", "stats.stop", "store.stop",
	}
	assertLifecycleCalls(t, log.Values(), want)
}

func TestRuntimeRunContextReportsErrorsAndCompletesShutdown(t *testing.T) {
	runFailure := errors.New("server failed")
	flushFailure := errors.New("flush failed")
	log := &lifecycleLog{}
	runtime := runtimeWithFakes(log)
	runtime.server = &serverComponent{log: log, started: make(chan struct{}), runErr: runFailure}
	runtime.firewallExecutor.(*executorComponent).flushErr = flushFailure

	err := runtime.RunContext(context.Background())
	if !errors.Is(err, runFailure) || !errors.Is(err, flushFailure) {
		t.Fatalf("LIFE-ERROR-201 error = %v, want server and flush failures", err)
	}
	wantTail := []string{
		"offload.stop", "executor.flush", "executor.stop",
		"dispatcher.idle", "dispatcher.stop", "stats.stop", "store.stop",
	}
	calls := log.Values()
	assertLifecycleCalls(t, calls[len(calls)-len(wantTail):], wantTail)
}

func runtimeWithFakes(log *lifecycleLog) *Runtime {
	return &Runtime{
		config:           config.DefaultConfig(),
		version:          "test",
		statsReporter:    &lifecycleComponent{log: log, name: "stats"},
		profileStore:     &lifecycleComponent{log: log, name: "store"},
		firewallExecutor: &executorComponent{lifecycleComponent: &lifecycleComponent{log: log, name: "executor"}},
		dispatcher:       &dispatcherComponent{lifecycleComponent: &lifecycleComponent{log: log, name: "dispatcher"}},
		offload:          &offloadComponent{log: log},
		shutdownTimeout:  time.Second,
	}
}

func assertLifecycleCalls(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("lifecycle calls = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("lifecycle call %d = %q, want %q; all calls: %v", index, got[index], want[index], got)
		}
	}
}

func waitAppClosed(t *testing.T, channel <-chan struct{}) {
	t.Helper()
	select {
	case <-channel:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runtime start")
	}
}

func waitAppValue[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runtime completion")
		var zero T
		return zero
	}
}
