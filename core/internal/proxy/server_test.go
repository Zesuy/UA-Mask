package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"UAmask/internal/model"

	"github.com/sirupsen/logrus"
)

func TestMain(main *testing.M) {
	logrus.SetOutput(io.Discard)
	os.Exit(main.Run())
}

type acceptResult struct {
	session *model.AcceptedSession
	err     error
}

type ingressStub struct {
	results   chan acceptResult
	delivered chan *model.AcceptedSession
	closed    chan struct{}
	once      sync.Once
}

func newIngressStub() *ingressStub {
	return &ingressStub{
		results:   make(chan acceptResult, 4),
		delivered: make(chan *model.AcceptedSession, 4),
		closed:    make(chan struct{}),
	}
}

func (stub *ingressStub) Accept() (*model.AcceptedSession, error) {
	select {
	case result := <-stub.results:
		select {
		case stub.delivered <- result.session:
		default:
		}
		return result.session, result.err
	case <-stub.closed:
		return nil, context.Canceled
	}
}

type contextBlockingManager struct {
	mu      sync.Mutex
	handled []*model.AcceptedSession
	started chan *model.AcceptedSession
}

type orderedContextManager struct {
	ingress  *ingressStub
	started  chan struct{}
	observed chan bool
}

func (manager *orderedContextManager) Handle(accepted *model.AcceptedSession) {
	manager.HandleContext(context.Background(), accepted)
}

func (manager *orderedContextManager) HandleContext(ctx context.Context, _ *model.AcceptedSession) {
	close(manager.started)
	<-ctx.Done()
	select {
	case <-manager.ingress.closed:
		manager.observed <- true
	default:
		manager.observed <- false
	}
}

func (manager *contextBlockingManager) Handle(accepted *model.AcceptedSession) {
	manager.HandleContext(context.Background(), accepted)
}

func (manager *contextBlockingManager) HandleContext(ctx context.Context, accepted *model.AcceptedSession) {
	manager.mu.Lock()
	manager.handled = append(manager.handled, accepted)
	manager.mu.Unlock()
	manager.started <- accepted
	<-ctx.Done()
}

func (manager *contextBlockingManager) Handled() []*model.AcceptedSession {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return append([]*model.AcceptedSession(nil), manager.handled...)
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

func TestServerWorkerPoolSkipsQueuedSessionsAfterCancellation(t *testing.T) {
	ingress := newIngressStub()
	manager := &contextBlockingManager{started: make(chan *model.AcceptedSession, 2)}
	server := NewServer(1, ingress, manager)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.RunContext(ctx) }()

	first := &model.AcceptedSession{Target: model.Target{Address: "203.0.113.10:80"}}
	second := &model.AcceptedSession{Target: model.Target{Address: "203.0.113.11:80"}}
	ingress.results <- acceptResult{session: first}
	if got := waitValue(t, manager.started); got != first {
		t.Fatalf("LIFE-POOL-202 first handled session = %p, want %p", got, first)
	}
	if got := waitValue(t, ingress.delivered); got != first {
		t.Fatalf("LIFE-POOL-202 first delivered session = %p, want %p", got, first)
	}
	ingress.results <- acceptResult{session: second}
	if got := waitValue(t, ingress.delivered); got != second {
		t.Fatalf("LIFE-POOL-202 delivered session = %p, want %p", got, second)
	}

	cancel()
	if err := waitValue(t, done); err != nil {
		t.Fatalf("LIFE-POOL-202 RunContext: %v", err)
	}
	handled := manager.Handled()
	if len(handled) != 1 || handled[0] != first {
		t.Fatalf("LIFE-POOL-202 handled sessions = %v, want only first", handled)
	}
}

func TestServerFatalAcceptErrorCancelsActiveAndQueuedSessions(t *testing.T) {
	for _, poolSize := range []int{0, 1} {
		poolSize := poolSize
		t.Run(fmt.Sprintf("LIFE-ERROR-203/pool-%d", poolSize), func(t *testing.T) {
			ingress := newIngressStub()
			manager := &contextBlockingManager{started: make(chan *model.AcceptedSession, 2)}
			server := NewServer(poolSize, ingress, manager)
			done := make(chan error, 1)
			go func() { done <- server.RunContext(context.Background()) }()

			first := &model.AcceptedSession{Target: model.Target{Address: "203.0.113.20:80"}}
			ingress.results <- acceptResult{session: first}
			if got := waitValue(t, manager.started); got != first {
				t.Fatalf("first handled session = %p, want %p", got, first)
			}
			if got := waitValue(t, ingress.delivered); got != first {
				t.Fatalf("first delivered session = %p, want %p", got, first)
			}

			var second *model.AcceptedSession
			if poolSize > 0 {
				second = &model.AcceptedSession{Target: model.Target{Address: "203.0.113.21:80"}}
				ingress.results <- acceptResult{session: second}
				if got := waitValue(t, ingress.delivered); got != second {
					t.Fatalf("queued delivered session = %p, want %p", got, second)
				}
			}

			ingress.results <- acceptResult{err: errors.New("listener failed")}
			err := waitValue(t, done)
			if err == nil || !strings.Contains(err.Error(), "listener failed") {
				t.Fatalf("RunContext error = %v, want listener failure", err)
			}
			handled := manager.Handled()
			if len(handled) != 1 || handled[0] != first {
				t.Fatalf("handled sessions = %v, want only first; queued=%p", handled, second)
			}
		})
	}
}

func TestServerClosesIngressBeforeCancellingHandlers(t *testing.T) {
	ingress := newIngressStub()
	manager := &orderedContextManager{
		ingress:  ingress,
		started:  make(chan struct{}),
		observed: make(chan bool, 1),
	}
	server := NewServer(0, ingress, manager)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.RunContext(ctx) }()

	ingress.results <- acceptResult{session: &model.AcceptedSession{Target: model.Target{Address: "203.0.113.30:80"}}}
	waitClosed(t, manager.started)
	cancel()
	if observed := waitValue(t, manager.observed); !observed {
		t.Fatal("LIFE-ORDER-204 handler observed cancellation before ingress was closed")
	}
	if err := waitValue(t, done); err != nil {
		t.Fatalf("LIFE-ORDER-204 RunContext: %v", err)
	}
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
