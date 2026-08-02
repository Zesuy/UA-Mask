package session

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"UAmask/internal/config"
	"UAmask/internal/events"
	"UAmask/internal/model"
	"UAmask/internal/policy"
	"UAmask/internal/rewrite"
	"UAmask/internal/state"

	"github.com/sirupsen/logrus"
)

var httpMethods = []string{"GET", "POST", "HEAD", "PUT", "DELETE", "OPTIONS", "TRACE", "CONNECT", "PATCH"}

type Manager interface {
	Handle(accepted *model.AcceptedSession)
}

type ContextManager interface {
	Manager
	HandleContext(context.Context, *model.AcceptedSession)
}

type SignalHandler interface {
	Handle(signal model.OffloadSignal)
}

type DefaultManager struct {
	config       config.ListenConfig
	evaluator    policy.SessionEvaluator
	rewriter     *rewrite.RequestRewriter
	profiles     state.ProfileStore
	offload      SignalHandler
	events       *events.Dispatcher
	readerPool   sync.Pool
	writerPool   sync.Pool
	sessionIDSeq atomic.Uint64
}

func NewManager(
	cfg config.ListenConfig,
	evaluator policy.SessionEvaluator,
	rewriter *rewrite.RequestRewriter,
	profiles state.ProfileStore,
	offload SignalHandler,
	dispatcher *events.Dispatcher,
) *DefaultManager {
	manager := &DefaultManager{
		config:    cfg,
		evaluator: evaluator,
		rewriter:  rewriter,
		profiles:  profiles,
		offload:   offload,
		events:    dispatcher,
	}

	manager.readerPool = sync.Pool{
		New: func() any {
			return bufio.NewReaderSize(nil, cfg.BufferSize)
		},
	}
	manager.writerPool = sync.Pool{
		New: func() any {
			return bufio.NewWriterSize(nil, cfg.BufferSize)
		},
	}

	return manager
}

func (m *DefaultManager) Handle(accepted *model.AcceptedSession) {
	m.HandleContext(context.Background(), accepted)
}

func (m *DefaultManager) HandleContext(ctx context.Context, accepted *model.AcceptedSession) {
	if accepted == nil || accepted.ClientConn == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		_ = accepted.ClientConn.Close()
		return
	}

	sessionState := model.SessionState{
		ID:                m.sessionIDSeq.Add(1),
		IngressKind:       accepted.IngressKind,
		ClientAddr:        accepted.ClientConn.RemoteAddr().String(),
		Target:            accepted.Target,
		FlowOffloadStatus: model.FlowOffloadNone,
		OpenedAt:          time.Now(),
	}

	m.emit(model.SessionEvent{
		Type:        model.EventSessionOpened,
		SessionID:   sessionState.ID,
		Target:      accepted.Target,
		IngressKind: accepted.IngressKind,
		Timestamp:   sessionState.OpenedAt,
	})
	defer func() {
		m.emit(model.SessionEvent{
			Type:        model.EventSessionClosed,
			SessionID:   sessionState.ID,
			Target:      accepted.Target,
			IngressKind: accepted.IngressKind,
			Protocol:    sessionState.Protocol,
			Timestamp:   time.Now(),
		})
		_ = accepted.ClientConn.Close()
	}()

	dialer := net.Dialer{
		Timeout:   m.config.DialTimeout,
		KeepAlive: m.config.UpstreamKeepAlive,
	}
	upstream, err := dialer.DialContext(ctx, "tcp", accepted.Target.Address)
	if err != nil {
		logrus.Debugf("[session] Failed to connect to %s: %v", accepted.Target.Address, err)
		return
	}
	defer upstream.Close()

	upstreamConn, ok := upstream.(*net.TCPConn)
	if !ok {
		logrus.Debugf("[session] Unexpected upstream conn type for %s", accepted.Target.Address)
		return
	}
	sessionState.UpstreamAddr = upstreamConn.RemoteAddr().String()
	connectionDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = accepted.ClientConn.Close()
			_ = upstreamConn.Close()
		case <-connectionDone:
		}
	}()
	defer close(connectionDone)
	m.emit(model.SessionEvent{
		Type:        model.EventUpstreamConnected,
		SessionID:   sessionState.ID,
		Target:      accepted.Target,
		IngressKind: accepted.IngressKind,
		Timestamp:   time.Now(),
	})

	done := make(chan struct{}, 2)

	go func() {
		defer upstreamConn.CloseWrite()
		m.processClientToUpstream(&sessionState, accepted.ClientConn, upstreamConn)
		done <- struct{}{}
	}()

	go func() {
		defer accepted.ClientConn.CloseWrite()
		_, _ = io.Copy(accepted.ClientConn, upstreamConn)
		done <- struct{}{}
	}()

	<-done
	<-done
}

