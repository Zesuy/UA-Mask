package events

import (
	"context"
	"sync"

	"UAmask/internal/model"
)

const defaultQueueCapacity = 10000

type Sink interface {
	Append(event model.SessionEvent)
}

type Dispatcher struct {
	mu       sync.Mutex
	cond     *sync.Cond
	sinks    []Sink
	queue    []model.SessionEvent
	head     int
	size     int
	running  bool
	stopping bool
	closed   bool
	dropped  uint64
	active   int
	busy     bool
	idle     chan struct{}
	wg       sync.WaitGroup
}

func NewDispatcher(sinks ...Sink) *Dispatcher {
	return NewDispatcherWithCapacity(defaultQueueCapacity, sinks...)
}

func NewDispatcherWithCapacity(capacity int, sinks ...Sink) *Dispatcher {
	if capacity <= 0 {
		capacity = 1
	}
	idle := make(chan struct{})
	close(idle)
	dispatcher := &Dispatcher{
		sinks: sinks,
		queue: make([]model.SessionEvent, capacity),
		idle:  idle,
	}
	dispatcher.cond = sync.NewCond(&dispatcher.mu)
	return dispatcher
}

func (d *Dispatcher) Add(sinks ...Sink) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	d.sinks = append(d.sinks, sinks...)
}

func (d *Dispatcher) Start() {
	if d == nil {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running || d.closed {
		return
	}

	d.running = true
	d.stopping = false
	d.wg.Add(1)
	go d.loop()
}

func (d *Dispatcher) Stop() {
	if d == nil {
		return
	}

	d.mu.Lock()
	if !d.running {
		d.closed = true
		d.mu.Unlock()
		return
	}
	d.stopping = true
	d.closed = true
	d.cond.Signal()
	d.mu.Unlock()

	d.wg.Wait()
}

func (d *Dispatcher) Append(event model.SessionEvent) {
	_ = d.TryAppend(event)
}

func (d *Dispatcher) TryAppend(event model.SessionEvent) bool {
	if d == nil {
		return false
	}

	d.mu.Lock()
	if d.closed || d.stopping {
		d.dropped++
		d.mu.Unlock()
		return false
	}
	if d.running {
		if d.size == len(d.queue) {
			d.dropped++
			d.mu.Unlock()
			return false
		}
		d.markBusyLocked()
		tail := (d.head + d.size) % len(d.queue)
		d.queue[tail] = event
		d.size++
		d.cond.Signal()
		d.mu.Unlock()
		return true
	}
	sinks := append([]Sink(nil), d.sinks...)
	d.markBusyLocked()
	d.active++
	d.mu.Unlock()

	dispatch(event, sinks)
	d.mu.Lock()
	d.active--
	d.markIdleLocked()
	d.mu.Unlock()
	return true
}

func (d *Dispatcher) WaitIdle(ctx context.Context) error {
	if d == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	d.mu.Lock()
	if !d.busy {
		d.mu.Unlock()
		return nil
	}
	idle := d.idle
	d.mu.Unlock()

	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *Dispatcher) Dropped() uint64 {
	if d == nil {
		return 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.dropped
}

func (d *Dispatcher) loop() {
	defer d.wg.Done()

	for {
		d.mu.Lock()
		for d.size == 0 && !d.stopping {
			d.cond.Wait()
		}
		if d.size == 0 && d.stopping {
			d.running = false
			d.markIdleLocked()
			d.mu.Unlock()
			return
		}

		event := d.queue[d.head]
		d.queue[d.head] = model.SessionEvent{}
		d.head = (d.head + 1) % len(d.queue)
		d.size--
		d.active++
		sinks := append([]Sink(nil), d.sinks...)
		d.mu.Unlock()

		dispatch(event, sinks)
		d.mu.Lock()
		d.active--
		d.markIdleLocked()
		d.mu.Unlock()
	}
}

func (d *Dispatcher) markBusyLocked() {
	if d.busy {
		return
	}
	d.busy = true
	d.idle = make(chan struct{})
}

func (d *Dispatcher) markIdleLocked() {
	if !d.busy || d.size != 0 || d.active != 0 {
		return
	}
	d.busy = false
	close(d.idle)
}

func dispatch(event model.SessionEvent, sinks []Sink) {
	for _, sink := range sinks {
		if sink == nil {
			continue
		}
		sink.Append(event)
	}
}
