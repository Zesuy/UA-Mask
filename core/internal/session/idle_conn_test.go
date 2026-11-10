package session

import (
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// mockConn 是可注入的 net.Conn，用 channel 控制读行为，避免依赖真实时间。
// 注意：它不实现 deadline 语义，只用于验证 lastSeen / idle 这类状态推进；
// 凡是需要"读写在超时后真的退出"的用例都必须用 net.Pipe。
type mockConn struct {
	net.Conn
	closed  atomic.Bool
	readCh  chan []byte
	writeCh chan []byte
}

func newMockConn() *mockConn {
	return &mockConn{
		readCh:  make(chan []byte, 8),
		writeCh: make(chan []byte, 8),
	}
}

func (m *mockConn) Read(p []byte) (int, error) {
	buf, ok := <-m.readCh
	if !ok {
		return 0, net.ErrClosed
	}
	n := copy(p, buf)
	return n, nil
}

func (m *mockConn) Write(p []byte) (int, error) {
	if m.closed.Load() {
		return 0, net.ErrClosed
	}
	select {
	case m.writeCh <- p:
	default:
	}
	return len(p), nil
}

func (m *mockConn) Close() error {
	if m.closed.Swap(true) {
		return nil
	}
	close(m.readCh)
	return nil
}

func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

func (m *mockConn) LocalAddr() net.Addr  { return &net.TCPAddr{IP: net.IPv4(10, 0, 0, 1)} }
func (m *mockConn) RemoteAddr() net.Addr { return &net.TCPAddr{IP: net.IPv4(10, 0, 0, 2)} }

// discardingWriteConn 让写立即成功、读仍然阻塞在真实 pipe 上。
// 用于复现"一个方向持续活跃、另一个方向一直阻塞"的场景。
type discardingWriteConn struct {
	net.Conn
}

func (c *discardingWriteConn) Write(p []byte) (int, error) { return len(p), nil }

// snapshot 供测试读取内部状态（同包内定义，不污染生产代码）。
func (c *idleConn) snapshot() (lastSeen, installed time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastSeen, c.installed
}

func waitFor(t *testing.T, limit time.Duration, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal(message)
}

// TestNewIdleConnDisabled：timeout<=0 时返回 nil，调用方应回退到裸 conn。
func TestNewIdleConnDisabled(t *testing.T) {
	mc := newMockConn()
	if got := newIdleConn(mc, 0); got != nil {
		t.Fatalf("timeout=0 should return nil, got %v", got)
	}
	if got := newIdleConn(mc, -time.Second); got != nil {
		t.Fatalf("negative timeout should return nil, got %v", got)
	}
}

// TestIdleConnTouchUpdatesLastSeen：读写任何一个方向都应刷新 lastSeen。
func TestIdleConnTouchUpdatesLastSeen(t *testing.T) {
	mc := newMockConn()
	ic := newIdleConn(mc, 30*time.Second)
	if ic == nil {
		t.Fatal("expected non-nil idleConn")
	}
	defer ic.Close()

	before, _ := ic.snapshot()

	// 单调时钟精度不足以区分同纳秒内的两次调用，
	// 因此显式推进时间再断言，避免测试本身变得脆弱。
	time.Sleep(2 * time.Millisecond)

	if _, err := ic.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	afterWrite, _ := ic.snapshot()
	if afterWrite <= before {
		t.Fatalf("Write did not advance lastSeen: before=%d after=%d", before, afterWrite)
	}

	time.Sleep(2 * time.Millisecond)

	mc.readCh <- []byte("pong")
	buf := make([]byte, 8)
	if _, err := ic.Read(buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	afterRead, _ := ic.snapshot()
	if afterRead <= afterWrite {
		t.Fatalf("Read did not advance lastSeen: afterWrite=%d afterRead=%d", afterWrite, afterRead)
	}
}

// TestIdleConnIdleGrowsWithInactivity：无读写时 idle() 应随时长增长。
func TestIdleConnIdleGrowsWithInactivity(t *testing.T) {
	mc := newMockConn()
	ic := newIdleConn(mc, 30*time.Second)
	if ic == nil {
		t.Fatal("expected non-nil idleConn")
	}
	defer ic.Close()

	first := ic.idle()
	time.Sleep(5 * time.Millisecond)
	second := ic.idle()
	if second <= first {
		t.Fatalf("idle should grow without activity: first=%v second=%v", first, second)
	}

	if _, err := ic.Write([]byte("x")); err != nil {
		t.Fatalf("write: %v", err)
	}
	afterWrite := ic.idle()
	if afterWrite > second {
		t.Fatalf("idle should reset after Write: before=%v after=%v", second, afterWrite)
	}
}

// TestIdleConnCloseIsIdempotent：Close 可被多次调用且不 panic。
func TestIdleConnCloseIsIdempotent(t *testing.T) {
	mc := newMockConn()
	ic := newIdleConn(mc, 30*time.Second)
	if ic == nil {
		t.Fatal("expected non-nil idleConn")
	}
	if err := ic.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := ic.Close(); err != nil {
		t.Fatalf("second close should be no-op, got: %v", err)
	}
}

// TestIdleConnWatchRunsAndExits：watch 必须真的跑起来（真的执行过 renew），
// 并且在 Close 之后真的退出。
//
// 旧版本这条用例里那个 goroutine 根本没运行 idleConn.watch()，只是在等一个
// 由测试自己关闭的 channel，所以它既证明不了 watcher 跑过，也证明不了它退出。
func TestIdleConnWatchRunsAndExits(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	const timeout = 400 * time.Millisecond
	ic := newIdleConnWithTick(server, timeout, 10*time.Millisecond)
	if ic == nil {
		t.Fatal("expected non-nil idleConn")
	}

	// 构造时装入的是裸 timeout。
	if _, installed := ic.snapshot(); installed != timeout {
		t.Fatalf("initial installed = %v, want %v", installed, timeout)
	}

	// 制造一次活动，watch 必须把 deadline 续到 lastSeen+timeout。
	time.Sleep(20 * time.Millisecond)
	ic.touch()

	waitFor(t, time.Second, func() bool {
		seen, installed := ic.snapshot()
		return installed == seen+timeout
	}, "watch never renewed the deadline to lastSeen+timeout")

	if err := ic.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	select {
	case <-ic.done:
	default:
		t.Fatal("watch goroutine did not exit after Close")
	}
}

// TestIdleConnReadReturnsTimeoutWhenIdle：真空闲时阻塞中的读必须真的返回超时错误，
// 而不是永远挂着。这是"释放僵死连接 worker"这个修复的核心行为。
func TestIdleConnReadReturnsTimeoutWhenIdle(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	const timeout = 150 * time.Millisecond
	ic := newIdleConnWithTick(server, timeout, 15*time.Millisecond)
	if ic == nil {
		t.Fatal("expected non-nil idleConn")
	}
	defer ic.Close()

	begin := time.Now()
	n, err := ic.Read(make([]byte, 8))
	elapsed := time.Since(begin)

	if n != 0 {
		t.Fatalf("read returned %d bytes from an idle connection", n)
	}
	if err == nil {
		t.Fatal("idle read returned nil error, connection should have timed out")
	}
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("idle read error = %v, want a timeout error", err)
	}
	if elapsed < timeout {
		t.Fatalf("read returned after %v, before the %v timeout", elapsed, timeout)
	}
	if elapsed > 8*timeout {
		t.Fatalf("read returned after %v, far past the %v timeout", elapsed, timeout)
	}
}

// TestIdleConnWriteReturnsTimeoutWhenIdle：写方向同样必须超时退出。
func TestIdleConnWriteReturnsTimeoutWhenIdle(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	const timeout = 150 * time.Millisecond
	ic := newIdleConnWithTick(server, timeout, 15*time.Millisecond)
	if ic == nil {
		t.Fatal("expected non-nil idleConn")
	}
	defer ic.Close()

	begin := time.Now()
	// net.Pipe 的对端不读，Write 会一直阻塞到 deadline 到期。
	n, err := ic.Write([]byte("blocked"))
	elapsed := time.Since(begin)

	if n != 0 {
		t.Fatalf("write reported %d bytes on an idle connection", n)
	}
	if err == nil {
		t.Fatal("idle write returned nil error, connection should have timed out")
	}
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("idle write error = %v, want a timeout error", err)
	}
	if elapsed < timeout {
		t.Fatalf("write returned after %v, before the %v timeout", elapsed, timeout)
	}
	if elapsed > 8*timeout {
		t.Fatalf("write returned after %v, far past the %v timeout", elapsed, timeout)
	}
}

