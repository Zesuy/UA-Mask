//go:build linux
// +build linux

package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	version              = "0.4.0"
	userAgent            string
	port                 int
	logLevel             string
	showVer              bool
	force_replace        bool
	enablePartialReplace bool
	uaCache              *lru.Cache[string, string]
	uaPattern            string
	uaRegexp             *regexp.Regexp
	logFile              string
	HTTP_METHOD          = []string{"GET", "POST", "HEAD", "PUT", "DELETE", "OPTIONS", "TRACE", "CONNECT", "PATCH"}
	whitelistArg         string
	whitelist            = []string{
		// 默认空白名单
	}
	statsActiveConnections atomic.Uint64 // 当前活跃连接数
	statsHttpRequests      atomic.Uint64 // 已处理 HTTP 请求总数
	statsModifiedRequests  atomic.Uint64 // 成功篡改总数
	statsCacheHits         atomic.Uint64 // 缓存命中(修改)
	statsCacheHitNoModify  atomic.Uint64 // 缓存命中(放行)

	keywords     string
	keywordsList []string
	enableRegex  bool
	cacheSize    int
	bufferSize   int
	poolSize     int

	// bufio.Reader 池
	bufioReaderPool = sync.Pool{
		New: func() any {
			return bufio.NewReaderSize(nil, 16*1024)
		},
	}
	// bufio.Writer 池
	bufioWriterPool = sync.Pool{
		New: func() any {
			// 默认大小，将在 main 中根据 bufferSize 重新初始化
			return bufio.NewWriterSize(nil, 16*1024)
		},
	}
)

func startStatsWriter(filePath string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastHttpRequests uint64
	lastCheckTime := time.Now()

	for range ticker.C {
		activeConn := statsActiveConnections.Load()
		httpRequests := statsHttpRequests.Load()
		modified := statsModifiedRequests.Load()
		cacheHitModify := statsCacheHits.Load()
		cacheHitPass := statsCacheHitNoModify.Load()

		// --- 2. 计算派生指标 ---

		// 处理速率 (RPS)
		now := time.Now()
		intervalSeconds := now.Sub(lastCheckTime).Seconds()
		var rps float64
		if intervalSeconds > 0 {
			requestsSinceLast := httpRequests - lastHttpRequests
			rps = float64(requestsSinceLast) / intervalSeconds
		}
		// 更新下次计算所需的状态
		lastHttpRequests = httpRequests
		lastCheckTime = now

		// 总缓存命中 = 缓存命中(修改) + 缓存命中(放行)
		totalCacheHits := cacheHitModify + cacheHitPass

		// 规则处理 = 请求总数 - 总缓存命中
		var ruleProcessing uint64
		if httpRequests > totalCacheHits {
			ruleProcessing = httpRequests - totalCacheHits
		}

		// 直接放行 = 请求总数 - 成功修改
		var directPass uint64
		if httpRequests > modified {
			directPass = httpRequests - modified
		}

		// 总缓存率 = 总缓存命中 / 请求总数 * 100%
		var totalCacheRatio float64
		if httpRequests > 0 {
			totalCacheRatio = (float64(totalCacheHits) * 100) / float64(httpRequests)
		}

		content := fmt.Sprintf(
			"current_connections:%d\n"+
				"total_requests:%d\n"+
				"rps:%.2f\n"+
				"successful_modifications:%d\n"+
				"direct_passthrough:%d\n"+
				"rule_processing:%d\n"+
				"cache_hit_modify:%d\n"+
				"cache_hit_pass:%d\n"+
				"total_cache_ratio:%.2f\n",
			activeConn,
			httpRequests,
			rps,
			modified,
			directPass,
			ruleProcessing,
			cacheHitModify,
			cacheHitPass,
			totalCacheRatio,
		)

		err := os.WriteFile(filePath, []byte(content), 0644)
		if err != nil {
			logrus.Warnf("Failed to write stats file: %v", err)
		}
	}
}

