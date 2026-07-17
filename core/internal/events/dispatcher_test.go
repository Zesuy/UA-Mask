package events

import (
	"sync"
	"testing"

	"UAmask/internal/model"
)

type collectingSink struct {
	mu     sync.Mutex
	events []model.SessionEventType
}

func (s *collectingSink) Append(event model.SessionEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event.Type)
}

func (s *collectingSink) Types() []model.SessionEventType {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]model.SessionEventType(nil), s.events...)
}

type emittingSink struct {
	dispatcher *Dispatcher
}

func (s *emittingSink) Append(event model.SessionEvent) {
	if event.Type != model.EventSessionOpened {
		return
	}
	s.dispatcher.Append(model.SessionEvent{Type: model.EventSessionClosed})
}

func TestDispatcherDrainsAsyncEventsInOrder(t *testing.T) {
	t.Parallel()

	dispatcher := NewDispatcher()
	collector := &collectingSink{}
	dispatcher.Add(&emittingSink{dispatcher: dispatcher}, collector)

	dispatcher.Start()
	dispatcher.Append(model.SessionEvent{Type: model.EventSessionOpened})
	dispatcher.Stop()

	got := collector.Types()
	want := []model.SessionEventType{
		model.EventSessionOpened,
		model.EventSessionClosed,
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d events, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d: expected %s, got %s", i, want[i], got[i])
		}
	}
}
