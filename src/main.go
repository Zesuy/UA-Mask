//go:build linux
// +build linux

package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"unsafe"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

var (
	version   = "0.1.0"
	userAgent string
	port      int
	logLevel  string
	showVer   bool
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

	if err := initTCPTProxy(listener); err != nil {
		logrus.Fatalf("Init TPROXY failed: %v", err)
	}

	logrus.Infof("TPROXY server listening on 0.0.0.0:%d", port)

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

	// 等待任意一个方向结束
	<-done
}
func initTCPTProxy(listener *net.TCPListener) error {
	file, err := listener.File()
	if err != nil {
		return fmt.Errorf("failed to get file descriptor: %w", err)
	}
	defer file.Close()

	fd := int(file.Fd())

	// 设置 IP_TRANSPARENT 选项
	if err := unix.SetsockoptInt(fd, unix.SOL_IP, unix.IP_TRANSPARENT, 1); err != nil {
		return fmt.Errorf("setsockopt IP_TRANSPARENT failed: %w", err)
	}

	return nil
}

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
	port := int(binary.BigEndian.Uint16([]byte{byte(addr.Port >> 8), byte(addr.Port & 0xff)}))

	return &net.TCPAddr{
		IP:   ip,
		Port: port,
	}, nil
}

// 检查是否是 HTTP 请求
func isHTTP(reader *bufio.Reader) bool {
	buf, err := reader.Peek(7)
	if err != nil {
		return false
	}
	hint := string(buf)
	httpMethods := []string{"GET", "POST", "HEAD", "PUT", "DELETE", "OPTIONS", "TRACE", "CONNECT"}
	for _, method := range httpMethods {
		if strings.HasPrefix(hint, method) {
			return true
		}
	}
	return false
}

// 修改 User-Agent 并转发数据
func modifyAndForward(dst net.Conn, src net.Conn, destAddrPort string) {
	srcReader := bufio.NewReader(src)

	// 检查是否是 HTTP 请求
	if !isHTTP(srcReader) {
		logrus.Debugf("[%s] Not HTTP, direct relay", destAddrPort)
		io.Copy(dst, srcReader)
		return
	}

	logrus.Debugf("[%s] HTTP detected, processing...", destAddrPort)

	// 处理 HTTP 请求
	for {
		request, err := http.ReadRequest(srcReader)
		if err != nil {
			if err == io.EOF {
				logrus.Debugf("[%s] Connection closed by client", destAddrPort)
			} else if strings.Contains(err.Error(), "use of closed network connection") {
				logrus.Debugf("[%s] Connection closed", destAddrPort)
			} else {
				logrus.Debugf("[%s] Read request error: %v", destAddrPort, err)
			}
			return
		}

		// 获取原始 UA
		originalUA := request.Header.Get("User-Agent")
		if originalUA == "" {
			logrus.Debugf("[%s] No User-Agent header, skip modification", destAddrPort)
		} else {
			// 修改 User-Agent (使用全局变量 userAgent)
			request.Header.Set("User-Agent", userAgent)
			logrus.Infof("[%s] UA modified: %s -> %s", destAddrPort, originalUA, userAgent)
		}

		// 写入修改后的请求
		err = request.Write(dst)
		if err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") {
				logrus.Debugf("[%s] Write failed: connection closed", destAddrPort)
			} else {
				logrus.Errorf("[%s] Write request error: %v", destAddrPort, err)
			}
			return
		}
	}
}
