package record

import (
	"sync"

	"UAmask/internal/firewall"
	"UAmask/internal/model"
)

type Recorder[T any] struct {
	mu     sync.Mutex
	values []T
}

func (recorder *Recorder[T]) Add(value T) {
	recorder.mu.Lock()
	recorder.values = append(recorder.values, value)
	recorder.mu.Unlock()
}

func (recorder *Recorder[T]) Values() []T {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return append([]T(nil), recorder.values...)
}

func (recorder *Recorder[T]) Len() int {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return len(recorder.values)
}

type Events struct {
	Recorder[model.SessionEvent]
}

func (recorder *Events) Append(event model.SessionEvent) {
	recorder.Add(event)
}

func (recorder *Events) Count(eventType model.SessionEventType) int {
	count := 0
	for _, event := range recorder.Values() {
		if event.Type == eventType {
			count++
		}
	}
	return count
}

type Signals struct {
	Recorder[model.OffloadSignal]
}

type Snapshots struct {
	Recorder[model.ProfileSnapshot]
}

type Batches struct {
	Recorder[[]firewall.BypassTarget]
}

func (recorder *Batches) AddBatch(items []firewall.BypassTarget) {
	copied := append([]firewall.BypassTarget(nil), items...)
	recorder.Add(copied)
}
