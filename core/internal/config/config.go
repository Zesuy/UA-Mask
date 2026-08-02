package config

import (
	"regexp"
	"time"

	"github.com/sirupsen/logrus"
)

const SchemaVersion = 1

type Config struct {
	Listen         ListenConfig
	Rewrite        RewriteConfig
	RewriteRuntime RewriteRuntimeConfig
	Firewall       FirewallConfig
	Observe        ObserveConfig
	Performance    PerformanceConfig
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

type PerformanceConfig struct {
	Profile   string
	GCPercent int
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
	logrus.Infof("Performance Profile: %s", c.Performance.Profile)
	logrus.Infof("GC Percent: %d", c.Performance.GCPercent)
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
