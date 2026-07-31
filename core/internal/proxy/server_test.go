package proxy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"UAmask/internal/model"
)

type acceptResult struct {
	session *model.AcceptedSession
	err     error
}

type ingressStub struct {
	results chan acceptResult
	closed  chan struct{}
	once    sync.Once
}

func newIngressStub() *ingressStub {
	return &ingressStub{
		results: make(chan acceptResult, 4),
		closed:  make(chan struct{}),
	}
}

func (stub *ingressStub) Accept() (*model.AcceptedSession, error) {
	select {
	case result := <-stub.results:
		return result.session, result.err
	case <-stub.closed:
		return nil, context.Canceled
	}
}

func (stub *ingressStub) Close() error {
	stub.once.Do(func() { close(stub.closed) })
	return nil
}

type blockingManager struct {
	started chan *model.AcceptedSession
	release chan struct{}
}

func (manager *blockingManager) Handle(accepted *model.AcceptedSession) {
	manager.started <- accepted
	<-manager.release
}

func TestServerRunContextWaitsForAcceptedSessions(t *testing.T) {
	for _, poolSize := range []int{0, 2} {
		poolSize := poolSize
		t.Run(fmt.Sprintf("LIFE-SHUTDOWN-201/pool-%d", poolSize), func(t *testing.T) {
			ingress := newIngressStub()
			manager := &blockingManager{
				started: make(chan *model.AcceptedSession, 1),
				release: make(chan struct{}),
			}
			server := NewServer(poolSize, ingress, manager)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- server.RunContext(ctx) }()

			accepted := &model.AcceptedSession{Target: model.Target{Address: "203.0.113.10:443"}}
			ingress.results <- acceptResult{session: accepted}
			if got := waitValue(t, manager.started); got != accepted {
				t.Fatalf("manager received %p, want %p", got, accepted)
			}

			cancel()
			waitClosed(t, ingress.closed)
			select {
			case err := <-done:
				t.Fatalf("server returned before accepted session completed: %v", err)
			default:
			}

			close(manager.release)
			if err := waitValue(t, done); err != nil {
				t.Fatalf("RunContext returned error on cancellation: %v", err)
			}
		})
	}
}

func TestServerRunContextReturnsFatalAcceptError(t *testing.T) {
	ingress := newIngressStub()
	ingress.results <- acceptResult{err: errors.New("listener failed")}
	server := NewServer(0, ingress, &blockingManager{
		started: make(chan *model.AcceptedSession, 1),
		release: make(chan struct{}),
	})

	err := server.RunContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "listener failed") {
		t.Fatalf("LIFE-ERROR-201 RunContext error = %v, want listener failure", err)
	}
	waitClosed(t, ingress.closed)
}

func waitClosed(t *testing.T, channel <-chan struct{}) {
	t.Helper()
	select {
	case <-channel:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel to close")
	}
}

func waitValue[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel value")
		var zero T
		return zero
	}
}
