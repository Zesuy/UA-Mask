package events

import (
	"sync"

	"UAmask/internal/model"
)

type Sink interface {
	Append(event model.SessionEvent)
}

type Dispatcher struct {
	mu       sync.Mutex
	cond     *sync.Cond
	sinks    []Sink
	queue    []model.SessionEvent
	running  bool
	stopping bool
	wg       sync.WaitGroup
}

func NewDispatcher(sinks ...Sink) *Dispatcher {
	dispatcher := &Dispatcher{sinks: sinks}
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
	if d == nil {
		return
	}

	d.mu.Lock()
	if d.running {
		d.queue = append(d.queue, event)
		d.cond.Signal()
		d.mu.Unlock()
		return
	}
	sinks := append([]Sink(nil), d.sinks...)
	d.mu.Unlock()

	dispatch(event, sinks)
}

func (d *Dispatcher) loop() {
	defer d.wg.Done()

	for {
		d.mu.Lock()
		for len(d.queue) == 0 && !d.stopping {
			d.cond.Wait()
		}
		if len(d.queue) == 0 && d.stopping {
			d.running = false
			d.stopping = false
			d.mu.Unlock()
			return
		}

		event := d.queue[0]
		copy(d.queue, d.queue[1:])
		d.queue = d.queue[:len(d.queue)-1]
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
