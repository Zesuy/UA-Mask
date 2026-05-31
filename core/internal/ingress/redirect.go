package ingress

import (
	"fmt"
	"net"
	"sync"

	"UAmask/internal/config"
	"UAmask/internal/model"
)

type Ingress interface {
	Accept() (*model.AcceptedSession, error)
	Close() error
}

type Redirect struct {
	mu       sync.Mutex
	listener *net.TCPListener
	config   config.ListenConfig
}

func NewRedirect(cfg config.ListenConfig) (*Redirect, error) {
	return &Redirect{
		config: cfg,
	}, nil
}

func (r *Redirect) Accept() (*model.AcceptedSession, error) {
	listener, err := r.ensureListener()
	if err != nil {
		return nil, err
	}

	clientConn, err := listener.AcceptTCP()
	if err != nil {
		return nil, err
	}

	originalDst, err := getOriginalDst(clientConn)
	if err != nil {
		_ = clientConn.Close()
		return nil, err
	}

	_ = clientConn.SetKeepAlive(true)
	_ = clientConn.SetKeepAlivePeriod(r.config.ClientKeepAlive)

	return &model.AcceptedSession{
		ClientConn:  clientConn,
		IngressKind: model.IngressKindRedirect,
		Target: model.Target{
			Address: originalDst.String(),
			IP:      originalDst.IP.String(),
			Port:    originalDst.Port,
		},
	}, nil
}

func (r *Redirect) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r == nil || r.listener == nil {
		return nil
	}
	return r.listener.Close()
}

func (r *Redirect) ensureListener() (*net.TCPListener, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.listener != nil {
		return r.listener, nil
	}

	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(0, 0, 0, 0), Port: r.config.Port})
	if err != nil {
		return nil, fmt.Errorf("listen failed: %w", err)
	}
	r.listener = listener
	return listener, nil
}
