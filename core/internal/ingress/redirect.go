package ingress

import (
	"errors"
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
	closed   bool
}

type temporaryAcceptError struct {
	err error
}

func (e temporaryAcceptError) Error() string   { return e.err.Error() }
func (e temporaryAcceptError) Unwrap() error   { return e.err }
func (e temporaryAcceptError) Temporary() bool { return true }

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
		return nil, temporaryAcceptError{err: err}
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

// Listen binds the redirect listener ahead of the first Accept call.
//
// The listener is created lazily inside Accept, which makes "is the process
// listening on its port?" an unreliable liveness signal right after startup: a
// freshly started instance is up but has no listening socket until a client
// happens to connect. Anything that gates traffic on that signal - an external
// iptables REDIRECT installer, a health check, a restart watchdog - then
// observes "not listening" and can deadlock the proxy: no redirect rule means
// no connection, no connection means no lazy bind, and no bind keeps the rule
// away forever.
//
// Binding up front makes the listening socket the truthful readiness signal and
// surfaces bind failures (for example EADDRINUSE from a competing instance) at
// startup instead of after the first connection arrives.
func (r *Redirect) Listen() error {
	_, err := r.ensureListener()
	return err
}

func (r *Redirect) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	listener := r.listener
	r.mu.Unlock()

	if listener == nil {
		return nil
	}
	if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		return err
	}
	return nil
}

func (r *Redirect) ensureListener() (*net.TCPListener, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, net.ErrClosed
	}

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
