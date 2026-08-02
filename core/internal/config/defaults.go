package config

import "time"

const (
	defaultPort                  = 8080
	defaultBufferSize            = 8192
	defaultStatsFile             = "/tmp/UAmask.stats"
	defaultStatsInterval         = 5 * time.Second
	defaultDialTimeout           = 30 * time.Second
	defaultKeepAlive             = 3 * time.Minute
	defaultProfileCleanup        = 10 * time.Minute
	defaultImmediateBypassExpiry = 24 * 3600
	defaultPattern               = "(iPhone|iPad|Android|Macintosh|Windows|Linux|Apple|Mac OS X|Mobile)"
)

func DefaultConfig() *Config {
	return &Config{
		Listen: ListenConfig{
			Port:              defaultPort,
			PoolSize:          0,
			BufferSize:        defaultBufferSize,
			DialTimeout:       defaultDialTimeout,
			ClientKeepAlive:   defaultKeepAlive,
			UpstreamKeepAlive: defaultKeepAlive,
		},
		Rewrite: RewriteConfig{
			UserAgent: "FFF",
			Keywords:  []string{"iPhone", "iPad", "Android", "Macintosh", "Windows"},
			Pattern:   defaultPattern,
			CacheSize: 1000,
		},
		Firewall: FirewallConfig{
			SetName:                "UAmask_bypass_set",
			Backend:                "ipt",
			NonHTTPThreshold:       5,
			Timeout:                8 * 3600,
			DecisionDelay:          60 * time.Second,
			HTTPCooldownPeriod:     time.Hour,
			ProfileCleanupInterval: defaultProfileCleanup,
			ImmediateBypassTimeout: defaultImmediateBypassExpiry,
		},
		Observe: ObserveConfig{
			LogLevel:      "info",
			StatsFilePath: defaultStatsFile,
			StatsInterval: defaultStatsInterval,
		},
		Performance: PerformanceConfig{
			Profile:   "custom",
			GCPercent: 100,
		},
	}
}
