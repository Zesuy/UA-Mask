package proxy

import (
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// fakeConn 是 net.TCPConn 的测试替身。
// 由于 model.AcceptedSession.ClientConn 是 *net.TCPConn（具体类型而非接口），
// 无法直接注入接口实现；这里用真实 TCP 连接对（loopback listener）来构造，
// 既保留类型约束，又能在测试里观察 Close 状态。
type fakeConn struct {
	conn   *net.TCPConn
	peer   *net.TCPConn
	closed atomic.Bool
	once   sync.Once
}

// newFakeConn 创建一对 loopback TCP 连接，返回服务端那一半。
func newFakeConn() *fakeConn {
	ln, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		panic("fakeConn: listen failed: " + err.Error())
	}
	defer ln.Close()

	type res struct {
		c   *net.TCPConn
		err error
	}
	ch := make(chan res, 1)
	go func() {
		c, err := ln.AcceptTCP()
		ch <- res{c: c, err: err}
	}()

	client, err := net.DialTCP("tcp", nil, ln.Addr().(*net.TCPAddr))
	if err != nil {
		panic("fakeConn: dial failed: " + err.Error())
	}

	select {
	case r := <-ch:
		if r.err != nil {
			panic("fakeConn: accept failed: " + r.err.Error())
		}
		// r.c 是服务端侧，client 是客户端侧；两者等价，取服务端侧
		return &fakeConn{conn: r.c, peer: client}
	case <-time.After(time.Second):
		panic("fakeConn: accept timeout")
	}
}

// tcpConn 返回底层 *net.TCPConn。
func (f *fakeConn) tcpConn() *net.TCPConn {
	return f.conn
}

// Closed 报告连接是否已被关闭。
// 直接检查 conn 是否还被跟踪（net.TCPConn 关闭后 File() 会报错），
// 比"写探针"更可靠：写探针在 loopback 上有缓冲，关闭瞬间仍可能成功。
func (f *fakeConn) Closed() bool {
	if f.closed.Load() {
		return true
	}
	if f.conn == nil {
		return true
	}
	// 关闭后的 *net.TCPConn，File() 会返回 use of closed network connection
	fd, err := f.conn.File()
	if err != nil {
		f.closed.Store(true)
		return true
	}
	_ = fd.Close()
	return false
}

// Close 关闭连接对。
func (f *fakeConn) Close() {
	f.once.Do(func() {
		_ = f.conn.Close()
		if f.peer != nil {
			_ = f.peer.Close()
		}
	})
}
