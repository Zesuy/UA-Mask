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

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	version              = "0.2.2"
	userAgent            string
	port                 int
	logLevel             string
	showVer              bool
	force_replace        bool
	enablePartialReplace bool
	cache                *expirable.LRU[string, string]
	uaCache              *expirable.LRU[string, string]
	uaPattern            string
	uaRegexp             *regexp.Regexp
	logFile              string
	HTTP_METHOD          = []string{"GET", "POST", "HEAD", "PUT", "DELETE", "OPTIONS", "TRACE", "CONNECT"}
	whitelist            = []string{
		"MicroMessenger Client",
		"ByteDancePcdn",
		"Go-http-client/1.1",
	}
	statsActiveConnections atomic.Uint64 // 当前活跃连接数
	statsHttpRequests      atomic.Uint64 // 已处理 HTTP 请求总数
	statsRegexHits         atomic.Uint64 // 正则命中总数
	statsModifiedRequests  atomic.Uint64 // 成功篡改总数

	// bufio.Reader 池
	bufioReaderPool = sync.Pool{
		New: func() any {
			return bufio.NewReaderSize(nil, 16*1024)
		},
	}

	//io.Copy 使用的缓冲区
	bufferPool = sync.Pool{
		New: func() any {
			buf := make([]byte, 32*1024)
			return &buf
		},
	}
)

func startStatsWriter(filePath string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		// 原子地读取所有计数器
		httpRequests := statsHttpRequests.Load()
		regexHits := statsRegexHits.Load()
		modified := statsModifiedRequests.Load()
		activeConn := statsActiveConnections.Load()

		// 格式化为简单的 key:value 格式
		content := fmt.Sprintf(
			"active_connections:%d\n"+
				"http_requests:%d\n"+
				"regex_hits:%d\n"+
				"modifications_done:%d\n",
			activeConn,
			httpRequests,
			regexHits,
			modified,
		)

		err := os.WriteFile(filePath, []byte(content), 0644)
		if err != nil {
			logrus.Warnf("Failed to write stats file: %v", err)
		}
	}
}