// TestIdleConnActiveTrafficDefersTimeout 覆盖维护者提的那个窗口：
// 一个方向持续活动、另一个方向一直阻塞在 Read 上时，连接不能被判超时。
//
// 这正是"watch 读到旧 lastSeen 之后、SetDeadline 之前来了新活动"会造成
// 的症状：装进内核的 deadline 早于 lastSeen+timeout，阻塞中的读被提前唤醒。
// 现在 lastSeen 更新与 deadline 装入共用一把锁，读必须一直挂到活动停止，
// 等活动停了才允许超时返回。
func TestIdleConnActiveTrafficDefersTimeout(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	const timeout = 150 * time.Millisecond
	ic := newIdleConnWithTick(&discardingWriteConn{Conn: server}, timeout, 15*time.Millisecond)
	if ic == nil {
		t.Fatal("expected non-nil idleConn")
	}
	defer ic.Close()

	readErr := make(chan error, 1)
	go func() {
		_, err := ic.Read(make([]byte, 8))
		readErr <- err
	}()

	// 持续 4 个 timeout 的"出方向活跃"：读方向始终阻塞，不应被打断。
	stopActivity := time.After(4 * timeout)
	activity := time.NewTicker(timeout / 5)
	defer activity.Stop()

activityLoop:
	for {
		select {
		case err := <-readErr:
			t.Fatalf("blocked read returned while the peer was still active: %v", err)
		case <-stopActivity:
			break activityLoop
		case <-activity.C:
			if _, err := ic.Write([]byte("keepalive")); err != nil {
				t.Fatalf("activity write: %v", err)
			}
		}
	}

	// 活动停止后，读必须在有限时间内以超时错误返回。
	select {
	case err := <-readErr:
		var netErr net.Error
		if !errors.As(err, &netErr) || !netErr.Timeout() {
			t.Fatalf("read error after activity stopped = %v, want a timeout error", err)
		}
	case <-time.After(10 * timeout):
		t.Fatal("read never timed out after activity stopped")
	}
}

