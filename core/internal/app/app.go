package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"UAmask/internal/config"
	"UAmask/internal/events"
	"UAmask/internal/firewall"
	"UAmask/internal/ingress"
	"UAmask/internal/offload"
	"UAmask/internal/policy"
	"UAmask/internal/proxy"
	"UAmask/internal/rewrite"
	"UAmask/internal/session"
	"UAmask/internal/state"
	"UAmask/internal/stats"

	"github.com/sirupsen/logrus"
)

const firewallQueueSize = 10000
const runtimeShutdownTimeout = 15 * time.Second

type startStop interface {
	Start()
	Stop()
}

type executorRuntime interface {
	startStop
	Flush(context.Context) error
}

type dispatcherRuntime interface {
	startStop
	WaitIdle(context.Context) error
}

type offloadRuntime interface {
	Stop()
}

type serverRuntime interface {
	RunContext(context.Context) error
}

type Runtime struct {
	config           *config.Config
	version          string
	stats            *stats.Stats
	statsReporter    startStop
	profileStore     startStop
	firewallExecutor executorRuntime
	dispatcher       dispatcherRuntime
	offload          offloadRuntime
	server           serverRuntime
	shutdownTimeout  time.Duration
}

func Run(version string) error {
	command, err := config.NewFromFlags()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if command.ShowVersion {
		_, err := fmt.Fprintf(os.Stdout, "UA-Mask version: %s\n", version)
		return err
	}
	if command.DumpEffective {
		return config.WriteEffective(os.Stdout, command.Config)
	}
	if command.CheckConfig {
		_, err := fmt.Fprintln(os.Stdout, "configuration valid")
		return err
	}

	cfg := command.Config
	if cfg == nil {
		return fmt.Errorf("configuration is not available")
	}
	applyRuntimeTuning(command)
	setupLogging(cfg.Observe.LogLevel, cfg.Observe.LogFile)

	if command.ConfigPath != "" {
		logrus.Infof("Loaded configuration from %s", command.ConfigPath)
	}

	runtime, err := NewRuntime(cfg, version)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runtime.RunContext(ctx)
}

func applyRuntimeTuning(command *config.Command) {
	if command == nil || command.Config == nil || command.ConfigPath == "" {
		return
	}
	// Legacy flag-only launches historically left GOGC under the Go runtime and
	// environment's control. JSON configuration makes GC tuning explicit.
	debug.SetGCPercent(command.Config.Performance.GCPercent)
}

func NewRuntime(cfg *config.Config, version string) (*Runtime, error) {
	appStats := stats.New()
	statsReporter := stats.NewFileReporter(appStats, cfg.Observe.StatsFilePath, cfg.Observe.StatsInterval)

	cache, err := rewrite.NewUACache(cfg.Rewrite.CacheSize)
	if err != nil {
		return nil, fmt.Errorf("create UA cache: %w", err)
	}

	firewallExecutor := firewall.NewExecutor(logrus.StandardLogger(), firewallQueueSize, firewall.NewCommandApplier())

	ruleEngine := rewrite.NewEngine(cfg.Rewrite, cfg.RewriteRuntime, cfg.Firewall)
	requestRewriter := rewrite.NewRequestRewriter(ruleEngine, cache)

	profileStore := state.NewStore(state.Config{
		NonHTTPEnabled:         cfg.Firewall.EnableBypass,
		NonHTTPThreshold:       cfg.Firewall.NonHTTPThreshold,
		HTTPCooldownPeriod:     cfg.Firewall.HTTPCooldownPeriod,
		DecisionDelay:          cfg.Firewall.DecisionDelay,
		ProfileCleanupInterval: cfg.Firewall.ProfileCleanupInterval,
	})

	targetOffloader := offload.NewFirewallTargetOffloader(firewallExecutor, cfg.Firewall.SetName, cfg.Firewall.Backend)

	sessionPolicy := policy.NewSessionPolicy()
	targetPolicy := policy.NewTargetPolicy(
		cfg.Firewall.Timeout,
		cfg.Firewall.ImmediateBypassTimeout,
		cfg.Firewall.NonHTTPThreshold,
		cfg.Firewall.EnableBypass,
	)
	dispatcher := events.NewDispatcher()
	offloadCoordinator := offload.NewCoordinator(
		logrus.StandardLogger(),
		profileStore,
		targetPolicy,
		targetOffloader,
		offload.NoopFlowOffloader{},
		dispatcher,
	)
	dispatcher.Add(profileStore, appStats, offloadCoordinator)

	sessionManager := session.NewManager(cfg.Listen, sessionPolicy, requestRewriter, profileStore, offloadCoordinator, dispatcher)
	redirectIngress, err := ingress.NewRedirect(cfg.Listen)
	if err != nil {
		return nil, err
	}
	server := proxy.NewServer(cfg.Listen.PoolSize, redirectIngress, sessionManager)

	return &Runtime{
		config:           cfg,
		version:          version,
		stats:            appStats,
		statsReporter:    statsReporter,
		profileStore:     profileStore,
		firewallExecutor: firewallExecutor,
		dispatcher:       dispatcher,
		offload:          offloadCoordinator,
		server:           server,
		shutdownTimeout:  runtimeShutdownTimeout,
	}, nil
}

func (r *Runtime) Run() error {
	return r.RunContext(context.Background())
}

func (r *Runtime) RunContext(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.config.LogConfig(r.version)

	r.firewallExecutor.Start()
	r.profileStore.Start()
	r.statsReporter.Start()
	r.dispatcher.Start()

	runErr := r.server.RunContext(ctx)
	if ctx.Err() != nil && errors.Is(runErr, ctx.Err()) {
		runErr = nil
	}
	return errors.Join(runErr, r.shutdown())
}

func (r *Runtime) validate() error {
	if r == nil || r.config == nil || r.statsReporter == nil || r.profileStore == nil ||
		r.firewallExecutor == nil || r.dispatcher == nil || r.offload == nil || r.server == nil {
		return errors.New("runtime dependencies are not configured")
	}
	return nil
}

func (r *Runtime) shutdown() error {
	timeout := r.shutdownTimeout
	if timeout <= 0 {
		timeout = runtimeShutdownTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	r.offload.Stop()
	flushErr := r.firewallExecutor.Flush(ctx)
	r.firewallExecutor.Stop()
	idleErr := r.dispatcher.WaitIdle(ctx)
	r.dispatcher.Stop()
	r.statsReporter.Stop()
	r.profileStore.Stop()

	return errors.Join(
		wrapShutdownError("flush firewall executor", flushErr),
		wrapShutdownError("wait for event dispatcher", idleErr),
	)
}

func wrapShutdownError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
