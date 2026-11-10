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

// TestDefaultIdleTickIsStrictlyUnderHalfTimeout：watch 周期必须为正
// （time.NewTicker(0) 会 panic），**严格**小于 timeout/2，且不得长于 timeout。
//
// 为什么是严格：touch() 只把 deadline 续到 lastSeen+headroom（= timeout/2），
// 完整续期要等下一次 tick。tick >= timeout/2 时两者首尾相接甚至倒挂，
// ticker 一旦被负载推迟几十毫秒，deadline 就会先到期，把仍有流量的连接判成空闲。
//
// 历史回归点：旧实现"先抬到 1s 下限、再夹到 timeout/2"，
//   - timeout=2s 时正好得到 1s == timeout/2，余量归零；
//   - 1ns~1s 区间 tick 被抬成 1s，直接长于 timeout 本身。
func TestDefaultIdleTickIsStrictlyUnderHalfTimeout(t *testing.T) {
	timeouts := []time.Duration{
		time.Nanosecond, 3 * time.Nanosecond, time.Microsecond, time.Millisecond,
		10 * time.Millisecond, 90 * time.Millisecond, 199 * time.Millisecond,
		200 * time.Millisecond, 500 * time.Millisecond, 999 * time.Millisecond,
		time.Second, 1500 * time.Millisecond, 2 * time.Second, 3 * time.Second,
		5 * time.Second, 10 * time.Second, time.Minute, 2 * time.Minute, time.Hour,
		// 2s <= timeout < 3s 是旧实现里"下限抬高后再夹回"真正咬人的区间：
		// timeout/3 < 1s，下限抬到 1s，而 1s 又 >= timeout/2。
		2100 * time.Millisecond, 2200 * time.Millisecond, 2500 * time.Millisecond,
		2800 * time.Millisecond, 2999 * time.Millisecond,
	}
	for _, timeout := range timeouts {
		tick := defaultIdleTick(timeout)

		// ① 必须为正：time.NewTicker(0) 会 panic。
		if tick <= 0 {
			t.Fatalf("timeout=%v produced non-positive tick %v", timeout, tick)
		}
		// ② 不得长于 timeout 本身（旧实现在 1ns~1s 区间违反这条）。
		if tick > timeout {
			t.Fatalf("timeout=%v produced tick %v, which exceeds the timeout itself", timeout, tick)
		}
		// ③ timeout/2 是整数除法：half <= 1ns 时不存在"严格小于 half 的正 Duration"
		//    （最小正周期就是 1ns），此时只要求取到 1ns。
		half := timeout / 2
		if half <= 1 {
			if tick != 1 {
				t.Fatalf("timeout=%v (half=%v) produced tick %v, want the 1ns floor", timeout, half, tick)
			}
			continue
		}
		// ④ 核心不变量：严格小于半个 timeout，留出补续期的余量。
		if tick >= half {
			t.Fatalf("timeout=%v produced tick %v, want strictly < half timeout %v", timeout, tick, half)
		}
	}

	// 精确值：保持 timeout/3，不设下限、不夹上限。
	for _, tc := range []struct {
		timeout time.Duration
		want    time.Duration
	}{
		{2 * time.Minute, 40 * time.Second},
		{30 * time.Second, 10 * time.Second},
		{5 * time.Second, 1666666666 * time.Nanosecond},
		{3 * time.Second, time.Second},
		{2500 * time.Millisecond, 833333333 * time.Nanosecond}, // 旧实现在此被抬到 1s
		{2 * time.Second, 666666666 * time.Nanosecond},         // 旧实现在此恰好 1s == timeout/2
		{1500 * time.Millisecond, 500 * time.Millisecond},
		{time.Second, 333333333 * time.Nanosecond},
	} {
		if got := defaultIdleTick(tc.timeout); got != tc.want {
			t.Fatalf("defaultIdleTick(%v) = %v, want %v", tc.timeout, got, tc.want)
		}
	}
}

