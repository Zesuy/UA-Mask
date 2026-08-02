package scenario

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"UAmask/internal/events"
	"UAmask/internal/firewall"
	"UAmask/internal/model"
	"UAmask/internal/offload"
	"UAmask/internal/policy"
	"UAmask/internal/state"
	"UAmask/internal/testkit/manualclock"
	"UAmask/internal/testkit/record"

	"github.com/sirupsen/logrus"
)

type TargetConfig struct {
	Start                   time.Time
	NonHTTPThreshold        int
	DecisionDelay           time.Duration
	HTTPCooldownPeriod      time.Duration
	ProfileCleanupInterval  time.Duration
	TargetOffloadTimeout    int
	WhitelistOffloadTimeout int
	ExecutorQueueSize       int
}

type Target struct {
	Clock       *manualclock.Clock
	Dispatcher  *events.Dispatcher
	Store       *state.Store
	Policy      *policy.TargetPolicy
	Coordinator *offload.Coordinator
	Executor    *firewall.Executor
	Events      *record.Events
	Signals     *record.Signals
	Snapshots   *record.Snapshots
	Batches     *record.Batches

	applier          *controlledApplier
	completedSignals int
	closeOnce        sync.Once
	closeErr         error
}

func NewTarget(config TargetConfig) *Target {
	config = targetDefaults(config)
	runtimeClock := manualclock.New(config.Start)
	profiles := state.NewStoreWithClock(state.Config{
		NonHTTPEnabled:         true,
		NonHTTPThreshold:       config.NonHTTPThreshold,
		HTTPCooldownPeriod:     config.HTTPCooldownPeriod,
		DecisionDelay:          config.DecisionDelay,
		ProfileCleanupInterval: config.ProfileCleanupInterval,
	}, runtimeClock)
	eventsRecorder := &record.Events{}
	signalsRecorder := &record.Signals{}
	snapshotsRecorder := &record.Snapshots{}
	batchesRecorder := &record.Batches{}
	applier := &controlledApplier{
		results: make(chan error, config.ExecutorQueueSize),
		batches: batchesRecorder,
	}
	executor := firewall.NewExecutorWithClock(testLogger(), config.ExecutorQueueSize, applier, runtimeClock)
	targetOffloader := &recordingTargetOffloader{
		signals:  signalsRecorder,
		delegate: offload.NewFirewallTargetOffloader(executor, "test_bypass_set", "ipt"),
	}
	flowOffloader := &recordingFlowOffloader{signals: signalsRecorder}
	targetPolicy := policy.NewTargetPolicyWithClock(
		config.TargetOffloadTimeout,
		config.WhitelistOffloadTimeout,
		config.NonHTTPThreshold,
		true,
		runtimeClock,
	)
	dispatcher := events.NewDispatcher()
	coordinator := offload.NewCoordinatorWithClock(
		testLogger(),
		profiles,
		targetPolicy,
		targetOffloader,
		flowOffloader,
		dispatcher,
		runtimeClock,
	)
	snapshotObserver := &snapshotObserver{profiles: profiles, snapshots: snapshotsRecorder}
	dispatcher.Add(profiles, snapshotObserver, eventsRecorder, coordinator)

	scenario := &Target{
		Clock:       runtimeClock,
		Dispatcher:  dispatcher,
		Store:       profiles,
		Policy:      targetPolicy,
		Coordinator: coordinator,
		Executor:    executor,
		Events:      eventsRecorder,
		Signals:     signalsRecorder,
		Snapshots:   snapshotsRecorder,
		Batches:     batchesRecorder,
		applier:     applier,
	}
	profiles.Start()
	executor.Start()
	dispatcher.Start()
	return scenario
}

func (scenario *Target) Emit(ctx context.Context, event model.SessionEvent) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = scenario.Clock.Now()
	}
	if !scenario.Dispatcher.TryAppend(event) {
		return fmt.Errorf("append event %s", event.Type)
	}
	return scenario.settle(ctx)
}

