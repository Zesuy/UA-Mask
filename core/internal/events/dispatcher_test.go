package events

import (
	"sync"
	"testing"
	"time"

	"UAmask/internal/model"
)

type collectingSink struct {
	mu     sync.Mutex
	events []model.SessionEventType
}

type blockingSink struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingSink) Append(model.SessionEvent) {
	s.once.Do(func() { close(s.started) })
	<-s.release
}

func (s *collectingSink) Append(event model.SessionEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event.Type)
}

func TestDispatcherDropsNewestEventWhenQueueIsFull(t *testing.T) {
	t.Parallel()

	blocker := &blockingSink{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	dispatcher := NewDispatcherWithCapacity(1, blocker)
	dispatcher.Start()

	if !dispatcher.TryAppend(model.SessionEvent{Type: model.EventSessionOpened}) {
		t.Fatal("expected first event to be accepted")
	}
	select {
	case <-blocker.started:
	case <-time.After(time.Second):
		t.Fatal("dispatcher did not start processing the first event")
	}

	if !dispatcher.TryAppend(model.SessionEvent{Type: model.EventHTTPRequestObserved}) {
		t.Fatal("expected queued event to be accepted")
	}
	if dispatcher.TryAppend(model.SessionEvent{Type: model.EventSessionClosed}) {
		t.Fatal("expected newest event to be dropped when queue is full")
	}
	if dispatcher.Dropped() != 1 {
		t.Fatalf("expected one dropped event, got %d", dispatcher.Dropped())
	}

	close(blocker.release)
	dispatcher.Stop()
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

func TestDispatcherStopsAtQueuedEventBoundary(t *testing.T) {
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
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d events, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d: expected %s, got %s", i, want[i], got[i])
		}
	}
	if dispatcher.Dropped() != 1 {
		t.Fatalf("expected event emitted after stop boundary to be dropped, got %d", dispatcher.Dropped())
	}
}
