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
//
// 传入的 tick 也会被夹到严格小于 timeout/2：这条不变量是"活跃连接不被误判"
// 的前提，不能靠调用方自觉。
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
	c.tick = clampIdleTick(c.tick, timeout)
	c.mu.Lock()
	c.installLocked(timeout)
	c.mu.Unlock()
	go c.watch()
	return c
}

// clampIdleTick 把 tick 收敛到 (0, timeout/2) 区间；tick <= 0 时取默认值。
func clampIdleTick(tick, timeout time.Duration) time.Duration {
	if tick <= 0 {
		return defaultIdleTick(timeout)
	}
	limit := timeout / 2
	if tick >= limit {
		tick = limit - 1
	}
	if tick <= 0 {
		tick = 1
	}
	return tick
}

// minIdleTickInterval 曾是"避免空转"的 1s 下限，现已移除，原因见 defaultIdleTick。

// defaultIdleTick 给出 watch 的检查周期：timeout/3，且恒为正。
//
// 为什么是 timeout/3 而不是"1s 下限 + 夹到 timeout/2"：
//
//	touch() 只把 deadline 续到 lastSeen+headroom（默认 timeout/2），
//	完整续期要等下一次 watch tick 补到 lastSeen+timeout。
//	因此必须保证 tick < headroom，否则补续期的时机撞上、甚至晚于已装 deadline，
//	"仍有流量"的连接就会被判空闲掐断——ticker 在负载下延迟几十毫秒是常态。
//
// 早年的写法是"先抬到 1s 下限、再夹到 timeout/2"，有两处错：
//  1. 在 timeout=2s 时正好落成 1s == timeout/2，余量归零；
//  2. 在 2s < timeout < 3s 时下限把 tick 抬到 1s，与 timeout/2 只差几十毫秒，
//     同样脆。
//
// 而 1s 下限本身也帮不上忙：它只在 timeout < 3s 时起作用，而生产配置的
// idle_timeout 是分钟级（2m -> tick 40s），根本触碰不到下限；短 timeout
// 只出现在测试里，那里反而需要更细的 tick。故直接取 timeout/3，
// 让 tick 与 deadline 之间恒有 timeout/6 的余量。
func defaultIdleTick(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return time.Nanosecond
	}
	tick := timeout / 3
	if tick <= 0 {
		// 亚纳秒级 timeout 除以 3 会截断成 0，而 time.NewTicker(0) 会 panic。
		tick = 1
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
	// 已装 deadline 若还盖得住这次的续期窗口，就不打断内核（热点路径零 syscall）；
	// 剩下那截由 watch 续到完整 timeout。
	//
	// 续期窗口必须严格大于 tick：touch 之后要保证"下一次 tick 一定会到来并
	// 补足 deadline"，否则 tick 一旦被负载推迟，deadline 会先于补续期到期，
	// 把仍有流量的连接判成空闲。取 timeout/2 满足 2*tick < timeout 的全部情形。
	headroom := c.timeout / 2
	if headroom <= c.tick {
		headroom = c.tick + (c.timeout-c.tick)/2
	}
	if headroom <= 0 || headroom > c.timeout {
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
