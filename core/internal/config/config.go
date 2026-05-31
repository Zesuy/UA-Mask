package config

import (
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	defaultPort                  = 8080
	defaultBufferSize            = 8192
	defaultStatsFile             = "/tmp/UAmask.stats"
	defaultStatsInterval         = 5 * time.Second
	defaultDialTimeout           = 30 * time.Second
	defaultKeepAlive             = 3 * time.Minute
	defaultProfileCleanup        = 10 * time.Minute
	defaultImmediateBypassExpiry = 24 * 3600
)

type Config struct {
	ShowVer        bool
	Listen         ListenConfig
	Rewrite        RewriteConfig
	RewriteRuntime RewriteRuntimeConfig
	Firewall       FirewallConfig
	Observe        ObserveConfig
}

type ListenConfig struct {
	Port              int
	PoolSize          int
	BufferSize        int
	DialTimeout       time.Duration
	ClientKeepAlive   time.Duration
	UpstreamKeepAlive time.Duration
}

type RewriteConfig struct {
	UserAgent            string
	Whitelist            []string
	ForceReplace         bool
	EnableRegex          bool
	EnablePartialReplace bool
	Keywords             []string
	Pattern              string
	CacheSize            int
}

type RewriteRuntimeConfig struct {
	UARegexp *regexp.Regexp
}

type FirewallConfig struct {
	UAWhitelist            []string
	EnableBypass           bool
	SetName                string
	Backend                string
	DropOnMatch            bool
	NonHTTPThreshold       int
	Timeout                int
	DecisionDelay          time.Duration
	HTTPCooldownPeriod     time.Duration
	ProfileCleanupInterval time.Duration
	ImmediateBypassTimeout int
}

type ObserveConfig struct {
	LogLevel      string
	LogFile       string
	StatsFilePath string
	StatsInterval time.Duration
}

func NewFromFlags() (*Config, error) {
	return NewFromArgs(os.Args[1:])
}

func NewFromArgs(args []string) (*Config, error) {
	fs := flag.NewFlagSet("UAmask", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var (
		userAgent                string
		port                     int
		logLevel                 string
		showVer                  bool
		forceReplace             bool
		enablePartialReplace     bool
		uaPattern                string
		logFile                  string
		whitelistArg             string
		keywords                 string
		enableRegex              bool
		cacheSize                int
		bufferSize               int
		poolSize                 int
		firewallUAWhitelistArg   string
		enableFirewallUABypass   bool
		firewallIPSetName        string
		firewallType             string
		firewallDropOnMatch      bool
		firewallNonHTTPThreshold int
		firewallTimeout          int
		firewallDecisionDelay    time.Duration
		firewallHTTPCooldown     time.Duration
		statsFilePath            string
		statsInterval            time.Duration
	)

	fs.StringVar(&userAgent, "u", "FFF", "User-Agent string")
	fs.IntVar(&port, "port", defaultPort, "TPROXY listen port")
	fs.StringVar(&logLevel, "loglevel", "info", "Log level (debug, info, warn, error)")
	fs.BoolVar(&showVer, "v", false, "Show version")
	fs.StringVar(&logFile, "log", "", "Log file path (e.g., /tmp/UAmask.log). Default is stdout.")
	fs.StringVar(&whitelistArg, "w", "", "Comma-separated User-Agent whitelist")

	fs.BoolVar(&forceReplace, "force", false, "Force replace User-Agent (match_mode 'all')")
	fs.BoolVar(&enableRegex, "enable-regex", false, "Enable Regex matching mode")
	fs.StringVar(&keywords, "keywords", "iPhone,iPad,Android,Macintosh,Windows", "Comma-separated User-Agent keywords (default mode)")
	fs.StringVar(&uaPattern, "r", "(iPhone|iPad|Android|Macintosh|Windows|Linux|Apple|Mac OS X|Mobile)", "UA-Pattern (Regex)")
	fs.BoolVar(&enablePartialReplace, "s", false, "Enable Regex Partial Replace (regex mode + partial)")

	fs.IntVar(&cacheSize, "cache-size", 1000, "LRU cache size (0 disables cache)")
	fs.IntVar(&bufferSize, "buffer-size", defaultBufferSize, "I/O buffer size (bytes)")
	fs.IntVar(&poolSize, "p", 0, "Worker pool size (0 or less = one goroutine per connection)")

	fs.StringVar(&firewallUAWhitelistArg, "fw-ua-w", "", "Comma-separated User-Agent firewall whitelist keywords")
	fs.BoolVar(&enableFirewallUABypass, "fw-bypass", false, "Enable firewall bypass for non-HTTP traffic")
	fs.StringVar(&firewallIPSetName, "fw-set-name", "UAmask_bypass_set", "Firewall ipset/nfset name")
	fs.StringVar(&firewallType, "fw-type", "ipt", "Firewall type (ipt or nft)")
	fs.BoolVar(&firewallDropOnMatch, "fw-drop", false, "Drop connections that match firewall rules")
	fs.IntVar(&firewallNonHTTPThreshold, "fw-nonhttp-threshold", 5, "Firewall non-HTTP traffic threshold")
	fs.IntVar(&firewallTimeout, "fw-timeout", 8*3600, "Firewall rule timeout in seconds")
	fs.DurationVar(&firewallDecisionDelay, "fw-decision-delay", 60*time.Second, "Firewall decision delay duration")
	fs.DurationVar(&firewallHTTPCooldown, "fw-http-cooldown", 1*time.Hour, "Firewall HTTP cooldown period")

	fs.StringVar(&statsFilePath, "stats-file", defaultStatsFile, "Stats output file path")
	fs.DurationVar(&statsInterval, "stats-interval", defaultStatsInterval, "Stats write interval")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	cfg := &Config{
		ShowVer: showVer,
		Listen: ListenConfig{
			Port:              port,
			PoolSize:          poolSize,
			BufferSize:        bufferSize,
			DialTimeout:       defaultDialTimeout,
			ClientKeepAlive:   defaultKeepAlive,
			UpstreamKeepAlive: defaultKeepAlive,
		},
		Rewrite: RewriteConfig{
			UserAgent:            userAgent,
			Whitelist:            splitCSV(whitelistArg),
			ForceReplace:         forceReplace,
			EnableRegex:          enableRegex,
			EnablePartialReplace: enablePartialReplace,
			Pattern:              uaPattern,
			CacheSize:            cacheSize,
			Keywords:             []string{},
		},
		Firewall: FirewallConfig{
			UAWhitelist:            splitCSV(firewallUAWhitelistArg),
			EnableBypass:           enableFirewallUABypass,
			SetName:                firewallIPSetName,
			Backend:                firewallType,
			DropOnMatch:            firewallDropOnMatch,
			NonHTTPThreshold:       firewallNonHTTPThreshold,
			Timeout:                firewallTimeout,
			DecisionDelay:          firewallDecisionDelay,
			HTTPCooldownPeriod:     firewallHTTPCooldown,
			ProfileCleanupInterval: defaultProfileCleanup,
			ImmediateBypassTimeout: defaultImmediateBypassExpiry,
		},
		Observe: ObserveConfig{
			LogLevel:      logLevel,
			LogFile:       logFile,
			StatsFilePath: statsFilePath,
			StatsInterval: statsInterval,
		},
	}

	if err := validate(cfg); err != nil {
		return nil, err
	}

	if cfg.Rewrite.EnableRegex {
		pattern := "(?i)" + cfg.Rewrite.Pattern
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid User-Agent regex pattern: %w", err)
		}
		cfg.Rewrite.Pattern = pattern
		cfg.RewriteRuntime.UARegexp = compiled
	} else if !cfg.Rewrite.ForceReplace {
		cfg.Rewrite.Keywords = splitCSV(keywords)
	}

	return cfg, nil
}