func main() {
	flag.StringVar(&userAgent, "u", "FFF", "User-Agent string")
	flag.IntVar(&port, "port", 8080, "TPROXY listen port")
	flag.StringVar(&logLevel, "loglevel", "info", "Log level (debug, info, warn, error)")
	flag.BoolVar(&showVer, "v", false, "Show version")
	flag.StringVar(&logFile, "log", "", "Log file path (e.g., /tmp/UAmask.log). Default is stdout.")
	flag.StringVar(&whitelistArg, "w", "", "Comma-separated User-Agent whitelist")

	// 匹配模式相关
	flag.BoolVar(&force_replace, "force", false, "Force replace User-Agent (match_mode 'all')")
	flag.BoolVar(&enableRegex, "enable-regex", false, "Enable Regex matching mode")
	flag.StringVar(&keywords, "keywords", "iPhone,iPad,Android,Macintosh,Windows", "Comma-separated User-Agent keywords (default mode)")
	flag.StringVar(&uaPattern, "r", "(iPhone|iPad|Android|Macintosh|Windows|Linux|Apple|Mac OS X|Mobile)", "UA-Pattern (Regex)")
	flag.BoolVar(&enablePartialReplace, "s", false, "Enable Regex Partial Replace (regex mode + partial)")

	// 性能调优
	flag.IntVar(&cacheSize, "cache-size", 1000, "LRU cache size")
	flag.IntVar(&bufferSize, "buffer-size", 8192, "I/O buffer size (bytes)")
	flag.IntVar(&poolSize, "p", 0, "Worker pool size (0 or less = one goroutine per connection)")

	flag.Parse()

	if whitelistArg != "" {
		parts := strings.Split(whitelistArg, ",")
		trimmed := make([]string, 0, len(parts))
		for _, s := range parts {
			s = strings.TrimSpace(s)
			if s != "" {
				trimmed = append(trimmed, s)
			}
		}
		if len(trimmed) > 0 {
			whitelist = trimmed
		}
	}

	if enableRegex {
		uaPattern = "(?i)" + uaPattern
		var err error
		uaRegexp, err = regexp.Compile(uaPattern)
		if err != nil {
			logrus.Fatalf("Invalid User-Agent Regex Pattern: %v", err)
		}
	} else if !force_replace {
		// 默认：关键词模式
		parts := strings.Split(keywords, ",")
		trimmed := make([]string, 0, len(parts))
		for _, s := range parts {
			s = strings.TrimSpace(s)
			if s != "" {
				trimmed = append(trimmed, s)
			}
		}
		if len(trimmed) > 0 {
			keywordsList = trimmed
		}
	}

	if logFile != "" {
		// 如果指定了 -log 文件路径，则使用 lumberjack 进行文件滚动日志
		logFileRotator := &lumberjack.Logger{
			Filename:   logFile,
			MaxSize:    1,
			MaxBackups: 3,
			MaxAge:     7,
			Compress:   false,
		}
		logrus.SetOutput(logFileRotator)
	} else {
		// 否则，输出到标准输出 (stdout)
		logrus.SetOutput(os.Stdout)
	}

	//loglevel
	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		logrus.Warnf("Invalid log level '%s', using 'info'", logLevel)
		level = logrus.InfoLevel
	}
	logrus.SetLevel(level)
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
	// 初始化 Reader 池
	bufioReaderPool = sync.Pool{
		New: func() any {
			return bufio.NewReaderSize(nil, bufferSize)
		},
	}
	// 初始化 Writer 池
	bufioWriterPool = sync.Pool{
		New: func() any {
			return bufio.NewWriterSize(nil, bufferSize)
		},
	}

	//key: originUa, value: finalUa
	uaCache, err = lru.New[string, string](cacheSize)
	if err != nil {
		logrus.Fatalf("Failed to create LRU cache: %v", err)
	}

	// 打印配置信息
	logrus.Infof("UA-MASK v%s", version)
	logrus.Infof("Port: %d", port)
	logrus.Infof("User-Agent: %s", userAgent)
	logrus.Infof("Log level: %s", logLevel)
	logrus.Infof("User-Agent Whitelist: %v", whitelist)
	logrus.Infof("Cache Size: %d", cacheSize)
	logrus.Infof("Buffer Size: %d", bufferSize)

	if force_replace {
		logrus.Infof("Mode: Force Replace (All)")
	} else if enableRegex {
		logrus.Infof("Mode: Regex")
		logrus.Infof("User-Agent Regex Pattern: %s", uaPattern)
		logrus.Infof("Enable Partial Replace: %v", enablePartialReplace)
	} else {
		logrus.Infof("Mode: Keywords")
		logrus.Infof("Keywords: %v", keywordsList)
	}

	// 监听端口
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(0, 0, 0, 0), Port: port})
	if err != nil {
		logrus.Fatalf("Listen failed: %v", err)
	}
	defer listener.Close()

	logrus.Infof("REDIRECT proxy server listening on 0.0.0.0:%d", port)

	go startStatsWriter("/tmp/UAmask.stats", 5*time.Second)

	if poolSize > 0 {
		// --- Worker Pool 模式 ---
		logrus.Infof("Starting in Worker Pool Mode (size: %d)", poolSize)
		connChan := make(chan *net.TCPConn, poolSize)

		// 启动指定数量的 worker goroutine
		for i := 0; i < poolSize; i++ {
			go func(workerID int) {
				for conn := range connChan {
					logrus.Debugf("Worker %d processing connection from %s", workerID, conn.RemoteAddr())
					handleConnection(conn)
				}
				logrus.Debugf("Worker %d stopping", workerID)
			}(i)
		}

		// Accept 循环 (生产者)
		for {
			conn, err := listener.AcceptTCP()
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Temporary() {
					logrus.Warnf("Temporary accept error: %v; sleeping for 5ms", err)
					time.Sleep(5 * time.Millisecond)
					continue
				}
				logrus.Errorf("Accept error: %v", err)
				continue
			}
			connChan <- conn
		}

	} else {
		// --- 默认模式---
		logrus.Info("Starting in Default Mode (one goroutine per connection)")
		for {
			conn, err := listener.AcceptTCP()
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Temporary() {
					logrus.Warnf("Temporary accept error: %v; sleeping for 5ms", err)
					time.Sleep(5 * time.Millisecond)
					continue
				}
				logrus.Errorf("Accept error: %v", err)
				continue
			}
			go handleConnection(conn)
		}
	}
}

