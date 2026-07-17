package events

import (
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
	dropped  uint64
	wg       sync.WaitGroup
}

func NewDispatcher(sinks ...Sink) *Dispatcher {
	return NewDispatcherWithCapacity(defaultQueueCapacity, sinks...)
}

func NewDispatcherWithCapacity(capacity int, sinks ...Sink) *Dispatcher {
	if capacity <= 0 {
		capacity = 1
	}
	dispatcher := &Dispatcher{
		sinks: sinks,
		queue: make([]model.SessionEvent, capacity),
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

	if d.running {
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
		d.mu.Unlock()
		return
	}
	d.stopping = true
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
	if d.running {
		if d.stopping || d.size == len(d.queue) {
			d.dropped++
			d.mu.Unlock()
			return false
		}
		tail := (d.head + d.size) % len(d.queue)
		d.queue[tail] = event
		d.size++
		d.cond.Signal()
		d.mu.Unlock()
		return true
	}
	sinks := append([]Sink(nil), d.sinks...)
	d.mu.Unlock()

	dispatch(event, sinks)
	return true
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
			d.stopping = false
			d.mu.Unlock()
			return
		}

		event := d.queue[d.head]
		d.queue[d.head] = model.SessionEvent{}
		d.head = (d.head + 1) % len(d.queue)
		d.size--
		sinks := append([]Sink(nil), d.sinks...)
		d.mu.Unlock()

		dispatch(event, sinks)
	}
}

func dispatch(event model.SessionEvent, sinks []Sink) {
	for _, sink := range sinks {
		if sink == nil {
			continue
		}
		sink.Append(event)
	}
}
