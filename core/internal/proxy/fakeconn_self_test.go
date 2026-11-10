package proxy

import (
	"net"
	"testing"
	"time"
)

// TestFakeConnClosedTracksRealState 守住测试替身本身的可信度。
//
// 历史问题：Closed() 用 (*net.TCPConn).File() 判断存活，但在 Windows 上
// 对已建立连接的 TCPConn 调 File() 会返回 "use of closed network connection"
// 之类的错误，于是**一个从未被关闭的完整连接也报 Closed()==true**。
// 后果是所有 "must be closed" 断言恒真，等于没测。
//
// 现在 fakeConn 记录真实的 Close 调用，本用例确保：
//   - 全新连接 Closed() 必须为 false
//   - 调用 Close() 之后必须为 true
//
// 这两条一旦退回去，池满那条回归测试就会重新变成空断言。
func TestFakeConnClosedTracksRealState(t *testing.T) {
	conn := newFakeConn()
	defer conn.Close()

	if conn.Closed() {
		t.Fatal("fresh fakeConn reports Closed()=true; 'must be closed' assertions would be vacuous")
	}
	if conn.Closed() {
		t.Fatal("Closed() must be stable for a live connection")
	}

	if err := conn.tcpConn().Close(); err != nil {
		t.Fatalf("close underlying conn: %v", err)
	}
	if !conn.Closed() {
		t.Fatal("Closed() must report true after the connection is really closed")
	}
}

// TestFakeConnPeerObservesClose 确认关闭后对端真的能看到 EOF，
// 而不是只在替身内部改了个标志位。
func TestFakeConnPeerObservesClose(t *testing.T) {
	conn := newFakeConn()
	defer conn.Close()

	_ = conn.tcpConn().Close()

	peer := conn.peerConn()
	if peer == nil {
		t.Fatal("fakeConn has no peer to observe the close")
	}
	_ = peer.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	if _, err := peer.Read(buf); err == nil {
		t.Fatal("peer read succeeded after close; expected EOF/reset")
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("peer read timed out: close was not propagated to the peer")
	}
}