func handleConnection(clientConn *net.TCPConn) {
	statsActiveConnections.Add(1)
	defer func() {
		statsActiveConnections.Add(^uint64(0))
		clientConn.Close()
	}()

	// 获取原始目标地址
	originalDst, err := getOriginalDst(clientConn)
	if err != nil {
		logrus.Debugf("Failed to get original destination: %v", err)
		return
	}

	destAddrPort := originalDst.String()
	clientAddr := clientConn.RemoteAddr()
	logrus.Debugf("Connection: %s -> %s (original: %s)",
		clientAddr.String(),
		clientConn.LocalAddr().String(),
		destAddrPort)

	// 连接到原始目标
	serverConn, err := net.DialTCP("tcp", nil, originalDst)
	if err != nil {
		logrus.Debugf("Failed to connect to %s: %v", destAddrPort, err)
		return
	}
	defer serverConn.Close()

	// 双向转发数据
	done := make(chan struct{}, 2)

	// 客户端 -> 服务器 (需要修改 UA)
	go func() {
		defer serverConn.CloseWrite()
		modifyAndForward(serverConn, clientConn, destAddrPort)
		done <- struct{}{}
	}()

	// 服务器 -> 客户端 (直接转发)
	go func() {
		defer clientConn.CloseWrite()
		io.Copy(clientConn, serverConn)
		done <- struct{}{}
	}()

	// 改为等待两个方向的转发完成，防止竞态
	<-done
	<-done
}

// getOriginalDst 获取被 REDIRECT 规则重定向前的原始目标地址
// 使用 SO_ORIGINAL_DST socket 选项，这是 iptables REDIRECT 目标填充的
func getOriginalDst(conn *net.TCPConn) (*net.TCPAddr, error) {
	file, err := conn.File()
	if err != nil {
		return nil, fmt.Errorf("failed to get file descriptor: %w", err)
	}
	defer file.Close()

	fd := int(file.Fd())

	// SO_ORIGINAL_DST = 80
	const SO_ORIGINAL_DST = 80

	// 使用 sockaddr 结构获取原始目标地址
	var addr unix.RawSockaddrInet4
	addrLen := uint32(unsafe.Sizeof(addr))

	_, _, errno := unix.Syscall6(
		unix.SYS_GETSOCKOPT,
		uintptr(fd),
		uintptr(unix.SOL_IP),
		uintptr(SO_ORIGINAL_DST),
		uintptr(unsafe.Pointer(&addr)),
		uintptr(unsafe.Pointer(&addrLen)),
		0,
	)

	if errno != 0 {
		return nil, fmt.Errorf("getsockopt SO_ORIGINAL_DST failed: %v", errno)
	}

	// 解析 IPv4 地址和端口
	ip := net.IPv4(addr.Addr[0], addr.Addr[1], addr.Addr[2], addr.Addr[3])
	port := int(addr.Port>>8 | addr.Port<<8)
	logrus.Debugf("getOriginalDst raw value: %d, parsed port: %d", addr.Port, port)
	return &net.TCPAddr{
		IP:   ip,
		Port: port,
	}, nil
}

