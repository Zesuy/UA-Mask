package manualclock

import (
	"sync"
	"time"

	runtimeclock "UAmask/internal/clock"
)

type Clock struct {
	advanceMu sync.Mutex
	mu        sync.Mutex
	now       time.Time
	nextID    uint64
	timers    map[*scheduled]struct{}
}

type scheduled struct {
	owner    *Clock
	id       uint64
	due      time.Time
	interval time.Duration
	channel  chan time.Time
	callback func()
	active   bool
	ticker   bool
}

type Timer struct {
	scheduled *scheduled
}

type Ticker struct {
	scheduled *scheduled
}

var _ runtimeclock.Clock = (*Clock)(nil)
var _ runtimeclock.Timer = (*Timer)(nil)
var _ runtimeclock.Ticker = (*Ticker)(nil)

func New(start time.Time) *Clock {
	return &Clock{
		now:    start,
		timers: make(map[*scheduled]struct{}),
	}
}

func (clock *Clock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *Clock) After(duration time.Duration) <-chan time.Time {
	return clock.NewTimer(duration).C()
}

func (clock *Clock) AfterFunc(duration time.Duration, callback func()) runtimeclock.Timer {
	return &Timer{scheduled: clock.schedule(duration, 0, nil, callback, false)}
}

func (clock *Clock) NewTimer(duration time.Duration) runtimeclock.Timer {
	return &Timer{scheduled: clock.schedule(duration, 0, make(chan time.Time, 1), nil, false)}
}

func (clock *Clock) NewTicker(duration time.Duration) runtimeclock.Ticker {
	if duration <= 0 {
		panic("non-positive interval for NewTicker")
	}
	return &Ticker{scheduled: clock.schedule(duration, duration, make(chan time.Time, 1), nil, true)}
}

func (clock *Clock) Advance(duration time.Duration) {
	if duration < 0 {
		panic("manual clock cannot advance backwards")
	}

	clock.advanceMu.Lock()
	defer clock.advanceMu.Unlock()

	clock.mu.Lock()
	target := clock.now.Add(duration)
	clock.mu.Unlock()

	for {
		clock.mu.Lock()
		next := clock.nextDueLocked(target)
		if next == nil {
			clock.now = target
			clock.mu.Unlock()
			return
		}

		firedAt := next.due
		clock.now = firedAt
		if next.ticker {
			next.due = next.due.Add(next.interval)
			next.id = clock.allocateIDLocked()
		} else {
			next.active = false
		}
		channel := next.channel
		callback := next.callback
		clock.mu.Unlock()

		if channel != nil {
			select {
			case channel <- firedAt:
			default:
			}
		}
		if callback != nil {
			callback()
		}
	}
}

func (clock *Clock) Pending() int {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	count := 0
	for timer := range clock.timers {
		if timer.active {
			count++
		}
	}
	return count
}

func (clock *Clock) Due() int {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	count := 0
	for timer := range clock.timers {
		if timer.active && !timer.due.After(clock.now) {
			count++
		}
	}
	return count
}

func (clock *Clock) schedule(duration, interval time.Duration, channel chan time.Time, callback func(), ticker bool) *scheduled {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	if duration < 0 {
		duration = 0
	}
	timer := &scheduled{
		owner:    clock,
		id:       clock.allocateIDLocked(),
		due:      clock.now.Add(duration),
		interval: interval,
		channel:  channel,
		callback: callback,
		active:   true,
		ticker:   ticker,
	}
	clock.timers[timer] = struct{}{}
	return timer
}

func (clock *Clock) allocateIDLocked() uint64 {
	clock.nextID++
	return clock.nextID
}

func (clock *Clock) nextDueLocked(target time.Time) *scheduled {
	var next *scheduled
	for timer := range clock.timers {
		if !timer.active || timer.due.After(target) {
			continue
		}
		if next == nil || timer.due.Before(next.due) || (timer.due.Equal(next.due) && timer.id < next.id) {
			next = timer
		}
	}
	return next
}

func (timer *Timer) C() <-chan time.Time {
	return timer.scheduled.channel
}

func (timer *Timer) Stop() bool {
	return timer.scheduled.stop()
}

func (timer *Timer) Reset(duration time.Duration) bool {
	scheduled := timer.scheduled
	scheduled.owner.mu.Lock()
	defer scheduled.owner.mu.Unlock()
	if duration < 0 {
		duration = 0
	}
	wasActive := scheduled.active
	scheduled.active = true
	scheduled.due = scheduled.owner.now.Add(duration)
	scheduled.id = scheduled.owner.allocateIDLocked()
	return wasActive
}

func (ticker *Ticker) C() <-chan time.Time {
	return ticker.scheduled.channel
}

func (ticker *Ticker) Stop() {
	_ = ticker.scheduled.stop()
}

func (timer *scheduled) stop() bool {
	timer.owner.mu.Lock()
	defer timer.owner.mu.Unlock()
	wasActive := timer.active
	timer.active = false
	return wasActive
}
