package proxy

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"UAmask/internal/ingress"
	"UAmask/internal/model"
	"UAmask/internal/session"

	"github.com/sirupsen/logrus"
)

const acceptRetryDelay = 5 * time.Millisecond

type Server struct {
	poolSize int
	ingress  ingress.Ingress
	manager  session.Manager
}

func NewServer(poolSize int, ingress ingress.Ingress, manager session.Manager) *Server {
	return &Server{
		poolSize: poolSize,
		ingress:  ingress,
		manager:  manager,
	}
}

func (s *Server) Run() error {
	return s.RunContext(context.Background())
}

func (s *Server) RunContext(ctx context.Context) error {
	if s.ingress == nil || s.manager == nil {
		return fmt.Errorf("server dependencies are not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sessionCtx, cancelSessions := context.WithCancel(context.WithoutCancel(ctx))

	tracker := newSessionTracker()
	shutdownStarted := make(chan struct{})
	var shutdownOnce sync.Once
	shutdown := func() {
		shutdownOnce.Do(func() {
			close(shutdownStarted)
			_ = s.ingress.Close()
			cancelSessions()
			tracker.CloseAll()
		})
	}
	watchDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdown()
		case <-watchDone:
		}
	}()

	logrus.Infof("UA-Mask ingress server ready")
	var runErr error
	if s.poolSize > 0 {
		runErr = s.runWithPool(sessionCtx, tracker, shutdownStarted, shutdown)
	} else {
		runErr = s.runDefault(sessionCtx, tracker, shutdownStarted)
	}
	shutdown()
	close(watchDone)
	tracker.Wait()
	return runErr
}

func (s *Server) runWithPool(
	ctx context.Context,
	tracker *sessionTracker,
	shutdownStarted <-chan struct{},
	shutdown func(),
) error {
	logrus.Infof("Starting in Worker Pool Mode (size: %d)", s.poolSize)

	sessionChan := make(chan *model.AcceptedSession, s.poolSize)
	var workers sync.WaitGroup
	for i := 0; i < s.poolSize; i++ {
		workers.Add(1)
		go func(workerID int) {
			defer workers.Done()
			for accepted := range sessionChan {
				if ctx.Err() != nil {
					closeAccepted(accepted)
					tracker.Done(accepted)
					continue
				}
				logrus.Debugf("[server] Worker %d processing connection for %s", workerID, accepted.Target.Address)
				s.handleSession(ctx, accepted)
				tracker.Done(accepted)
			}
		}(i)
	}

	err := s.acceptLoop(ctx, shutdownStarted, func(accepted *model.AcceptedSession) bool {
		if !tracker.Add(accepted) {
			closeAccepted(accepted)
			return false
		}
		select {
		case sessionChan <- accepted:
			return true
		case <-ctx.Done():
			closeAccepted(accepted)
			tracker.Done(accepted)
			return false
		}
	})
	shutdown()
	close(sessionChan)
	workers.Wait()
	return err
}

func (s *Server) runDefault(ctx context.Context, tracker *sessionTracker, shutdownStarted <-chan struct{}) error {
	logrus.Info("Starting in Default Mode (one goroutine per connection)")
	return s.acceptLoop(ctx, shutdownStarted, func(accepted *model.AcceptedSession) bool {
		if !tracker.Add(accepted) {
			closeAccepted(accepted)
			return false
		}
		go func() {
			defer tracker.Done(accepted)
			s.handleSession(ctx, accepted)
		}()
		return true
	})
}

func (s *Server) handleSession(ctx context.Context, accepted *model.AcceptedSession) {
	if manager, ok := s.manager.(session.ContextManager); ok {
		manager.HandleContext(ctx, accepted)
		return
	}
	s.manager.Handle(accepted)
}

func (s *Server) acceptLoop(
	ctx context.Context,
	shutdownStarted <-chan struct{},
	submit func(*model.AcceptedSession) bool,
) error {
	for {
		accepted, err := s.ingress.Accept()
		if err != nil {
			if ctx.Err() != nil || channelClosed(shutdownStarted) {
				return nil
			}
			if !isTemporaryAcceptError(err) {
				return fmt.Errorf("accept session: %w", err)
			}
			logrus.Warnf("Accept error: %v; retrying...", err)
			if !waitForRetry(ctx) {
				return nil
			}
			continue
		}
		if accepted == nil {
			return errors.New("accept session: ingress returned nil session")
		}
		if !submit(accepted) {
			return nil
		}
	}
}

func channelClosed(channel <-chan struct{}) bool {
	select {
	case <-channel:
		return true
	default:
		return false
	}
}

func isTemporaryAcceptError(err error) bool {
	var temporary interface{ Temporary() bool }
	return errors.As(err, &temporary) && temporary.Temporary()
}

func waitForRetry(ctx context.Context) bool {
	timer := time.NewTimer(acceptRetryDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

type sessionTracker struct {
	mu     sync.Mutex
	active map[*model.AcceptedSession]struct{}
	closed bool
	wg     sync.WaitGroup
}

func newSessionTracker() *sessionTracker {
	return &sessionTracker{active: make(map[*model.AcceptedSession]struct{})}
}

func (tracker *sessionTracker) Add(accepted *model.AcceptedSession) bool {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.closed {
		return false
	}
	tracker.active[accepted] = struct{}{}
	tracker.wg.Add(1)
	return true
}

func (tracker *sessionTracker) Done(accepted *model.AcceptedSession) {
	tracker.mu.Lock()
	delete(tracker.active, accepted)
	tracker.mu.Unlock()
	tracker.wg.Done()
}

func (tracker *sessionTracker) CloseAll() {
	tracker.mu.Lock()
	tracker.closed = true
	accepted := make([]*model.AcceptedSession, 0, len(tracker.active))
	for session := range tracker.active {
		accepted = append(accepted, session)
	}
	tracker.mu.Unlock()
	for _, session := range accepted {
		closeAccepted(session)
	}
}

func (tracker *sessionTracker) Wait() {
	tracker.wg.Wait()
}

func closeAccepted(accepted *model.AcceptedSession) {
	if accepted != nil && accepted.ClientConn != nil {
		_ = accepted.ClientConn.Close()
	}
}
