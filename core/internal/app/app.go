package app

import (
	"fmt"

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

type Runtime struct {
	config           *config.Config
	version          string
	stats            *stats.Stats
	statsReporter    *stats.FileReporter
	profileStore     *state.Store
	firewallExecutor *firewall.Executor
	dispatcher       *events.Dispatcher
	offload          *offload.Coordinator
	server           *proxy.Server
}

func Run(version string) error {
	cfg, err := config.NewFromFlags()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	setupLogging(cfg.Observe.LogLevel, cfg.Observe.LogFile)

	if cfg.ShowVer {
		logrus.Infof("UA-Mask version: %s", version)
		return nil
	}

	runtime, err := NewRuntime(cfg, version)
	if err != nil {
		return err
	}

	return runtime.Run()
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
	}, nil
}

func (r *Runtime) Run() error {
	r.config.LogConfig(r.version)

	r.firewallExecutor.Start()
	r.profileStore.Start()
	r.statsReporter.Start()
	r.dispatcher.Start()
	defer func() {
		if r.offload != nil {
			r.offload.Stop()
		}
		r.firewallExecutor.Stop()
		r.dispatcher.Stop()
		r.statsReporter.Stop()
		r.profileStore.Stop()
	}()

	return r.server.Run()
}
