package stats

import (
	"sync/atomic"

	"UAmask/internal/model"
)

type Snapshot struct {
	ActiveConnections uint64
	HTTPRequests      uint64
	ModifiedRequests  uint64
	CacheHits         uint64
	CacheHitNoModify  uint64
}

type Stats struct {
	activeConnections atomic.Uint64
	httpRequests      atomic.Uint64
	modifiedRequests  atomic.Uint64
	cacheHits         atomic.Uint64
	cacheHitNoModify  atomic.Uint64
}

func New() *Stats {
	return &Stats{}
}

func (s *Stats) ConnectionOpened() {
	s.activeConnections.Add(1)
}

func (s *Stats) ConnectionClosed() {
	s.activeConnections.Add(^uint64(0))
}

func (s *Stats) RecordHTTPRequest() {
	s.httpRequests.Add(1)
}

func (s *Stats) RecordModifiedRequest() {
	s.modifiedRequests.Add(1)
}

func (s *Stats) RecordCacheHitModified() {
	s.cacheHits.Add(1)
}

func (s *Stats) RecordCacheHitPassthrough() {
	s.cacheHitNoModify.Add(1)
}

func (s *Stats) Snapshot() Snapshot {
	return Snapshot{
		ActiveConnections: s.activeConnections.Load(),
		HTTPRequests:      s.httpRequests.Load(),
		ModifiedRequests:  s.modifiedRequests.Load(),
		CacheHits:         s.cacheHits.Load(),
		CacheHitNoModify:  s.cacheHitNoModify.Load(),
	}
}

func (s *Stats) Append(event model.SessionEvent) {
	switch event.Type {
	case model.EventSessionOpened:
		s.ConnectionOpened()
	case model.EventSessionClosed:
		s.ConnectionClosed()
	case model.EventHTTPRequestObserved:
		s.RecordHTTPRequest()
	case model.EventRewriteApplied:
		s.RecordModifiedRequest()
		if event.FromCache {
			s.RecordCacheHitModified()
		}
	case model.EventRewriteSkipped:
		if event.FromCache {
			s.RecordCacheHitPassthrough()
		}
	}
}
