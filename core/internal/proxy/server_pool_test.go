package proxy

import (
	"context"
	"sync"
	"testing"
	"time"

	"UAmask/internal/model"
)

// newTestSession 构造一个带真实 net.Conn 的 AcceptedSession；
// closeAccepted 需要 ClientConn 非 nil 才能验证"连接确实被关闭"。
func newTestSession(conn *fakeConn) *model.AcceptedSession {
	return &model.AcceptedSession{
		ClientConn:  conn.tcpConn(),
		IngressKind: model.IngressKindRedirect,
		Target:      model.Target{Address: "10.0.0.9:80", IP: "10.0.0.9", Port: 80},
	}
}

// slowManager 模拟"worker 全被僵死连接占住"的场景：
// 每条 HandleContext 都会一直阻塞，直到 ctx 被取消。
type slowManager struct {
	mu      sync.Mutex
	handled int
	release chan struct{}
	started chan struct{}
}

func newSlowManager() *slowManager {
	return &slowManager{
		release: make(chan struct{}),
		started: make(chan struct{}, 16),
	}
}

func (m *slowManager) Handle(accepted *model.AcceptedSession) {
	m.HandleContext(context.Background(), accepted)
}

func (m *slowManager) HandleContext(ctx context.Context, accepted *model.AcceptedSession) {
	m.mu.Lock()
	m.handled++
	m.mu.Unlock()
	select {
	case m.started <- struct{}{}:
	default:
	}
	select {
	case <-ctx.Done():
	case <-m.release:
	}
	if accepted != nil && accepted.ClientConn != nil {
		_ = accepted.ClientConn.Close()
	}
}

func (m *slowManager) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.handled
}

// TestSubmitPoolFullRejectsInsteadOfBlocking 是本修复的核心回归测试。
//
// 断言：当 worker 池被占满时，submit 必须"快速失败"（拒绝这一个连接），
// 而不是阻塞 acceptLoop。
//
// 修复前的行为：池满时 `sessionChan <- accepted` 会永久阻塞，
// acceptLoop 停摆 -> 新连接堆积在 TCP backlog -> 表现为"全屋断网"。
func TestSubmitPoolFullRejectsInsteadOfBlocking(t *testing.T) {
	const poolSize = 2

	ingress := newIngressStub()
	manager := newSlowManager()
	server := NewServer(poolSize, ingress, manager)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() { runErr <- server.RunContext(ctx) }()

	// 先喂满 poolSize 条，让所有 worker 进入阻塞状态
	for i := 0; i < poolSize; i++ {
		conn := newFakeConn()
		ingress.results <- acceptResult{session: newTestSession(conn)}
	}
	// 等 worker 真正开始处理
	for i := 0; i < poolSize; i++ {
		select {
		case <-manager.started:
		case <-time.After(2 * time.Second):
			t.Fatalf("worker %d did not start in time", i)
		}
	}

	// 再喂一条：池已满，submit 应立即返回（拒绝），不得阻塞
	extra := newFakeConn()
	ingress.results <- acceptResult{session: newTestSession(extra)}

	// 关键断言：acceptLoop 必须仍然活着，能继续 accept。
	// 若池满时阻塞，这里会超时。
	select {
	case <-manager.started:
		t.Fatalf("unexpected: extra session was handed to a worker despite full pool")
	case <-time.After(500 * time.Millisecond):
		// 期望：extra 被拒绝，没有 worker 接手
	}

	// extra 连接必须已被关闭（快速失败的语义）
	if !extra.Closed() {
		t.Fatalf("rejected session's client conn should be closed")
	}

	// acceptLoop 仍可继续工作：再喂一条也能被接受（accept 未停摆）
	another := newFakeConn()
	select {
	case ingress.results <- acceptResult{session: newTestSession(another)}:
	case <-time.After(time.Second):
		t.Fatalf("acceptLoop appears stalled: cannot feed another session")
	}

	// 释放所有 worker，避免 goroutine 泄漏
	close(manager.release)
	cancel()
	select {
	case <-runErr:
	case <-time.After(3 * time.Second):
		t.Fatalf("server did not shut down")
	}

	// 归一检查：被拒绝的连接不应进入 handled 计数
	if got := manager.count(); got > poolSize {
		t.Fatalf("rejected sessions must not be handled: handled=%d pool=%d", got, poolSize)
	}
}