func main() {
	// 解析命令行参数
	flag.StringVar(&userAgent, "ua", "FFF", "User-Agent string")
	flag.IntVar(&port, "port", 8080, "TPROXY listen port")
	flag.StringVar(&logLevel, "loglevel", "info", "Log level (debug, info, warn, error)")
	flag.BoolVar(&showVer, "v", false, "Show version")
	flag.BoolVar(&force_replace, "force", false, "Force replace User-Agent, ignore whitelist and regex pattern")
	flag.BoolVar(&enablePartialReplace, "s", false, "Enable Regex Partial Replace")
	flag.StringVar(&logFile, "log", "", "Log file path (e.g., /tmp/ua3f-tproxy.log). Default is stdout.")
	flag.StringVar(&uaPattern, "r", "(iPhone|iPad|Android|Macintosh|Windows|Linux|Apple|Mac OS X|Mobile)", "UA-Pattern (Regex)")
	flag.Parse()
	// 编译 UA 正则表达式
	uaPattern = "(?i)" + uaPattern
	var err error
	uaRegexp, err = regexp.Compile(uaPattern)
	if err != nil {
		logrus.Fatalf("Invalid User-Agent Regex Pattern: %v", err)
	}
	// 显示版本
	if showVer {
		fmt.Printf("UA3F-TPROXY v%s\n", version)
		return
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
	cache = expirable.NewLRU[string, string](300, nil, time.Second*600)
	//key: originUa, value: finalUa
	uaCache = expirable.NewLRU[string, string](1000, nil, time.Second*600)

	// 打印配置信息
	logrus.Infof("UA3F-TPROXY v%s", version)
	logrus.Infof("Port: %d", port)
	logrus.Infof("User-Agent: %s", userAgent)
	logrus.Infof("User-Agent Regex Pattern: %s", uaPattern)
	logrus.Infof("Enable Partial Replace: %v", enablePartialReplace)
	logrus.Infof("Log level: %s", logLevel)

	// 监听端口
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(0, 0, 0, 0), Port: port})
	if err != nil {
		logrus.Fatalf("Listen failed: %v", err)
	}
	defer listener.Close()

	logrus.Infof("REDIRECT proxy server listening on 0.0.0.0:%d", port)

	go startStatsWriter("/tmp/ua3f-tproxy.stats", 5*time.Second)

	for {
		conn, err := listener.AcceptTCP()
		if err != nil {
			logrus.Errorf("Accept error: %v", err)
			continue
		}

		go handleConnection(conn)
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
	if cache.Contains(destAddrPort) {
		// 命中缓存，直接转发
		logrus.Debugf("[%s] Hit LRU cache, direct relaying Client -> Server", destAddrPort)
		go func() {
			defer serverConn.CloseWrite()
			// 使用带缓冲区的 Copy
			buf := bufferPool.Get().([]byte)
			io.CopyBuffer(serverConn, clientConn, buf)
			bufferPool.Put(buf) // 放回池中
			done <- struct{}{}
		}()
	} else {
		// 未命中缓存，进行 UA 修改
		logrus.Debugf("[%s] not a cached https processing Client -> Server", destAddrPort)
		go func() {
			defer serverConn.CloseWrite()
			modifyAndForward(serverConn, clientConn, destAddrPort)
			done <- struct{}{}
		}()
	}

	// 服务器 -> 客户端 (直接转发)
	go func() {
		defer clientConn.CloseWrite()
		// 使用带缓冲区的 Copy
		buf := bufferPool.Get().([]byte)
		io.CopyBuffer(clientConn, serverConn, buf)
		bufferPool.Put(buf) // 放回池中
		done <- struct{}{}
	}()

	// 等待任意一个方向结束
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

	// 1. 检查是否是 HTTP 请求
	is_http, err := isHTTP(srcReader)
	if err != nil {
		if strings.Contains(err.Error(), "use of closed network connection") {
			logrus.Warnf("[%s] isHTTP error: %s", destAddrPort, err.Error())
			return
		}
	}
	if !is_http && err == nil {
		logrus.Debugf("[%s] Not HTTP, direct relay. Adding to LRU cache.", destAddrPort)
		cache.Add(destAddrPort, destAddrPort)

		buf := bufferPool.Get().([]byte)
		io.CopyBuffer(dst, srcReader, buf)
		bufferPool.Put(buf)
		return
	}

	logrus.Debugf("[%s] HTTP detected, processing with go prase", destAddrPort)

	for {
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
					statsModifiedRequests.Add(1)
					logrus.Debugf("[%s] UA modified (cached): %s -> %s", destAddrPort, uaStr, finalUA)
				} else {
					logrus.Debugf("[%s] UA not modified (cached): %s", destAddrPort, uaStr)
				}
			} else {
				// 未命中 UA 缓存，继续下面的逻辑
				var shouldReplace bool
				if force_replace {
					// 强制替换模式：忽略白名单和正则
					shouldReplace = true
					logrus.Debugf("[%s] Force replacing User-Agent: %s", destAddrPort, uaStr)
				} else {
					// 默认模式：检查白名单和正则
					isInWhiteList := false
					for _, v := range whitelist {
						if v == uaStr {
							isInWhiteList = true
							break
						}
					}

					isMatchUaPattern := true // 默认为 true
					if uaRegexp != nil {
						isMatchUaPattern = uaRegexp.MatchString(uaStr)
					}

					if isMatchUaPattern && uaFound {
						statsRegexHits.Add(1)
					}

					shouldReplace = !isInWhiteList && isMatchUaPattern

					if !shouldReplace {
						// 记录不替换的原因并加入缓存
						if isInWhiteList {
							logrus.Debugf("[%s] Hit User-Agent Whitelist: %s. ", destAddrPort, uaStr)
						} else { // 意味着 !isMatchUaPattern
							logrus.Debugf("[%s] Not Hit User-Agent Pattern: %s. ", destAddrPort, uaStr)
						}
						uaCache.Add(uaStr, uaStr) //缓存不修改的UA
					} else {
						logrus.Debugf("[%s] Hit User-Agent: %s", destAddrPort, uaStr)
					}
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
		if err = request.Write(dst); err != nil {
			logrus.Errorf("[%s] Write modified headers error: %v", destAddrPort, err)
			request.Body.Close()
			return
		}

		// 8. 关闭 Body，准备读取下一个 Keep-Alive 请求
		request.Body.Close()
		bodySize := request.ContentLength
		logrus.Debugf("[%s] Request processed, body size: %d. Waiting for next request...", destAddrPort, bodySize)
	}
}