func (scenario *Target) Advance(ctx context.Context, duration time.Duration) error {
	scenario.Clock.Advance(duration)
	return scenario.settle(ctx)
}

func (scenario *Target) CompleteNext(ctx context.Context, result error) error {
	if scenario.Signals.Len() <= scenario.completedSignals {
		return errors.New("no pending target offload signal")
	}
	scenario.applier.results <- result
	if err := scenario.Executor.Flush(ctx); err != nil {
		return err
	}
	scenario.completedSignals = scenario.Signals.Len()
	return scenario.settle(ctx)
}

func (scenario *Target) Snapshot(target model.Target) model.ProfileSnapshot {
	return scenario.Store.Snapshot(target)
}

func (scenario *Target) Close(ctx context.Context) error {
	scenario.closeOnce.Do(func() {
		scenario.Coordinator.Stop()
		if scenario.Signals.Len() > scenario.completedSignals {
			scenario.applier.results <- nil
		}
		if err := scenario.Executor.Flush(ctx); err != nil && !errors.Is(err, firewall.ErrExecutorStopped) {
			scenario.closeErr = err
		}
		scenario.Executor.Stop()
		if err := scenario.Dispatcher.WaitIdle(ctx); err != nil && scenario.closeErr == nil {
			scenario.closeErr = err
		}
		scenario.Dispatcher.Stop()
		scenario.Store.Stop()
	})
	return scenario.closeErr
}

func (scenario *Target) settle(ctx context.Context) error {
	for {
		if err := scenario.Dispatcher.WaitIdle(ctx); err != nil {
			return err
		}
		if scenario.Clock.Due() == 0 {
			return nil
		}
		scenario.Clock.Advance(0)
	}
}

func targetDefaults(config TargetConfig) TargetConfig {
	if config.Start.IsZero() {
		config.Start = time.Unix(1_000, 0)
	}
	if config.NonHTTPThreshold <= 0 {
		config.NonHTTPThreshold = 2
	}
	if config.DecisionDelay <= 0 {
		config.DecisionDelay = 30 * time.Second
	}
	if config.HTTPCooldownPeriod <= 0 {
		config.HTTPCooldownPeriod = time.Minute
	}
	if config.ProfileCleanupInterval <= 0 {
		config.ProfileCleanupInterval = 24 * time.Hour
	}
	if config.TargetOffloadTimeout <= 0 {
		config.TargetOffloadTimeout = 60
	}
	if config.WhitelistOffloadTimeout <= 0 {
		config.WhitelistOffloadTimeout = 3600
	}
	if config.ExecutorQueueSize <= 0 {
		config.ExecutorQueueSize = 16
	}
	return config
}

type snapshotObserver struct {
	profiles  state.ProfileStore
	snapshots *record.Snapshots
}

func (observer *snapshotObserver) Append(event model.SessionEvent) {
	if event.Target.IP == "" || event.Target.Port == 0 {
		return
	}
	observer.snapshots.Add(observer.profiles.Snapshot(event.Target))
}

type targetOffloader interface {
	Handle(model.OffloadSignal, func(error)) error
}

type recordingTargetOffloader struct {
	signals  *record.Signals
	delegate targetOffloader
}

func (offloader *recordingTargetOffloader) Handle(signal model.OffloadSignal, completion func(error)) error {
	offloader.signals.Add(signal)
	return offloader.delegate.Handle(signal, completion)
}

type recordingFlowOffloader struct {
	signals *record.Signals
	mu      sync.Mutex
	err     error
}

func (offloader *recordingFlowOffloader) Handle(signal model.OffloadSignal) error {
	offloader.signals.Add(signal)
	offloader.mu.Lock()
	defer offloader.mu.Unlock()
	return offloader.err
}

type controlledApplier struct {
	results chan error
	batches *record.Batches
}

func (applier *controlledApplier) ApplyBatch(items []firewall.BypassTarget) error {
	applier.batches.AddBatch(items)
	return <-applier.results
}

func testLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return logger
}