func (m *DefaultManager) processClientToUpstream(sessionState *model.SessionState, src net.Conn, dst net.Conn) {
	srcReader := m.readerPool.Get().(*bufio.Reader)
	srcReader.Reset(src)
	defer m.readerPool.Put(srcReader)

	dstWriter := m.writerPool.Get().(*bufio.Writer)
	dstWriter.Reset(dst)
	defer func() {
		if err := dstWriter.Flush(); err != nil {
			logrus.Debugf("[session] [%s] final flush error: %v", sessionState.Target.Address, err)
		}
		m.writerPool.Put(dstWriter)
	}()

	for {
		isHTTP, err := isHTTP(srcReader)
		if err != nil {
			m.handleDetectionError(sessionState, dst, srcReader, dstWriter, err)
			return
		}

		if !isHTTP {
			sessionState.Protocol = model.ProtocolNonHTTP
			m.emit(model.SessionEvent{
				Type:        model.EventProtocolClassified,
				SessionID:   sessionState.ID,
				Target:      sessionState.Target,
				IngressKind: sessionState.IngressKind,
				Protocol:    model.ProtocolNonHTTP,
				Timestamp:   time.Now(),
			})

			snapshot := m.profiles.Snapshot(sessionState.Target)
			decision := m.evaluator.Evaluate(sessionState, model.SessionInput{
				Kind:     model.SessionInputClassification,
				Protocol: model.ProtocolNonHTTP,
			}, snapshot)

			m.emit(model.SessionEvent{
				Type:        model.EventNonHTTPObserved,
				SessionID:   sessionState.ID,
				Target:      sessionState.Target,
				IngressKind: sessionState.IngressKind,
				Protocol:    model.ProtocolNonHTTP,
				Timestamp:   time.Now(),
			})

			if decision.FlowSignal.Kind != model.SignalNoAction && m.offload != nil {
				m.offload.Handle(decision.FlowSignal)
			}

			m.emit(model.SessionEvent{
				Type:        model.EventSessionForwardFallback,
				SessionID:   sessionState.ID,
				Target:      sessionState.Target,
				IngressKind: sessionState.IngressKind,
				Protocol:    model.ProtocolNonHTTP,
				Timestamp:   time.Now(),
			})
			m.flushAndCopy(dst, srcReader, dstWriter, sessionState.Target)
			return
		}

		if !m.processRequest(sessionState, srcReader, dstWriter) {
			return
		}
	}
}

