package config

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type Command struct {
	Config        *Config
	ConfigPath    string
	ShowVersion   bool
	CheckConfig   bool
	DumpEffective bool
}

type legacyValues struct {
	userAgent                string
	port                     int
	logLevel                 string
	logFile                  string
	whitelist                string
	forceReplace             bool
	enableRegex              bool
	partialReplace           bool
	keywords                 string
	pattern                  string
	cacheSize                int
	bufferSize               int
	poolSize                 int
	firewallUAWhitelist      string
	firewallBypass           bool
	firewallSetName          string
	firewallBackend          string
	firewallDropOnMatch      bool
	firewallNonHTTPThreshold int
	firewallTimeout          int
	firewallDecisionDelay    time.Duration
	firewallHTTPCooldown     time.Duration
	statsFilePath            string
	statsInterval            time.Duration
}

func NewFromFlags() (*Command, error) {
	return LoadFromArgs(os.Args[1:])
}

func NewFromArgs(args []string) (*Config, error) {
	command, err := LoadFromArgs(args)
	if err != nil {
		return nil, err
	}
	return command.Config, nil
}

func LoadFromArgs(args []string) (*Command, error) {
	defaults := DefaultConfig()
	legacy := legacyValues{
		userAgent:                defaults.Rewrite.UserAgent,
		port:                     defaults.Listen.Port,
		logLevel:                 defaults.Observe.LogLevel,
		logFile:                  defaults.Observe.LogFile,
		whitelist:                strings.Join(defaults.Rewrite.Whitelist, ","),
		forceReplace:             defaults.Rewrite.ForceReplace,
		enableRegex:              defaults.Rewrite.EnableRegex,
		partialReplace:           defaults.Rewrite.EnablePartialReplace,
		keywords:                 strings.Join(defaults.Rewrite.Keywords, ","),
		pattern:                  defaults.Rewrite.Pattern,
		cacheSize:                defaults.Rewrite.CacheSize,
		bufferSize:               defaults.Listen.BufferSize,
		poolSize:                 defaults.Listen.PoolSize,
		firewallUAWhitelist:      strings.Join(defaults.Firewall.UAWhitelist, ","),
		firewallBypass:           defaults.Firewall.EnableBypass,
		firewallSetName:          defaults.Firewall.SetName,
		firewallBackend:          defaults.Firewall.Backend,
		firewallDropOnMatch:      defaults.Firewall.DropOnMatch,
		firewallNonHTTPThreshold: defaults.Firewall.NonHTTPThreshold,
		firewallTimeout:          defaults.Firewall.Timeout,
		firewallDecisionDelay:    defaults.Firewall.DecisionDelay,
		firewallHTTPCooldown:     defaults.Firewall.HTTPCooldownPeriod,
		statsFilePath:            defaults.Observe.StatsFilePath,
		statsInterval:            defaults.Observe.StatsInterval,
	}

	command := &Command{}
	fs := flag.NewFlagSet("UAmask", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&command.ConfigPath, "config", "", "Path to JSON configuration file")
	fs.BoolVar(&command.CheckConfig, "check-config", false, "Validate configuration and exit")
	fs.BoolVar(&command.DumpEffective, "dump-effective-config", false, "Print normalized configuration and exit")
	fs.BoolVar(&command.ShowVersion, "v", false, "Show version")

	fs.StringVar(&legacy.userAgent, "u", legacy.userAgent, "User-Agent string (deprecated: use -config)")
	fs.IntVar(&legacy.port, "port", legacy.port, "Listen port (deprecated: use -config)")
	fs.StringVar(&legacy.logLevel, "loglevel", legacy.logLevel, "Log level (deprecated: use -config)")
	fs.StringVar(&legacy.logFile, "log", legacy.logFile, "Log file path (deprecated: use -config)")
	fs.StringVar(&legacy.whitelist, "w", legacy.whitelist, "User-Agent whitelist CSV (deprecated: use -config)")
	fs.BoolVar(&legacy.forceReplace, "force", legacy.forceReplace, "Force replace User-Agent (deprecated: use -config)")
	fs.BoolVar(&legacy.enableRegex, "enable-regex", legacy.enableRegex, "Enable regex matching (deprecated: use -config)")
	fs.BoolVar(&legacy.partialReplace, "s", legacy.partialReplace, "Enable partial replacement (deprecated: use -config)")
	fs.StringVar(&legacy.keywords, "keywords", legacy.keywords, "User-Agent keyword CSV (deprecated: use -config)")
	fs.StringVar(&legacy.pattern, "r", legacy.pattern, "User-Agent regex (deprecated: use -config)")
	fs.IntVar(&legacy.cacheSize, "cache-size", legacy.cacheSize, "LRU cache size (deprecated: use -config)")
	fs.IntVar(&legacy.bufferSize, "buffer-size", legacy.bufferSize, "I/O buffer size (deprecated: use -config)")
	fs.IntVar(&legacy.poolSize, "p", legacy.poolSize, "Worker pool size (deprecated: use -config)")
	fs.StringVar(&legacy.firewallUAWhitelist, "fw-ua-w", legacy.firewallUAWhitelist, "Firewall UA whitelist CSV (deprecated: use -config)")
	fs.BoolVar(&legacy.firewallBypass, "fw-bypass", legacy.firewallBypass, "Enable non-HTTP bypass (deprecated: use -config)")
	fs.StringVar(&legacy.firewallSetName, "fw-set-name", legacy.firewallSetName, "Firewall set name (deprecated: use -config)")
	fs.StringVar(&legacy.firewallBackend, "fw-type", legacy.firewallBackend, "Firewall backend (deprecated: use -config)")
	fs.BoolVar(&legacy.firewallDropOnMatch, "fw-drop", legacy.firewallDropOnMatch, "Drop firewall whitelist matches (deprecated: use -config)")
	fs.IntVar(&legacy.firewallNonHTTPThreshold, "fw-nonhttp-threshold", legacy.firewallNonHTTPThreshold, "Non-HTTP threshold (deprecated: use -config)")
	fs.IntVar(&legacy.firewallTimeout, "fw-timeout", legacy.firewallTimeout, "Firewall timeout seconds (deprecated: use -config)")
	fs.DurationVar(&legacy.firewallDecisionDelay, "fw-decision-delay", legacy.firewallDecisionDelay, "Firewall decision delay (deprecated: use -config)")
	fs.DurationVar(&legacy.firewallHTTPCooldown, "fw-http-cooldown", legacy.firewallHTTPCooldown, "HTTP cooldown (deprecated: use -config)")
	fs.StringVar(&legacy.statsFilePath, "stats-file", legacy.statsFilePath, "Stats file path (deprecated: use -config)")
	fs.DurationVar(&legacy.statsInterval, "stats-interval", legacy.statsInterval, "Stats interval (deprecated: use -config)")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() != 0 {
		return nil, fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	cfg := defaults
	var err error
	if command.ConfigPath != "" {
		cfg, err = LoadFile(command.ConfigPath)
		if err != nil {
			return nil, err
		}
	}

	visited := make(map[string]bool)
	fs.Visit(func(value *flag.Flag) {
		visited[value.Name] = true
	})
	applyLegacyOverrides(cfg, legacy, visited)
	if err := finalize(cfg); err != nil {
		return nil, err
	}
	command.Config = cfg
	return command, nil
}

func applyLegacyOverrides(cfg *Config, values legacyValues, visited map[string]bool) {
	if visited["u"] {
		cfg.Rewrite.UserAgent = values.userAgent
	}
	if visited["port"] {
		cfg.Listen.Port = values.port
	}
	if visited["loglevel"] {
		cfg.Observe.LogLevel = strings.ToLower(values.logLevel)
	}
	if visited["log"] {
		cfg.Observe.LogFile = values.logFile
	}
	if visited["w"] {
		cfg.Rewrite.Whitelist = splitCSV(values.whitelist)
	}
	if visited["force"] {
		cfg.Rewrite.ForceReplace = values.forceReplace
	}
	if visited["enable-regex"] {
		cfg.Rewrite.EnableRegex = values.enableRegex
	}
	if visited["force"] && values.forceReplace {
		cfg.Rewrite.ForceReplace = true
		cfg.Rewrite.EnableRegex = false
	} else if visited["enable-regex"] && values.enableRegex {
		cfg.Rewrite.ForceReplace = false
		cfg.Rewrite.EnableRegex = true
	}
	if visited["s"] {
		cfg.Rewrite.EnablePartialReplace = values.partialReplace
	}
	if visited["keywords"] {
		cfg.Rewrite.Keywords = splitCSV(values.keywords)
	}
	if visited["r"] {
		cfg.Rewrite.Pattern = values.pattern
	}
	if visited["cache-size"] {
		cfg.Rewrite.CacheSize = values.cacheSize
		cfg.Performance.Profile = "custom"
	}
	if visited["buffer-size"] {
		cfg.Listen.BufferSize = values.bufferSize
		cfg.Performance.Profile = "custom"
	}
	if visited["p"] {
		cfg.Listen.PoolSize = values.poolSize
		cfg.Performance.Profile = "custom"
	}
	if visited["fw-ua-w"] {
		cfg.Firewall.UAWhitelist = splitCSV(values.firewallUAWhitelist)
	}
	if visited["fw-bypass"] {
		cfg.Firewall.EnableBypass = values.firewallBypass
	}
	if visited["fw-set-name"] {
		cfg.Firewall.SetName = values.firewallSetName
	}
	if visited["fw-type"] {
		cfg.Firewall.Backend = strings.ToLower(values.firewallBackend)
	}
	if visited["fw-drop"] {
		cfg.Firewall.DropOnMatch = values.firewallDropOnMatch
	}
	if visited["fw-nonhttp-threshold"] {
		cfg.Firewall.NonHTTPThreshold = values.firewallNonHTTPThreshold
	}
	if visited["fw-timeout"] {
		cfg.Firewall.Timeout = values.firewallTimeout
	}
	if visited["fw-decision-delay"] {
		cfg.Firewall.DecisionDelay = values.firewallDecisionDelay
	}
	if visited["fw-http-cooldown"] {
		cfg.Firewall.HTTPCooldownPeriod = values.firewallHTTPCooldown
	}
	if visited["stats-file"] {
		cfg.Observe.StatsFilePath = values.statsFilePath
	}
	if visited["stats-interval"] {
		cfg.Observe.StatsInterval = values.statsInterval
	}
}

func splitCSV(value string) []string {
	if value == "" {
		return []string{}
	}
	return normalizeList(strings.Split(value, ","))
}
