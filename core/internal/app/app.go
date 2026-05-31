package app

import (
	"fmt"

	"UAmask/internal/config"
	"UAmask/internal/firewall"
	"UAmask/internal/proxy"
	"UAmask/internal/rewrite"
	"UAmask/internal/stats"

	"github.com/sirupsen/logrus"
)

const firewallQueueSize = 10000

type Runtime struct {
	config           *config.Config
	version          string
	stats            *stats.Stats
	statsReporter    *stats.FileReporter
	firewallExecutor *firewall.Executor
	firewallDecision *firewall.DecisionEngine
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
	whitelistSink := firewall.NewWhitelistBypassSink(firewallExecutor, cfg.Firewall.SetName, cfg.Firewall.Backend)

	ruleEngine := rewrite.NewEngine(cfg.Rewrite, cfg.RewriteRuntime, cfg.Firewall)
	requestRewriter := rewrite.NewRequestRewriter(ruleEngine, cache, whitelistSink, cfg.Firewall.ImmediateBypassTimeout)

	var trafficReporter proxy.TrafficReporter = firewall.NoopReporter{}
	var firewallDecision *firewall.DecisionEngine
	if cfg.Firewall.EnableBypass {
		firewallDecision = firewall.NewDecisionEngine(
			logrus.StandardLogger(),
			firewallQueueSize,
			firewall.DecisionConfig{
				NonHTTPThreshold:       cfg.Firewall.NonHTTPThreshold,
				HTTPCooldownPeriod:     cfg.Firewall.HTTPCooldownPeriod,
				DecisionDelay:          cfg.Firewall.DecisionDelay,
				ProfileCleanupInterval: cfg.Firewall.ProfileCleanupInterval,
				TargetTemplate: firewall.BypassTarget{
					SetName: cfg.Firewall.SetName,
					Backend: cfg.Firewall.Backend,
					Timeout: cfg.Firewall.Timeout,
				},
			},
			firewall.NewExecutorSink(firewallExecutor),
		)
		trafficReporter = firewallDecision
	}

	processor := proxy.NewProcessor(cfg.Listen, requestRewriter, trafficReporter, appStats)
	server := proxy.NewServer(cfg.Listen, processor, appStats)

	return &Runtime{
		config:           cfg,
		version:          version,
		stats:            appStats,
		statsReporter:    statsReporter,
		firewallExecutor: firewallExecutor,
		firewallDecision: firewallDecision,
		server:           server,
	}, nil
}

func (r *Runtime) Run() error {
	r.config.LogConfig(r.version)

	r.firewallExecutor.Start()
	defer r.firewallExecutor.Stop()

	if r.firewallDecision != nil {
		r.firewallDecision.Start()
		defer r.firewallDecision.Stop()
	}

	r.statsReporter.Start()
	defer r.statsReporter.Stop()

	return r.server.Run()
}