func (m *DefaultManager) processRequest(sessionState *model.SessionState, srcReader *bufio.Reader, dstWriter *bufio.Writer) bool {
	request, err := http.ReadRequest(srcReader)
	if err != nil {
		if err == io.EOF || strings.Contains(err.Error(), "use of closed network connection") {
			logrus.Debugf("[session] [%s] connection closed", sessionState.Target.Address)
		} else if strings.Contains(err.Error(), "connection reset by peer") {
			logrus.Debugf("[session] [%s] connection reset", sessionState.Target.Address)
		} else {
			logrus.Debugf("[session] [%s] read request error: %v", sessionState.Target.Address, err)
		}
		return false
	}
	defer request.Body.Close()

	sessionState.Protocol = model.ProtocolHTTP
	m.emit(model.SessionEvent{
		Type:        model.EventProtocolClassified,
		SessionID:   sessionState.ID,
		Target:      sessionState.Target,
		IngressKind: sessionState.IngressKind,
		Protocol:    model.ProtocolHTTP,
		Timestamp:   time.Now(),
	})

	rewriteResult := m.rewriteRequest(request, sessionState.Target)
	snapshot := m.profiles.Snapshot(sessionState.Target)
	decision := m.evaluator.Evaluate(sessionState, model.SessionInput{
		Kind:           model.SessionInputRequest,
		Protocol:       model.ProtocolHTTP,
		Request:        request,
		Modified:       rewriteResult.Modified,
		FromCache:      rewriteResult.FromCache,
		Reason:         string(rewriteResult.Decision.Reason),
		FirewallHit:    rewriteResult.Decision.FirewallBypassHit,
		DropConnection: rewriteResult.Decision.DropConnection,
	}, snapshot)
	m.updateRewriteCounters(sessionState, decision.Modified)

	m.emit(model.SessionEvent{
		Type:        model.EventHTTPRequestObserved,
		SessionID:   sessionState.ID,
		Target:      sessionState.Target,
		IngressKind: sessionState.IngressKind,
		Protocol:    model.ProtocolHTTP,
		Timestamp:   time.Now(),
	})

	eventType := model.EventRewriteSkipped
	if decision.Modified {
		eventType = model.EventRewriteApplied
	}
	m.emit(model.SessionEvent{
		Type:        eventType,
		SessionID:   sessionState.ID,
		Target:      sessionState.Target,
		IngressKind: sessionState.IngressKind,
		Protocol:    model.ProtocolHTTP,
		Reason:      decision.Reason,
		Modified:    decision.Modified,
		FromCache:   decision.FromCache,
		FirewallHit: decision.FirewallHit,
		Timestamp:   time.Now(),
	})

	if decision.FlowSignal.Kind != model.SignalNoAction && m.offload != nil {
		m.offload.Handle(decision.FlowSignal)
	}

	if decision.Action == model.ActionDrop {
		logrus.Debugf("[session] [%s] dropping connection after policy decision", sessionState.Target.Address)
		return false
	}

	if err := request.Write(dstWriter); err != nil {
		logrus.Debugf("[session] [%s] write request error: %v", sessionState.Target.Address, err)
		return false
	}
	if err := dstWriter.Flush(); err != nil {
		logrus.Debugf("[session] [%s] flush after request failed: %v", sessionState.Target.Address, err)
		return false
	}

	return true
}

func (m *DefaultManager) handleDetectionError(sessionState *model.SessionState, dst net.Conn, srcReader *bufio.Reader, dstWriter *bufio.Writer, err error) {
	if err == io.EOF || strings.Contains(err.Error(), "use of closed network connection") {
		logrus.Debugf("[session] [%s] connection closed", sessionState.Target.Address)
	} else {
		logrus.Debugf("[session] [%s] HTTP detection error: %v", sessionState.Target.Address, err)
	}

	m.emit(model.SessionEvent{
		Type:        model.EventSessionForwardFallback,
		SessionID:   sessionState.ID,
		Target:      sessionState.Target,
		IngressKind: sessionState.IngressKind,
		Protocol:    sessionState.Protocol,
		Timestamp:   time.Now(),
	})
	m.flushAndCopy(dst, srcReader, dstWriter, sessionState.Target)
}

func (m *DefaultManager) emit(event model.SessionEvent) {
	if m.events == nil {
		return
	}
	m.events.Append(event)
}

func (m *DefaultManager) rewriteRequest(request *http.Request, target model.Target) rewrite.Result {
	if m.rewriter == nil {
		return rewrite.Result{}
	}
	return m.rewriter.Rewrite(request, target.IP, target.Port)
}

func (m *DefaultManager) updateRewriteCounters(sessionState *model.SessionState, modified bool) {
	if modified {
		sessionState.ConsecutiveRewrite++
		sessionState.ConsecutiveNoRewrite = 0
		return
	}

	sessionState.ConsecutiveNoRewrite++
	sessionState.ConsecutiveRewrite = 0
}

func isHTTP(reader *bufio.Reader) (bool, error) {
	buf, err := reader.Peek(7)
	if err != nil {
		if !strings.Contains(err.Error(), "EOF") {
			logrus.Debugf("[session] Peek error: %s", err.Error())
		}
		return false, err
	}

	hint := string(buf)
	for _, method := range httpMethods {
		if strings.HasPrefix(hint, method) {
			return true, nil
		}
	}
	return false, nil
}

func (m *DefaultManager) flushAndCopy(dst net.Conn, srcReader *bufio.Reader, dstWriter *bufio.Writer, target model.Target) {
	if err := dstWriter.Flush(); err != nil {
		logrus.Debugf("[session] [%s] flush before fallback failed: %v", target.Address, err)
	}
	if _, err := io.Copy(dst, srcReader); err != nil && err != io.EOF {
		logrus.Debugf("[session] [%s] fallback copy error: %v", target.Address, err)
	}
}