// 检查是否是 HTTP 请求
func isHTTP(reader *bufio.Reader) (bool, error) {
	buf, err := reader.Peek(7)
	if err != nil {
		if strings.Contains(err.Error(), "EOF") {
			logrus.Debug(fmt.Sprintf("Peek EOF: %s", err.Error()))
		} else {
			logrus.Debug(fmt.Sprintf("Peek error: %s", err.Error()))
		}
		return false, err
	}
	hint := string(buf)
	is_http := false
	for _, v := range HTTP_METHOD {
		if strings.HasPrefix(hint, v) {
			is_http = true
			break
		}
	}
	return is_http, nil
}

// buildNewUA 根据是否启用部分替换来构造新的 User-Agent 字符串
func buildNewUA(originUA string, replacementUA string, uaRegexp *regexp.Regexp, enablePartialReplace bool) string {
	if enablePartialReplace && uaRegexp != nil {
		// 启用部分替换：使用正则替换
		newUaHearder := uaRegexp.ReplaceAllString(originUA, replacementUA)
		return newUaHearder
	}
	// 默认完整替换
	return replacementUA
}

// 修改 User-Agent 并转发数据
func modifyAndForward(dst net.Conn, src net.Conn, destAddrPort string) {
	srcReader := bufioReaderPool.Get().(*bufio.Reader)
	srcReader.Reset(src)
	defer bufioReaderPool.Put(srcReader)

	dstWriter := bufioWriterPool.Get().(*bufio.Writer)
	dstWriter.Reset(dst)
	defer func() {
		dstWriter.Flush()
		bufioWriterPool.Put(dstWriter)
	}()
	logrus.Debugf("[%s] HTTP detected, processing with go prase", destAddrPort)

	for {
		is_http, err := isHTTP(srcReader)
		if err != nil {
			if err == io.EOF || strings.Contains(err.Error(), "use of closed network connection") {
				logrus.Debugf("[%s] Connection closed (EOF or closed in loop)", destAddrPort)
			} else {
				logrus.Debugf("[%s] isHTTP check in loop error: %v", destAddrPort, err)
			}

			if err_flush := dstWriter.Flush(); err_flush != nil {
				logrus.Debugf("[%s] Flush error before fallback (isHTTP err): %v", destAddrPort, err_flush)
			}
			io.Copy(dst, srcReader)
			return
		}

		if !is_http {
			logrus.Debugf("[%s] Protocol switch detected. Changing to direct relay mode.", destAddrPort)

			if err_flush := dstWriter.Flush(); err_flush != nil {
				logrus.Debugf("[%s] Flush error before fallback (isHTTP err): %v", destAddrPort, err_flush)
			}
			io.Copy(dst, srcReader)
			return
		}
		// 3. 使用 Go 标准库解析 HTTP 头部
		request, err := http.ReadRequest(srcReader)
		if err != nil {
			// 如果是 EOF 或连接关闭，是正常退出
			if err == io.EOF || strings.Contains(err.Error(), "use of closed network connection") {
				logrus.Debugf("[%s] Connection closed (EOF or closed)", destAddrPort)
			} else if strings.Contains(err.Error(), "connection reset by peer") {
				logrus.Debugf("[%s] Connection reset", destAddrPort)
			} else {
				// 其他解析错误
				logrus.Debugf("[%s] HTTP read request error: %v", destAddrPort, err)
			}
			return
		}

		// 统计 HTTP 请求
		statsHttpRequests.Add(1)

		// 4. 获取 User-Agent
		uaStr := request.Header.Get("User-Agent")
		uaFound := uaStr != ""

		if !uaFound {
			logrus.Debugf("[%s] No User-Agent header, skip modification.", destAddrPort)
		} else {
			if finalUA, ok := uaCache.Get(uaStr); ok {
				// 命中 UA 缓存，直接使用缓存的 finalUA
				request.Header.Set("User-Agent", finalUA)
				if finalUA != uaStr {
					statsCacheHits.Add(1)
					statsModifiedRequests.Add(1)
					logrus.Debugf("[%s] UA modified (cached): %s -> %s", destAddrPort, uaStr, finalUA)
				} else {
					statsCacheHitNoModify.Add(1)
					logrus.Debugf("[%s] UA not modified (cached): %s", destAddrPort, uaStr)
				}
			} else {
				// 未命中 UA 缓存，执行完整匹配逻辑
				var shouldReplace bool
				var matchReason string

				// 1. 检查白名单 (最高优先级)
				isInWhiteList := false
				for _, v := range whitelist {
					if v == uaStr {
						isInWhiteList = true
						break
					}
				}

				if isInWhiteList {
					shouldReplace = false
					matchReason = "Hit User-Agent Whitelist"
				} else {
					// 2. 根据模式进行匹配
					if force_replace {
						// 强制模式
						shouldReplace = true
						matchReason = "Force Replace Mode"
					} else if enableRegex {
						// 正则模式
						if uaRegexp != nil && uaRegexp.MatchString(uaStr) {
							shouldReplace = true
							matchReason = "Hit User-Agent Pattern"
						} else {
							shouldReplace = false
							matchReason = "Not Hit User-Agent Pattern"
						}
					} else {
						// 默认：关键词模式
						shouldReplace = false
						matchReason = "Not Hit User-Agent Keywords"
						for _, keyword := range keywordsList {
							if strings.Contains(uaStr, keyword) {
								shouldReplace = true
								matchReason = "Hit User-Agent Keyword"
								break
							}
						}
					}
				}

				// 3. 处理日志和缓存
				if !shouldReplace {
					logrus.Debugf("[%s] %s: %s. ", destAddrPort, matchReason, uaStr)
					uaCache.Add(uaStr, uaStr) // 缓存不修改的UA
				} else {
					logrus.Debugf("[%s] %s: %s", destAddrPort, matchReason, uaStr)
				}

				// 根据 shouldReplace 标志执行操作
				if shouldReplace {
					// 调用 buildNewUA 来获取最终的 UA 字符串
					// uaStr 是原始 UA 值, e.g., "Mozilla/5.0 (iPhone; ...)"
					// userAgent 是替换字符串, e.g., "FFF"
					finalUA := buildNewUA(uaStr, userAgent, uaRegexp, enablePartialReplace)

					// 使用 .Set() 来修改 Header
					request.Header.Set("User-Agent", finalUA)

					statsModifiedRequests.Add(1)

					uaCache.Add(uaStr, finalUA) //缓存修改后的UA

					if force_replace {
						logrus.Debugf("[%s] UA modified (forced): %s -> %s", destAddrPort, uaStr, finalUA)
					} else {
						if enablePartialReplace && finalUA != userAgent {
							logrus.Debugf("[%s] UA partially modified: %s -> %s", destAddrPort, uaStr, finalUA)
						} else {
							logrus.Debugf("[%s] UA fully modified: %s -> %s", destAddrPort, uaStr, finalUA)
						}
					}
				}
			}
		} // 循环，回到第 3 步，等待下一个 http.ReadRequest
		// 6. 将头部写回目标
		if err := request.Write(dstWriter); err != nil {
			logrus.Debugf("[%s] HTTP write request error: %v", destAddrPort, err)
			request.Body.Close()
			return
		}
		if err := dstWriter.Flush(); err != nil {
			logrus.Debugf("[%s] Flush error after writing request: %v", destAddrPort, err)
			request.Body.Close()
			return
		}

		// 8. 关闭 Body，准备读取下一个 Keep-Alive 请求
		request.Body.Close()
		bodySize := request.ContentLength
		logrus.Debugf("[%s] Request processed, body size: %d. Waiting for next request...", destAddrPort, bodySize)
	}
}
