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
//
// 队列容量也是 poolSize，所以"poolSize 个 worker 都在忙"并不等于"池满"：
// 此时队列还有 poolSize 个空位，再提交的会话会老老实实进队等待。
// 必须先把这 poolSize 个空位也填满，之后提交的那一条才会走到拒绝分支。
// 这一点决定了本用例是否真的覆盖了回归点。
func TestSubmitPoolFullRejectsInsteadOfBlocking(t *testing.T) {
	const poolSize = 2

	ingress := newIngressStub()
	manager := newSlowManager()
	server := NewServer(poolSize, ingress, manager)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() { runErr <- server.RunContext(ctx) }()

	// ① 让 poolSize 个 worker 全部进入阻塞状态。
	for i := 0; i < poolSize; i++ {
		conn := newFakeConn()
		ingress.results <- acceptResult{session: newTestSession(conn)}
	}
	for i := 0; i < poolSize; i++ {
		select {
		case <-manager.started:
		case <-time.After(2 * time.Second):
			t.Fatalf("worker %d did not start in time", i)
		}
	}
	if got := manager.count(); got != poolSize {
		t.Fatalf("expected %d blocked workers, got %d", poolSize, got)
	}

	// ② 池还没满：队列还有 poolSize 个空位。先填满它们。
	//    这些会话必须能成功入队（不阻塞、不被拒绝），否则是另一个 bug。
	queued := make([]*fakeConn, 0, poolSize)
	for i := 0; i < poolSize; i++ {
		conn := newFakeConn()
		select {
		case ingress.results <- acceptResult{session: newTestSession(conn)}:
		case <-time.After(time.Second):
			t.Fatalf("could not enqueue filler %d: acceptLoop stalled", i)
		}
		queued = append(queued, conn)
	}
	// 给 acceptLoop 一点时间把这 poolSize 条搬进 sessionChan。
	waitForPoolState(t, "queue to fill", func() bool {
		return manager.count() == poolSize
	})
	for i, conn := range queued {
		if conn.Closed() {
			t.Fatalf("filler %d was rejected but the pool was not full yet", i)
		}
	}

	// ③ 现在 worker 全忙、队列全满，再提交一条必然走"池满快速失败"分支。
	extra := newFakeConn()
	select {
	case ingress.results <- acceptResult{session: newTestSession(extra)}:
	case <-time.After(time.Second):
		t.Fatalf("acceptLoop stalled: could not submit the overflowing session")
	}

	// 关键断言：extra 必须被拒绝（连接被关闭），且不会交给任何 worker。
	waitForPoolState(t, "overflow session to be rejected", func() bool {
		return extra.Closed()
	})
	select {
	case <-manager.started:
		t.Fatalf("unexpected: overflow session was handed to a worker despite a full pooled queue")
	case <-time.After(150 * time.Millisecond):
		// 期望路径：没有 worker 接手
	}

	// ④ acceptLoop 必须仍然活着：还能继续 accept 后续连接。
	another := newFakeConn()
	select {
	case ingress.results <- acceptResult{session: newTestSession(another)}:
	case <-time.After(time.Second):
		t.Fatalf("acceptLoop appears stalled after rejecting an overflow session")
	}
	// 池依旧满，所以这一条同样会被快速拒绝——这正说明循环还在转。
	waitForPoolState(t, "post-rejection session to be rejected", func() bool {
		return another.Closed()
	})

	// ⑤ 放行 worker：之前排队的那 poolSize 条应被正常处理。
	close(manager.release)
	waitForPoolState(t, "queued sessions to be handled", func() bool {
		return manager.count() >= poolSize*2
	})

	cancel()
	select {
	case <-runErr:
	case <-time.After(3 * time.Second):
		t.Fatalf("server did not shut down")
	}

	// 归一检查：被拒绝的连接不应进入 handled 计数。
	// 处理过的只有 poolSize 个阻塞 worker + poolSize 个排队会话。
	if got, want := manager.count(), poolSize*2; got != want {
		t.Fatalf("handled sessions = %d, want %d (rejected sessions must not be handled)", got, want)
	}
	for i, conn := range queued {
		if !conn.Closed() {
			t.Fatalf("queued session %d should have been closed once handled", i)
		}
	}
}

// waitForPoolState 轮询等待条件成立，用于取代对固定时长的依赖。
func waitForPoolState(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
