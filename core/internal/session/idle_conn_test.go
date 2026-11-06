package session

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockConn 是可注入的 net.Conn，用 channel 控制读行为，避免依赖真实时间。
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

	before := ic.lastSeen.Load()

	// 单调时钟精度不足以区分同纳秒内的两次调用，
	// 因此显式推进时间再断言，避免测试本身变得脆弱。
	time.Sleep(2 * time.Millisecond)

	// 写入应 touch
	if _, err := ic.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	afterWrite := ic.lastSeen.Load()
	if afterWrite <= before {
		t.Fatalf("Write did not advance lastSeen: before=%d after=%d", before, afterWrite)
	}

	time.Sleep(2 * time.Millisecond)

	// 读取应 touch（先投递数据）
	mc.readCh <- []byte("pong")
	buf := make([]byte, 8)
	if _, err := ic.Read(buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	afterRead := ic.lastSeen.Load()
	if afterRead <= afterWrite {
		t.Fatalf("Read did not advance lastSeen: afterWrite=%d afterRead=%d", afterWrite, afterRead)
	}
}

// TestIdleConnIdleGrowsWithInactivity：无读写时 idle() 应随时长增长，
// 这是超时判据的基础。
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

	// 一次写入后 idle 应回落到接近 0
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

// TestIdleConnWatchStopsOnClose：Close 之后后台 watch goroutine 必须退出，
// 否则每连接泄漏一个 goroutine。
func TestIdleConnWatchStopsOnClose(t *testing.T) {
	mc := newMockConn()
	ic := newIdleConn(mc, time.Second) // interval = 1/3s -> 夹到 1s
	if ic == nil {
		t.Fatal("expected non-nil idleConn")
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// watch 内部 select 会在 stop 关闭后返回
		<-done
	}()

	if err := ic.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	close(done)

	waitDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("watch goroutine did not stop after Close")
	}

	// stop channel 应已关闭
	select {
	case <-ic.stop:
	default:
		t.Fatal("stop channel should be closed after Close()")
	}
}

// TestIdleConnImplementsNetConn：确保 idleConn 满足 net.Conn，
// 这样 manager 才能透明地替换。
func TestIdleConnImplementsNetConn(t *testing.T) {
	var _ net.Conn = (*idleConn)(nil)
}
