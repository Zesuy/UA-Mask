package events

import "UAmask/internal/model"

type Sink interface {
	Append(event model.SessionEvent)
}

type Dispatcher struct {
	sinks []Sink
}

func NewDispatcher(sinks ...Sink) *Dispatcher {
	return &Dispatcher{sinks: sinks}
}

func (d *Dispatcher) Add(sinks ...Sink) {
	if d == nil {
		return
	}
	d.sinks = append(d.sinks, sinks...)
}

func (d *Dispatcher) Append(event model.SessionEvent) {
	if d == nil {
		return
	}
	for _, sink := range d.sinks {
		if sink == nil {
			continue
		}
		sink.Append(event)
	}
}
