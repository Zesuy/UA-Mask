package main

import (
	"flag"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// Config 结构体保存所有应用配置
type Config struct {
	UserAgent                  string
	Port                       int
	LogLevel                   string
	ShowVer                    bool
	LogFile                    string
	Whitelist                  []string
	ForceReplace               bool
	EnableRegex                bool
	EnablePartialReplace       bool
	KeywordsList               []string
	UAPattern                  string
	UARegexp                   *regexp.Regexp
	CacheSize                  int
	BufferSize                 int
	PoolSize                   int
	FirewallUAWhitelist        []string      // 防火墙 UA 白名单
	EnableFirewallUABypass     bool          // 启用防火墙非 HTTP 绕过
	FirewallIPSetName          string        // 防火墙 set 名称
	FirewallType               string        // 防火墙类型 (ipt or nft)
	FirewallDropOnMatch        bool          // 防火墙匹配时断开连接
	FirewallNonHttpThreshold   int           // 防火墙非 HTTP 判定阈值
	FirewallTimeout            int           // 防火墙规则超时时间 (秒)
	FirewallDecisionDelay      time.Duration // 防火墙决策延迟时间
	FirewallHttpCooldownPeriod time.Duration // 防火墙 HTTP 冷却时间
}

func NewConfig() (*Config, error) {
	var (
		userAgent                  string
		port                       int
		logLevel                   string
		showVer                    bool
		forceReplace               bool
		enablePartialReplace       bool
		uaPattern                  string
		logFile                    string
		whitelistArg               string
		keywords                   string
		enableRegex                bool
		cacheSize                  int
		bufferSize                 int
		poolSize                   int
		firewallUAWhitelistArg     string
		enableFirewallUABypass     bool
		firewallIPSetName          string
		firewallType               string
		firewallDropOnMatch        bool
		firewallNonHttpThreshold   int
		firewallTimeout            int
		firewallDecisionDelay      time.Duration
		firewallHttpCooldownPeriod time.Duration
	)

	// 2. 注册 flag
	flag.StringVar(&userAgent, "u", "FFF", "User-Agent string")
	flag.IntVar(&port, "port", 8080, "TPROXY listen port")
	flag.StringVar(&logLevel, "loglevel", "info", "Log level (debug, info, warn, error)")
	flag.BoolVar(&showVer, "v", false, "Show version")
	flag.StringVar(&logFile, "log", "", "Log file path (e.g., /tmp/UAmask.log). Default is stdout.")
	flag.StringVar(&whitelistArg, "w", "", "Comma-separated User-Agent whitelist")

	// 匹配模式
	flag.BoolVar(&forceReplace, "force", false, "Force replace User-Agent (match_mode 'all')")
	flag.BoolVar(&enableRegex, "enable-regex", false, "Enable Regex matching mode")
	flag.StringVar(&keywords, "keywords", "iPhone,iPad,Android,Macintosh,Windows", "Comma-separated User-Agent keywords (default mode)")
	flag.StringVar(&uaPattern, "r", "(iPhone|iPad|Android|Macintosh|Windows|Linux|Apple|Mac OS X|Mobile)", "UA-Pattern (Regex)")
	flag.BoolVar(&enablePartialReplace, "s", false, "Enable Regex Partial Replace (regex mode + partial)")

	// 性能调优
	flag.IntVar(&cacheSize, "cache-size", 1000, "LRU cache size")
	flag.IntVar(&bufferSize, "buffer-size", 8192, "I/O buffer size (bytes)")
	flag.IntVar(&poolSize, "p", 0, "Worker pool size (0 or less = one goroutine per connection)")

	// 防火墙绕过
	flag.StringVar(&firewallUAWhitelistArg, "fw-ua-w", "", "Comma-separated User-Agent firewall whitelist keywords")
	flag.BoolVar(&enableFirewallUABypass, "fw-bypass", false, "Enable firewall bypass for non-HTTP traffic")
	flag.StringVar(&firewallIPSetName, "fw-set-name", "UAmask_bypass_set", "Firewall ipset/nfset name")
	flag.StringVar(&firewallType, "fw-type", "ipt", "Firewall type (ipt or nft)")
	flag.BoolVar(&firewallDropOnMatch, "fw-drop", false, "Drop connections that match firewall rules")

	flag.IntVar(&firewallNonHttpThreshold, "fw-nonhttp-threshold", 5, "Firewall non-HTTP traffic threshold")
	flag.IntVar(&firewallTimeout, "fw-timeout", 8*3600, "Firewall rule timeout in seconds")
	flag.DurationVar(&firewallDecisionDelay, "fw-decision-delay", 60*time.Second, "Firewall decision delay duration")
	flag.DurationVar(&firewallHttpCooldownPeriod, "fw-http-cooldown", 1*time.Hour, "Firewall HTTP cooldown period")

	// 3. 解析 flag
	flag.Parse()

	// 4. 结构体
	cfg := &Config{
		UserAgent:            userAgent,
		Port:                 port,
		LogLevel:             logLevel,
		ShowVer:              showVer,
		LogFile:              logFile,
		ForceReplace:         forceReplace,
		EnableRegex:          enableRegex,
		EnablePartialReplace: enablePartialReplace,
		CacheSize:            cacheSize,
		BufferSize:           bufferSize,
		PoolSize:             poolSize,
		Whitelist:            []string{},
		KeywordsList:         []string{},

		FirewallUAWhitelist:        []string{},
		EnableFirewallUABypass:     enableFirewallUABypass,
		FirewallIPSetName:          firewallIPSetName,
		FirewallType:               firewallType,
		FirewallDropOnMatch:        firewallDropOnMatch,
		FirewallNonHttpThreshold:   firewallNonHttpThreshold,
		FirewallTimeout:            firewallTimeout,
		FirewallDecisionDelay:      firewallDecisionDelay,
		FirewallHttpCooldownPeriod: firewallHttpCooldownPeriod,
	}

	// 处理白名单
	if whitelistArg != "" {
		parts := strings.Split(whitelistArg, ",")
		for _, s := range parts {
			s = strings.TrimSpace(s)
			if s != "" {
				cfg.Whitelist = append(cfg.Whitelist, s)
			}
		}
	}

	// 防火墙 UA 白名单
	if firewallUAWhitelistArg != "" {
		parts := strings.Split(firewallUAWhitelistArg, ",")
		for _, s := range parts {
			s = strings.TrimSpace(s)
			if s != "" {
				cfg.FirewallUAWhitelist = append(cfg.FirewallUAWhitelist, s)
			}
		}
	}

	// 验证配置
	if cfg.Port < 1 || cfg.Port > 65535 {
		return nil, fmt.Errorf("invalid port: %d", cfg.Port)
	}
	if cfg.BufferSize < 1024 || cfg.BufferSize > 65536 {
		return nil, fmt.Errorf("invalid buffer size: %d", cfg.BufferSize)
	}
	if cfg.CacheSize < 0 {
		return nil, fmt.Errorf("invalid cache size: %d", cfg.CacheSize)
	}

	// 根据模式处理 keywords 或 regex
	if cfg.EnableRegex {
		// 正则模式
		cfg.UAPattern = "(?i)" + uaPattern
		var err error
		cfg.UARegexp, err = regexp.Compile(cfg.UAPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid User-Agent Regex Pattern: %w", err)
		}
	} else if !cfg.ForceReplace {
		parts := strings.Split(keywords, ",")
		for _, s := range parts {
			s = strings.TrimSpace(s)
			if s != "" {
				cfg.KeywordsList = append(cfg.KeywordsList, s)
			}
		}
	}

	// 6. 返回配置实例
	return cfg, nil
}

func (c *Config) LogConfig(version string) {
	logrus.Infof("UA-MASK v%s", version)
	logrus.Infof("Port: %d", c.Port)
	logrus.Infof("User-Agent: %s", c.UserAgent)
	logrus.Infof("Log level: %s", c.LogLevel)
	logrus.Infof("User-Agent Whitelist: %v", c.Whitelist)
	logrus.Infof("Cache Size: %d", c.CacheSize)
	logrus.Infof("Buffer Size: %d", c.BufferSize)
	logrus.Infof("Worker Pool Size: %d", c.PoolSize)

	// 日志
	logrus.Infof("Firewall Type: %s", c.FirewallType)
	logrus.Infof("Firewall IPSet Name: %s", c.FirewallIPSetName)
	logrus.Infof("Firewall UA Whitelist: %v", c.FirewallUAWhitelist)
	logrus.Infof("Enable Firewall Non-HTTP Bypass: %v", c.EnableFirewallUABypass)
	logrus.Infof("Firewall Drop On Match: %v", c.FirewallDropOnMatch)
	logrus.Infof("Firewall Non-HTTP Threshold: %d", c.FirewallNonHttpThreshold)
	logrus.Infof("Firewall Rule Timeout (seconds): %d", c.FirewallTimeout)
	logrus.Infof("Firewall Decision Delay: %s", c.FirewallDecisionDelay)
	logrus.Infof("Firewall HTTP Cooldown Period: %s", c.FirewallHttpCooldownPeriod)

	if c.ForceReplace {
		logrus.Info("Mode: Force Replace (All)")
	} else if c.EnableRegex {
		logrus.Infof("Mode: Regex | Pattern: %s | Partial Replace: %v", c.UAPattern, c.EnablePartialReplace)
	} else {
		logrus.Infof("Mode: Keywords | Keywords: %v", c.KeywordsList)
	}
}
