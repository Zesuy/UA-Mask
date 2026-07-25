package clock

import "time"

type Timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(time.Duration) bool
}

type Ticker interface {
	C() <-chan time.Time
	Stop()
}

type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
	AfterFunc(time.Duration, func()) Timer
	NewTimer(time.Duration) Timer
	NewTicker(time.Duration) Ticker
}

type RealClock struct{}

func (RealClock) Now() time.Time {
	return time.Now()
}

func (RealClock) After(duration time.Duration) <-chan time.Time {
	return time.After(duration)
}

func (RealClock) AfterFunc(duration time.Duration, callback func()) Timer {
	return realTimer{timer: time.AfterFunc(duration, callback)}
}

func (RealClock) NewTimer(duration time.Duration) Timer {
	return realTimer{timer: time.NewTimer(duration)}
}

func (RealClock) NewTicker(duration time.Duration) Ticker {
	return realTicker{ticker: time.NewTicker(duration)}
}

func OrReal(value Clock) Clock {
	if value == nil {
		return RealClock{}
	}
	return value
}

type realTimer struct {
	timer *time.Timer
}

func (timer realTimer) C() <-chan time.Time {
	return timer.timer.C
}

func (timer realTimer) Stop() bool {
	return timer.timer.Stop()
}

func (timer realTimer) Reset(duration time.Duration) bool {
	return timer.timer.Reset(duration)
}

type realTicker struct {
	ticker *time.Ticker
}

func (ticker realTicker) C() <-chan time.Time {
	return ticker.ticker.C
}

func (ticker realTicker) Stop() {
	ticker.ticker.Stop()
}
