package config

import "time"

const (
	defaultPort          = 8080
	defaultBufferSize    = 8192
	defaultStatsFile     = "/tmp/UAmask.stats"
	defaultStatsInterval = 5 * time.Second
	defaultDialTimeout   = 30 * time.Second
	defaultKeepAlive     = 3 * time.Minute
	// 默认空闲超时 5 分钟。取这个值的原因：
	//   - 浏览器 keep-alive 空闲连接通常 ≤ 2 分钟即被回收，5 分钟不会误杀
	//   - 视频/下载等大数据流期间不会触发（有持续数据流动）
	//   - 僵死连接最多占用 worker 5 分钟，200 池在极端情况下也能自我恢复
	defaultIdleTimeout           = 5 * time.Minute
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
			IdleTimeout:       defaultIdleTimeout,
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
