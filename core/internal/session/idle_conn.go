package session

import (
	"net"
	"sync/atomic"
	"time"
)

// idleConn 包装 net.Conn，为连接提供"空闲超时"能力。
//
// 动机：上层 io.Copy 会一直阻塞到对端关闭，没有超时。若客户端异常消失
// 而 TCP 未感知（常见于 NAT/防火墙只丢包不回 RST 的场景），worker 会被
// 这个僵死连接长期占用；worker 池耗尽后 acceptLoop 会阻塞，表现为
// "进程还在、端口在听、但 LAN 全断"。空闲超时让僵死连接在有限时间内
// 释放 worker。
//
// 设计要点：
//   - 不改变读写语义，只附加超时；
//   - 用单调时钟记录最后一次活动，避免系统时间被 NTP 调整时误判；
//   - deadline 由后台 ticker 基于 lastSeen 推算，避免每次 Read/Write 都
//     触发一次 SetDeadline 系统调用（热点路径零额外 syscall）；
//   - timeout <= 0 时 newIdleConn 返回 nil，调用方回退到裸 conn。
type idleConn struct {
	net.Conn
	timeout  time.Duration
	start    time.Time    // 单调计时的基准点
	lastSeen atomic.Int64 // 自 start 起经过的纳秒（单调递增）
	stop     chan struct{}
}

// newIdleConn 返回包装后的连接；timeout <= 0 时返回 nil。
func newIdleConn(conn net.Conn, timeout time.Duration) *idleConn {
	if timeout <= 0 {
		return nil
	}
	c := &idleConn{
		Conn:    conn,
		timeout: timeout,
		start:   time.Now(),
		stop:    make(chan struct{}),
	}
	c.lastSeen.Store(0)
	_ = conn.SetDeadline(time.Now().Add(timeout))
	go c.watch()
	return c
}

// touch 记录一次活动。用 time.Since(start) 取单调时长，
// 不受 wall clock 跳变影响；同值覆盖是可接受的（分辨率上限）。
func (c *idleConn) touch() {
	c.lastSeen.Store(int64(time.Since(c.start)))
}

// idle 返回当前连接已空闲的时长。
func (c *idleConn) idle() time.Duration {
	return time.Since(c.start) - time.Duration(c.lastSeen.Load())
}

// watch 周期性重算 deadline：
//   - 有活动 -> lastSeen 前移 -> deadline 随之前移，连接不会被打断；
//   - 无活动 -> lastSeen 不动 -> deadline 固定为 lastSeen+timeout，
//     到期后 Read/Write 返回超时错误，io.Copy 退出、worker 释放。
func (c *idleConn) watch() {
	interval := c.timeout / 3
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			idle := c.idle()
			if idle >= c.timeout {
				// 已超时：不再续期，让已设的 deadline 自然到期。
				// 本轮之后仍会继续检查，防止 deadline 被上层改写。
				continue
			}
			_ = c.Conn.SetDeadline(time.Now().Add(c.timeout - idle))
		}
	}
}

func (c *idleConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.touch()
	}
	return n, err
}

func (c *idleConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 {
		c.touch()
	}
	return n, err
}

// Close 停止后台 watch 并关闭底层连接；可重复调用。
func (c *idleConn) Close() error {
	select {
	case <-c.stop:
		// 已关闭，避免重复 close(channel) panic
	default:
		close(c.stop)
	}
	return c.Conn.Close()
}
