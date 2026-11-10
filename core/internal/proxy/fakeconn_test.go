package proxy

import (
	"errors"
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

// peerConn 返回对端连接，用于观察关闭是否真的传播出去。
func (f *fakeConn) peerConn() *net.TCPConn {
	return f.peer
}

// Closed 报告服务端这一侧的连接是否已被对方感知到关闭。
//
// 判断方式：服务端 ClientConn 被 Close 后，loopback 对端必然读到 EOF/Reset。
// 这才是真实的关闭语义，而不是替身内部改的一个标志位。
//
// 不能再用 (*net.TCPConn).File() 嗅探：在 Windows 上对正常 ESTABLISHED 的
// TCPConn 调 File() 会返回 "use of closed network connection"，于是**活连接
// 也被判定为已关闭**，使所有 "must be closed" 断言恒真、形同虚设。
//
// Read 的三种结果分别对应：
//   - 超时           -> 连接仍然活着（没关闭，也没数据）
//   - EOF / Reset    -> 已关闭
//   - 读到数据       -> 仍然活着
func (f *fakeConn) Closed() bool {
	if f.closed.Load() {
		return true
	}
	if f.conn == nil || f.peer == nil {
		return true
	}

	if err := f.peer.SetReadDeadline(time.Now().Add(probeTimeout)); err != nil {
		// 对端已不可用，视作已关闭
		f.closed.Store(true)
		return true
	}
	var buf [1]byte
	if _, err := f.peer.Read(buf[:]); err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return false
		}
		f.closed.Store(true)
		return true
	}
	// 读到数据说明连接仍然活着。
	return false
}

// probeTimeout 是 Closed() 探测时愿意等的时长，需小于测试里的轮询间隔上限。
const probeTimeout = 30 * time.Millisecond

// Close 关闭连接对。
func (f *fakeConn) Close() {
	f.once.Do(func() {
		_ = f.conn.Close()
		if f.peer != nil {
			_ = f.peer.Close()
		}
	})
}