func (c *Config) LogConfig(version string) {
	logrus.Infof("UA-MASK v%s", version)
	logrus.Infof("Port: %d", c.Listen.Port)
	logrus.Infof("User-Agent: %s", c.Rewrite.UserAgent)
	logrus.Infof("Log level: %s", c.Observe.LogLevel)
	logrus.Infof("User-Agent Whitelist: %v", c.Rewrite.Whitelist)
	logrus.Infof("Cache Size: %d", c.Rewrite.CacheSize)
	logrus.Infof("Buffer Size: %d", c.Listen.BufferSize)
	logrus.Infof("Worker Pool Size: %d", c.Listen.PoolSize)
	logrus.Infof("Stats File: %s", c.Observe.StatsFilePath)
	logrus.Infof("Stats Interval: %s", c.Observe.StatsInterval)

	logrus.Infof("Firewall Type: %s", c.Firewall.Backend)
	logrus.Infof("Firewall IPSet Name: %s", c.Firewall.SetName)
	logrus.Infof("Firewall UA Whitelist: %v", c.Firewall.UAWhitelist)
	logrus.Infof("Enable Firewall Non-HTTP Bypass: %v", c.Firewall.EnableBypass)
	logrus.Infof("Firewall Drop On Match: %v", c.Firewall.DropOnMatch)
	logrus.Infof("Firewall Non-HTTP Threshold: %d", c.Firewall.NonHTTPThreshold)
	logrus.Infof("Firewall Rule Timeout (seconds): %d", c.Firewall.Timeout)
	logrus.Infof("Firewall Decision Delay: %s", c.Firewall.DecisionDelay)
	logrus.Infof("Firewall HTTP Cooldown Period: %s", c.Firewall.HTTPCooldownPeriod)

	if c.Rewrite.ForceReplace {
		logrus.Info("Mode: Force Replace (All)")
	} else if c.Rewrite.EnableRegex {
		logrus.Infof("Mode: Regex | Pattern: %s | Partial Replace: %v", c.Rewrite.Pattern, c.Rewrite.EnablePartialReplace)
	} else {
		logrus.Infof("Mode: Keywords | Keywords: %v", c.Rewrite.Keywords)
	}
}

func splitCSV(value string) []string {
	if value == "" {
		return []string{}
	}

	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}

	return result
}

func validate(cfg *Config) error {
	if cfg.Listen.Port < 1 || cfg.Listen.Port > 65535 {
		return fmt.Errorf("invalid port: %d", cfg.Listen.Port)
	}
	if cfg.Listen.BufferSize < 1024 || cfg.Listen.BufferSize > 65536 {
		return fmt.Errorf("invalid buffer size: %d", cfg.Listen.BufferSize)
	}
	if cfg.Rewrite.CacheSize < 0 {
		return fmt.Errorf("invalid cache size: %d", cfg.Rewrite.CacheSize)
	}
	if cfg.Firewall.Backend != "ipt" && cfg.Firewall.Backend != "nft" {
		return fmt.Errorf("invalid firewall type: %s", cfg.Firewall.Backend)
	}
	if cfg.Observe.StatsInterval <= 0 {
		return fmt.Errorf("invalid stats interval: %s", cfg.Observe.StatsInterval)
	}
	return nil
}