// TestNewIdleConnClampsCallerTick 确认显式传入过大的 tick 会被夹回安全区间，
// 这条不变量不能只靠调用方自觉。
func TestNewIdleConnClampsCallerTick(t *testing.T) {
	const timeout = 100 * time.Millisecond
	for _, requested := range []time.Duration{
		timeout, timeout / 2, timeout * 2, time.Hour,
	} {
		client, server := net.Pipe()
		ic := newIdleConnWithTick(server, timeout, requested)
		if ic == nil {
			client.Close()
			t.Fatal("expected non-nil idleConn")
		}
		if ic.tick >= timeout/2 {
			t.Fatalf("requested tick %v -> %v, want strictly < %v", requested, ic.tick, timeout/2)
		}
		if ic.tick <= 0 {
			t.Fatalf("requested tick %v -> %v, want positive", requested, ic.tick)
		}
		ic.Close()
		client.Close()
	}
}

// TestIdleConnActivityRightAfterTickIsNotCut 复现评审提到的时序：
// 活动刚好发生在一次 watch tick 之后。
//
// tick 紧跟在 tick 之后发生时，touch 只把 deadline 续到 lastSeen+headroom，
// 而下一轮补续期要等一个完整 tick 之后。只要 headroom > tick，补续期就还来得及，
// 连接不会被误判超时。旧实现在 tick >= timeout/2 时这里会失败。
func TestIdleConnActivityRightAfterTickIsNotCut(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	const timeout = 200 * time.Millisecond
	// 故意使用默认 tick（timeout/3 ≈ 66ms），它就是"tick 之后"窗口的来源。
	ic := newIdleConnWithTick(&discardingWriteConn{Conn: server}, timeout, 0)
	if ic == nil {
		t.Fatal("expected non-nil idleConn")
	}
	defer ic.Close()

	if ic.tick >= timeout/2 {
		t.Fatalf("precondition failed: tick %v must be < %v", ic.tick, timeout/2)
	}

	readErr := make(chan error, 1)
	go func() {
		_, err := ic.Read(make([]byte, 8))
		readErr <- err
	}()

	// 让连接活得比好几个 timeout 久，并且活动卡在"刚过 tick"的相位上：
	// 高频 touch 会覆盖各种相位，其中包括紧跟在 tick 之后的那些。
	deadline := time.Now().Add(4 * timeout)
	tick := time.NewTicker(timeout / 8)
	defer tick.Stop()

	for time.Now().Before(deadline) {
		select {
		case err := <-readErr:
			t.Fatalf("blocked read returned while the peer was still active: %v", err)
		case <-tick.C:
			if _, err := ic.Write([]byte("keepalive")); err != nil {
				t.Fatalf("activity write: %v", err)
			}
		}
	}

	// 活动停止后，仍应正常以超时结束。
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

// TestIdleConnShortTimeoutWithActivity 覆盖"短超时 + 持续活动"这个组合：
// 短 timeout 下 tick 被夹得很小，最容易暴露续期与 deadline 的竞争。
func TestIdleConnShortTimeoutWithActivity(t *testing.T) {
	for _, timeout := range []time.Duration{
		40 * time.Millisecond, 80 * time.Millisecond, 150 * time.Millisecond,
	} {
		// 每一轮都要一对新的 pipe：上一轮 Close 会关掉底层连接。
		client, server := net.Pipe()

		ic := newIdleConnWithTick(&discardingWriteConn{Conn: server}, timeout, 0)
		if ic == nil {
			client.Close()
			t.Fatal("expected non-nil idleConn")
		}

		readErr := make(chan error, 1)
		go func() {
			_, err := ic.Read(make([]byte, 8))
			readErr <- err
		}()

		// 让连接活过 10 个 timeout，期间保持出方向有流量。
		stop := time.Now().Add(10 * timeout)
		pulse := time.NewTicker(timeout / 4)
		aborted := false
		for time.Now().Before(stop) {
			select {
			case err := <-readErr:
				pulse.Stop()
				ic.Close()
				client.Close()
				t.Fatalf("timeout=%v: read cut off while active: %v", timeout, err)
			case <-pulse.C:
				if _, err := ic.Write([]byte("x")); err != nil {
					aborted = true
					break
				}
			}
			if aborted {
				break
			}
		}
		pulse.Stop()
		ic.Close()
		client.Close()

		select {
		case <-readErr:
		case <-time.After(timeout * 5):
			t.Fatalf("timeout=%v: read never returned after Close", timeout)
		}
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
