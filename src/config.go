package main

import (
	"flag"
	"fmt"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"
)

// Config 结构体保存所有应用配置
type Config struct {
	UserAgent            string
	Port                 int
	LogLevel             string
	ShowVer              bool
	LogFile              string
	Whitelist            []string
	ForceReplace         bool
	EnableRegex          bool
	EnablePartialReplace bool
	KeywordsList         []string
	UAPattern            string
	UARegexp             *regexp.Regexp
	CacheSize            int
	BufferSize           int
	PoolSize             int
}

func NewConfig() (*Config, error) {
	var (
		userAgent            string
		port                 int
		logLevel             string
		showVer              bool
		forceReplace         bool
		enablePartialReplace bool
		uaPattern            string
		logFile              string
		whitelistArg         string
		keywords             string
		enableRegex          bool
		cacheSize            int
		bufferSize           int
		poolSize             int
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

	if c.ForceReplace {
		logrus.Infof("Mode: Force Replace (All)")
	} else if c.EnableRegex {
		logrus.Infof("Mode: Regex")
		logrus.Infof("User-Agent Regex Pattern: %s", c.UAPattern)
		logrus.Infof("Enable Partial Replace: %v", c.EnablePartialReplace)
	} else {
		logrus.Infof("Mode: Keywords")
		logrus.Infof("Keywords: %v", c.KeywordsList)
	}
}