// TestIdleConnDeadlineCoversLastActivity 直接盯住那条不变量：
// 任何时刻已装入的 deadline 都不能早于 lastSeen（否则活连接会被提前掐断）。
//
// 用例在 watch 持续 tick 的同时高频制造活动，逐次采样 lastSeen 与 installed；
// 若 deadline 的装入与活动更新之间存在窗口，就会出现 installed < lastSeen
// 的采样点。
func TestIdleConnDeadlineCoversLastActivity(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	const timeout = 120 * time.Millisecond
	ic := newIdleConnWithTick(server, timeout, 10*time.Millisecond)
	if ic == nil {
		t.Fatal("expected non-nil idleConn")
	}
	defer ic.Close()

	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) {
		ic.touch()
		seen, installed := ic.snapshot()
		if installed < seen {
			t.Fatalf("installed deadline %v precedes last activity %v", installed, seen)
		}
		if installed < seen+timeout/2 {
			t.Fatalf("installed deadline %v does not cover half the activity window of %v", installed, seen)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestIdleConnImplementsNetConn：确保 idleConn 满足 net.Conn，
// 这样 manager 才能透明地替换。
func TestIdleConnImplementsNetConn(t *testing.T) {
	var _ net.Conn = (*idleConn)(nil)
}

// TestDefaultIdleTickIsPositiveAndDense：watch 周期必须为正（time.NewTicker(0)
// 会 panic），且对生产用的量级要明显小于 timeout，否则 deadline 可能在两次
// tick 之间提前到期。
func TestDefaultIdleTickIsPositiveAndDense(t *testing.T) {
	for _, timeout := range []time.Duration{
		time.Nanosecond, time.Microsecond, time.Millisecond, 90 * time.Millisecond,
		time.Second, 5 * time.Second, 2 * time.Minute, time.Hour,
	} {
		tick := defaultIdleTick(timeout)
		if tick <= 0 {
			t.Fatalf("timeout=%v produced non-positive tick %v", timeout, tick)
		}
		if timeout >= time.Second && tick > timeout/2 {
			t.Fatalf("timeout=%v produced tick %v, want <= %v", timeout, tick, timeout/2)
		}
	}
	if got := defaultIdleTick(2 * time.Minute); got != 40*time.Second {
		t.Fatalf("defaultIdleTick(2m) = %v, want 40s", got)
	}
}

// TestNewIdleConnAcceptsTinyTimeout：极小的 timeout 也不应让构造过程 panic。
func TestNewIdleConnAcceptsTinyTimeout(t *testing.T) {
	mc := newMockConn()
	ic := newIdleConn(mc, time.Nanosecond)
	if ic == nil {
		t.Fatal("expected non-nil idleConn")
	}
	if ic.tick <= 0 {
		t.Fatalf("tick = %v, want a positive interval", ic.tick)
	}
	if err := ic.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
