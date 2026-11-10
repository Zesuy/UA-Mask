package session

import (
	"net"
	"sync"
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
// deadline 的语义是 lastSeen + timeout：
//   - 有活动 -> lastSeen 前移 -> deadline 随之前移，连接不会被打断；
//   - 无活动 -> lastSeen 不动 -> deadline 固定，到期后 Read/Write 返回
//     超时错误，io.Copy 退出、worker 释放。
//
// 关于并发（这是本类型唯一容易写错的地方）：
// lastSeen 的更新与 deadline 的装入共用同一把锁。若像早期版本那样"先读
// lastSeen、算完再 SetDeadline"，新活动可以插在两者之间，于是装进内核的
// deadline 会早于 lastSeen+timeout；此时另一方向正阻塞在 Read 上，就会在
// 明明还有流量的情况下被判超时，把活连接掐掉。加锁后这两步对外表现为
// 一个原子动作，deadline 永远不会落在最近一次活动的前面。
//
// 成本：绝大多数 Read/Write 只做一次原子赋值，不产生系统调用；只有已装
// deadline 覆盖不到"新活动 + 半个超时"时才真正续期一次。
type idleConn struct {
	net.Conn
	timeout time.Duration
	start   time.Time

	// tick 是 watch 的检查周期，构造时确定后只读。
	tick time.Duration

	stop chan struct{}
	done chan struct{}

	mu        sync.Mutex
	lastSeen  time.Duration // 距 start 的最后一次活动（单调）
	installed time.Duration // 已装入内核的 deadline（距 start），只增不减
}

// newIdleConn 返回包装后的连接；timeout <= 0 时返回 nil。
func newIdleConn(conn net.Conn, timeout time.Duration) *idleConn {
	return newIdleConnWithTick(conn, timeout, 0)
}

// newIdleConnWithTick 允许调用方固定 watch 周期；tick <= 0 时按 timeout 推导。
// 生产路径只走 newIdleConn；测试用它把周期钉到毫秒级，免得为了等一次 tick
// 把用例拖到秒级。
func newIdleConnWithTick(conn net.Conn, timeout, tick time.Duration) *idleConn {
	if timeout <= 0 {
		return nil
	}
	c := &idleConn{
		Conn:    conn,
		timeout: timeout,
		start:   time.Now(),
		tick:    tick,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	if c.tick <= 0 {
		c.tick = defaultIdleTick(timeout)
	}
	c.mu.Lock()
	c.installLocked(timeout)
	c.mu.Unlock()
	go c.watch()
	return c
}

// defaultIdleTick 取 timeout/3，并保证周期小于 timeout/2：
// 太密会空转，太疏则 deadline 可能在两次 tick 之间提前到期。
func defaultIdleTick(timeout time.Duration) time.Duration {
	tick := timeout / 3
	if tick < time.Second {
		tick = time.Second
	}
	if half := timeout / 2; half > 0 && tick > half {
		tick = half
	}
	// timeout 小到 time.Second 都显不出差别时，退回一个必定为正的周期，
	// 否则 time.NewTicker(0) 会 panic。
	if tick <= 0 {
		tick = timeout
	}
	return tick
}

// installLocked 把 deadline 推到 start+want；绝不回退，调用方需持有 mu。
func (c *idleConn) installLocked(want time.Duration) {
	if c.installed >= want {
		return
	}
	c.installed = want
	_ = c.Conn.SetDeadline(c.start.Add(want))
}

// touch 记录一次活动；用 time.Since(start) 取单调时长，不受系统时间跳变影响。
func (c *idleConn) touch() {
	seen := time.Since(c.start)
	c.mu.Lock()
	defer c.mu.Unlock()
	if seen <= c.lastSeen {
		return
	}
	c.lastSeen = seen
	// 已装 deadline 若还盖得住"新活动 + 半个超时"，就不打断内核；剩下的
	// 交给 watch 续到完整 timeout。这样活跃连接在任何方向上都不会被判超时。
	headroom := c.timeout / 2
	if headroom <= 0 {
		headroom = c.timeout
	}
	c.installLocked(seen + headroom)
}

// idle 返回当前连接已空闲的时长。
func (c *idleConn) idle() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.idleLocked()
}

func (c *idleConn) idleLocked() time.Duration {
	elapsed := time.Since(c.start)
	if elapsed <= c.lastSeen {
		return 0
	}
	return elapsed - c.lastSeen
}

// watch 周期性把 deadline 续到 lastSeen+timeout；真空闲时不再续期，
// 让已装入的 deadline 到期，把阻塞中的 Read/Write 唤醒成超时错误。
func (c *idleConn) watch() {
	defer close(c.done)
	ticker := time.NewTicker(c.tick)
	defer ticker.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			c.renew()
		}
	}
}

func (c *idleConn) renew() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.idleLocked() >= c.timeout {
		return
	}
	c.installLocked(c.lastSeen + c.timeout)
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
// 它会等 watch 真正退出后才返回，避免 Close 之后还有 goroutine 继续改
// deadline，也让"watch 是否泄漏"可以被测试直接断言。
func (c *idleConn) Close() error {
	select {
	case <-c.stop:
	default:
		close(c.stop)
	}
	<-c.done
	return c.Conn.Close()
}
