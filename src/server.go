package main

import (
	"fmt"
	"io"
	"net"
	"time"

	"github.com/sirupsen/logrus"
)

type Server struct {
	config  *Config
	handler *HTTPHandler
}

func NewServer(config *Config, handler *HTTPHandler) *Server {
	return &Server{
		config:  config,
		handler: handler,
	}
}

func (s *Server) Run() error {
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(0, 0, 0, 0), Port: s.config.Port})
	if err != nil {
		return fmt.Errorf("listen failed: %v", err)
	}
	defer listener.Close()
	logrus.Infof("REDIRECT proxy server listening on 0.0.0.0:%d", s.config.Port)

	if s.config.PoolSize > 0 {
		// --- Worker Pool 模式 ---
		logrus.Infof("Starting in Worker Pool Mode (size: %d)", s.config.PoolSize)
		connChan := make(chan *net.TCPConn, s.config.PoolSize)

		// 启动指定数量的 worker goroutine
		for i := 0; i < s.config.PoolSize; i++ {
			go func(workerID int) {
				for conn := range connChan {
					logrus.Debugf("[server] Worker %d processing connection from %s", workerID, conn.RemoteAddr())
					s.handleConnection(conn)
				}
				logrus.Debugf("[server] Worker %d stopping", workerID)
			}(i)
		}

		// Accept 循环 (生产者)
		for {
			conn, err := listener.AcceptTCP()
			if err != nil {
				logrus.Warnf("Accept error: %v; retrying...", err)
				time.Sleep(5 * time.Millisecond)
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
				logrus.Warnf("Accept error: %v; retrying...", err)
				time.Sleep(5 * time.Millisecond)
				continue
			}
			go s.handleConnection(conn)
		}
	}
}

func (s *Server) handleConnection(clientConn *net.TCPConn) {

	s.handler.stats.AddActiveConnections(1)
	defer func() {
		s.handler.stats.AddActiveConnections(^uint64(0)) // 减 1
		clientConn.Close()
	}()

	originalDst, err := getOriginalDst(clientConn)
	if err != nil {
		logrus.Debugf("[server] Failed to get original destination: %v", err)
		return
	}

	destAddrPort := originalDst.String()
	clientAddr := clientConn.RemoteAddr()

	logrus.Debugf("[server] Connection: %s -> %s (original: %s)",
		clientAddr.String(),
		clientConn.LocalAddr().String(),
		destAddrPort)

	// 使用 DialTimeout 连接到原始目标
	var serverConn net.Conn
	var Timeout time.Duration = 5 * time.Minute
	if Timeout > 0 {
		dialer := net.Dialer{Timeout: Timeout}
		serverConn, err = dialer.Dial("tcp", destAddrPort)
	} else {
		serverConn, err = net.Dial("tcp", destAddrPort)
	}

	if err != nil {
		logrus.Debugf("[server] Failed to connect to %s: %v", destAddrPort, err)
		return
	}
	defer serverConn.Close()

	// 为两个连接设置 I/O 超时
	if Timeout > 0 {
		deadline := time.Now().Add(Timeout)
		clientConn.SetDeadline(deadline)
		serverConn.SetDeadline(deadline)
		// 使用 defer 确保在函数退出时清除 deadline
		defer clientConn.SetDeadline(time.Time{})
		defer serverConn.SetDeadline(time.Time{})
	}

	// 双向转发数据
	done := make(chan struct{}, 2)

	// 客户端 -> 服务器 (调用 handler 修改 UA)
	go func() {
		defer serverConn.(*net.TCPConn).CloseWrite()
		s.handler.ModifyAndForward(serverConn, clientConn, destAddrPort, originalDst.IP.String(), originalDst.Port)
		done <- struct{}{}
	}()

	// 服务器 -> 客户端 (直接转发)
	go func() {
		defer clientConn.CloseWrite()
		io.Copy(clientConn, serverConn)
		done <- struct{}{}
	}()

	// 等待两个方向的转发完成
	<-done
	<-done
}
