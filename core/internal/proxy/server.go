package proxy

import (
	"fmt"
	"io"
	"net"
	"time"

	"UAmask/internal/config"

	"github.com/sirupsen/logrus"
)

type Server struct {
	listenConfig config.ListenConfig
	processor    RequestProcessor
	connMetrics  ConnMetrics
}

func NewServer(cfg config.ListenConfig, processor RequestProcessor, connMetrics ConnMetrics) *Server {
	return &Server{
		listenConfig: cfg,
		processor:    processor,
		connMetrics:  connMetrics,
	}
}

func (s *Server) Run() error {
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(0, 0, 0, 0), Port: s.listenConfig.Port})
	if err != nil {
		return fmt.Errorf("listen failed: %w", err)
	}
	defer listener.Close()

	logrus.Infof("REDIRECT proxy server listening on 0.0.0.0:%d", s.listenConfig.Port)

	if s.listenConfig.PoolSize > 0 {
		return s.runWithPool(listener)
	}
	return s.runDefault(listener)
}

func (s *Server) runWithPool(listener *net.TCPListener) error {
	logrus.Infof("Starting in Worker Pool Mode (size: %d)", s.listenConfig.PoolSize)

	connChan := make(chan *net.TCPConn, s.listenConfig.PoolSize)
	for i := 0; i < s.listenConfig.PoolSize; i++ {
		go func(workerID int) {
			for conn := range connChan {
				logrus.Debugf("[server] Worker %d processing connection from %s", workerID, conn.RemoteAddr())
				s.handleConnection(conn)
			}
		}(i)
	}

	for {
		conn, err := listener.AcceptTCP()
		if err != nil {
			logrus.Warnf("Accept error: %v; retrying...", err)
			time.Sleep(5 * time.Millisecond)
			continue
		}
		connChan <- conn
	}
}

func (s *Server) runDefault(listener *net.TCPListener) error {
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

func (s *Server) handleConnection(clientConn *net.TCPConn) {
	s.connMetrics.ConnectionOpened()
	defer func() {
		s.connMetrics.ConnectionClosed()
		_ = clientConn.Close()
	}()

	originalDst, err := getOriginalDst(clientConn)
	if err != nil {
		logrus.Debugf("[server] Failed to get original destination: %v", err)
		return
	}

	target := Target{
		Address: originalDst.String(),
		IP:      originalDst.IP.String(),
		Port:    originalDst.Port,
	}

	logrus.Debugf("[server] Connection: %s -> %s (original: %s)",
		clientConn.RemoteAddr().String(),
		clientConn.LocalAddr().String(),
		target.Address)

	_ = clientConn.SetKeepAlive(true)
	_ = clientConn.SetKeepAlivePeriod(s.listenConfig.ClientKeepAlive)

	dialer := net.Dialer{
		Timeout:   s.listenConfig.DialTimeout,
		KeepAlive: s.listenConfig.UpstreamKeepAlive,
	}
	serverConn, err := dialer.Dial("tcp", target.Address)
	if err != nil {
		logrus.Debugf("[server] Failed to connect to %s: %v", target.Address, err)
		return
	}
	defer serverConn.Close()

	done := make(chan struct{}, 2)
	clientIOConn := net.Conn(clientConn)
	serverIOConn := serverConn

	go func() {
		defer serverConn.(*net.TCPConn).CloseWrite()
		s.processor.Process(serverIOConn, clientIOConn, target)
		done <- struct{}{}
	}()

	go func() {
		defer clientConn.CloseWrite()
		_, _ = io.Copy(clientIOConn, serverIOConn)
		done <- struct{}{}
	}()

	<-done
	<-done
}
