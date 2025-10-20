//go:build linux
// +build linux

package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
	"unsafe"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

var (
	version     = "0.1.0"
	userAgent   string
	port        int
	logLevel    string
	showVer     bool
	cache       *expirable.LRU[string, string]
	HTTP_METHOD = []string{"GET", "POST", "HEAD", "PUT", "DELETE", "OPTIONS", "TRACE", "CONNECT"}
	whitelist   = []string{
		"MicroMessenger Client",
		"ByteDancePcdn",
		"Go-http-client/1.1",
	}
)

func main() {
	// 解析命令行参数
	flag.StringVar(&userAgent, "ua", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36", "User-Agent string")
	flag.IntVar(&port, "port", 8080, "TPROXY listen port")
	flag.StringVar(&logLevel, "loglevel", "info", "Log level (debug, info, warn, error)")
	flag.BoolVar(&showVer, "v", false, "Show version")
	flag.Parse()

	// 显示版本
	if showVer {
		fmt.Printf("UA3F-TPROXY v%s\n", version)
		return
	}

	// 设置日志级别
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

	// 打印配置信息
	logrus.Infof("UA3F-TPROXY v%s", version)
	logrus.Infof("Port: %d", port)
	logrus.Infof("User-Agent: %s", userAgent)
	logrus.Infof("Log level: %s", logLevel)

	// 监听端口
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(0, 0, 0, 0), Port: port})
	if err != nil {
		logrus.Fatalf("Listen failed: %v", err)
	}
	defer listener.Close()

	logrus.Infof("REDIRECT proxy server listening on 0.0.0.0:%d", port)

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
	defer clientConn.Close()

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
			io.Copy(serverConn, clientConn)
			done <- struct{}{}
		}()
	} else {
		// 未命中缓存，进行 UA 修改
		logrus.Debugf("[%s] Cache miss, processing Client -> Server", destAddrPort)
		go func() {
			defer serverConn.CloseWrite()
			modifyAndForward(serverConn, clientConn, destAddrPort)
			done <- struct{}{}
		}()
	}

	// 服务器 -> 客户端 (直接转发)
	go func() {
		defer clientConn.CloseWrite()
		io.Copy(clientConn, serverConn)
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

	// 解析 IPv4 地址和端口 (网络字节序)
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
			logrus.Error(fmt.Sprintf("Peek error: %s", err.Error()))
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

// 修改 User-Agent 并转发数据 (高性能流式版本)
func modifyAndForward(dst net.Conn, src net.Conn, destAddrPort string) {
	srcReader := bufio.NewReader(src)

	// 1. 检查是否是 HTTP 请求 (这个检查仍然是必要的)
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
		io.Copy(dst, srcReader) // 转发非 HTTP 数据
		return
	}

	logrus.Debugf("[%s] HTTP detected, processing with streaming parser...", destAddrPort)

	uaFound := false // 标记是否找到了 UA

	// 2. 开始逐行扫描 HTTP Headers
	for {
		// 3. 逐行读取 Header
		line, err := srcReader.ReadString('\n')
		if err != nil {
			if err != io.EOF && !strings.Contains(err.Error(), "use of closed network connection") {
				logrus.Debugf("[%s] Read header line error: %v", destAddrPort, err)
			} else {
				logrus.Debugf("[%s] Connection closed while reading headers", destAddrPort)
			}
			return // 连接关闭或读取错误
		}

		// 4. 检查是否是 Header 的结尾 (空行)
		if line == "\r\n" || line == "\n" {
			// Header 结束
			if !uaFound {
				// 整个 Header 都读完了，也没找到 User-Agent
				logrus.Debugf("[%s] No User-Agent header, skip modification. Adding to LRU cache.", destAddrPort)
				cache.Add(destAddrPort, destAddrPort)
			}

			// 将 Header 的结尾（空行）写入目标
			if _, err = io.WriteString(dst, line); err != nil {
				logrus.Errorf("[%s] Write header end error: %v", destAddrPort, err)
				return
			}

			// 5. 【关键】快速转发剩余所有数据
			// 这会转发请求体 (Request Body) 以及此 TCP 连接上
			// 后续所有的 Keep-Alive 请求，不再进行任何解析。
			logrus.Debugf("[%s] Header end, starting fast relay (io.Copy)", destAddrPort)
			io.Copy(dst, srcReader)
			return // 此连接处理完毕
		}

		// 6. 检查是否是 User-Agent 行 (高性能、不区分大小写)
		if len(line) > 11 && strings.EqualFold(line[:11], "user-agent:") {
			uaFound = true
			// 提取 UA 字符串
			uaStr := strings.TrimSpace(line[11:])

			// 检查白名单
			isInWhiteList := false
			for _, v := range whitelist {
				if v == uaStr {
					isInWhiteList = true
					break
				}
			}

			if isInWhiteList {
				logrus.Debugf("[%s] Hit User-Agent Whitelist: %s. Adding to LRU cache.", destAddrPort, uaStr)
				cache.Add(destAddrPort, destAddrPort)
				// 白名单，写入原始行
				if _, err = io.WriteString(dst, line); err != nil {
					logrus.Errorf("[%s] Write original UA line error: %v", destAddrPort, err)
					return
				}
			} else {
				// 不在白名单，执行修改
				logrus.Debugf("[%s] Hit User-Agent: %s", destAddrPort, uaStr)

				// 构造新行，同时必须保留原始的行尾 (CRLF 或 LF)
				var newLine string
				if strings.HasSuffix(line, "\r\n") {
					newLine = fmt.Sprintf("User-Agent: %s\r\n", userAgent)
				} else {
					newLine = fmt.Sprintf("User-Agent: %s\n", userAgent)
				}

				// 写入修改后的行
				if _, err = io.WriteString(dst, newLine); err != nil {
					logrus.Errorf("[%s] Write modified UA line error: %v", destAddrPort, err)
					return
				}
				logrus.Infof("[%s] UA modified: %s -> %s", destAddrPort, uaStr, userAgent)
			}
		} else {
			// 7. 不是 UA 行，也不是空行，说明是其他 Header，原样写入
			if _, err = io.WriteString(dst, line); err != nil {
				logrus.Errorf("[%s] Write header line error: %v", destAddrPort, err)
				return
			}
		}
	} // 循环读取下一行 Header
}
