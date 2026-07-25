package state

import (
	"fmt"
	"sync"
	"time"

	"UAmask/internal/clock"
	"UAmask/internal/model"
)

type EventSink interface {
	Append(event model.SessionEvent)
}

type ProfileStore interface {
	Snapshot(target model.Target) model.ProfileSnapshot
}

type Config struct {
	NonHTTPEnabled         bool
	NonHTTPThreshold       int
	HTTPCooldownPeriod     time.Duration
	DecisionDelay          time.Duration
	ProfileCleanupInterval time.Duration
}

type Store struct {
	config Config
	clock  clock.Clock

	mu       sync.RWMutex
	profiles map[string]*model.TargetProfile

	stopChan  chan struct{}
	stopOnce  sync.Once
	waitGroup sync.WaitGroup
}

func NewStore(cfg Config) *Store {
	return NewStoreWithClock(cfg, clock.RealClock{})
}

func NewStoreWithClock(cfg Config, runtimeClock clock.Clock) *Store {
	if cfg.ProfileCleanupInterval <= 0 {
		cfg.ProfileCleanupInterval = 10 * time.Minute
	}

	return &Store{
		config:   cfg,
		clock:    clock.OrReal(runtimeClock),
		profiles: make(map[string]*model.TargetProfile),
		stopChan: make(chan struct{}),
	}
}

func (s *Store) Start() {
	s.waitGroup.Add(1)
	go s.cleanupLoop()
}

func (s *Store) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopChan)
	})
	s.waitGroup.Wait()
}

func (s *Store) Append(event model.SessionEvent) {
	if event.Target.IP == "" || event.Target.Port == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := event.Timestamp
	if now.IsZero() {
		now = s.clock.Now()
	}

	switch event.Type {
	case model.EventHTTPRequestObserved:
		profile := s.ensureProfileLocked(event.Target)
		profile.LastEventType = event.Type
		profile.LastReason = event.Reason
		s.applyHTTPObservation(profile, now)
	case model.EventNonHTTPObserved:
		profile := s.ensureProfileLocked(event.Target)
		profile.LastEventType = event.Type
		profile.LastReason = event.Reason
		s.applyNonHTTPObservation(profile, now)
	case model.EventRewriteSkipped:
		profile := s.ensureProfileLocked(event.Target)
		profile.LastEventType = event.Type
		profile.LastReason = event.Reason
		profile.LastFirewallHit = event.FirewallHit
		profile.LastActivity = now
		if event.FirewallHit {
			profile.FirewallWhitelistHits++
		}
	case model.EventRewriteApplied:
		profile := s.ensureProfileLocked(event.Target)
		profile.LastEventType = event.Type
		profile.LastReason = event.Reason
		profile.LastFirewallHit = event.FirewallHit
		profile.LastActivity = now
	case model.EventTargetOffloadSucceeded:
		profile := s.ensureProfileLocked(event.Target)
		profile.LastEventType = event.Type
		profile.LastReason = event.Reason
		profile.LastActivity = now
		profile.NonHTTPScore = 0
		profile.PendingTargetOffloadAt = time.Time{}
		profile.TargetOffloadUntil = now.Add(time.Duration(event.Timeout) * time.Second)
		profile.FirewallWhitelistHits = 0
	case model.EventTargetOffloadFailed:
		profile := s.ensureProfileLocked(event.Target)
		profile.LastEventType = event.Type
		profile.LastReason = event.Reason
		profile.LastActivity = now
		profile.PendingTargetOffloadAt = time.Time{}
	case model.EventSessionOpened, model.EventUpstreamConnected, model.EventProtocolClassified,
		model.EventSessionForwardFallback, model.EventSessionClosed,
		model.EventFlowOffloadSucceeded, model.EventFlowOffloadFailed:
		return
	}
}

func (s *Store) Snapshot(target model.Target) model.ProfileSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	profile, ok := s.profiles[profileKey(target)]
	if !ok {
		return model.ProfileSnapshot{Target: target}
	}

	return model.ProfileSnapshot{
		Found:                  true,
		Target:                 profile.Target,
		NonHTTPScore:           profile.NonHTTPScore,
		HTTPCooldownUntil:      profile.HTTPCooldownUntil,
		LastActivity:           profile.LastActivity,
		PendingTargetOffloadAt: profile.PendingTargetOffloadAt,
		TargetOffloadUntil:     profile.TargetOffloadUntil,
		FirewallWhitelistHits:  profile.FirewallWhitelistHits,
		LastFirewallHit:        profile.LastFirewallHit,
		LastEventType:          profile.LastEventType,
		LastReason:             profile.LastReason,
	}
}

func (s *Store) cleanupLoop() {
	defer s.waitGroup.Done()

	ticker := s.clock.NewTicker(s.config.ProfileCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C():
			s.cleanup()
		case <-s.stopChan:
			return
		}
	}
}

func (s *Store) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock.Now()
	for key, profile := range s.profiles {
		if !profile.TargetOffloadUntil.IsZero() && now.Before(profile.TargetOffloadUntil) {
			continue
		}
		if !profile.PendingTargetOffloadAt.IsZero() && now.Before(profile.PendingTargetOffloadAt) {
			continue
		}
		if !profile.HTTPCooldownUntil.IsZero() && now.Before(profile.HTTPCooldownUntil) {
			continue
		}
		if !profile.LastActivity.IsZero() && now.Sub(profile.LastActivity) <= s.config.ProfileCleanupInterval {
			continue
		}
		delete(s.profiles, key)
	}
}

func (s *Store) applyHTTPObservation(profile *model.TargetProfile, now time.Time) {
	if now.Before(profile.HTTPCooldownUntil) {
		return
	}

	profile.NonHTTPScore = 0
	profile.HTTPCooldownUntil = now.Add(s.config.HTTPCooldownPeriod)
	profile.PendingTargetOffloadAt = time.Time{}
	profile.LastActivity = now
}

func (s *Store) applyNonHTTPObservation(profile *model.TargetProfile, now time.Time) {
	if !s.config.NonHTTPEnabled {
		return
	}
	if now.Before(profile.HTTPCooldownUntil) {
		return
	}
	if !profile.TargetOffloadUntil.IsZero() && now.Before(profile.TargetOffloadUntil) {
		return
	}

	profile.NonHTTPScore++
	profile.LastActivity = now
	if profile.NonHTTPScore >= s.config.NonHTTPThreshold && profile.PendingTargetOffloadAt.IsZero() {
		profile.PendingTargetOffloadAt = now.Add(s.config.DecisionDelay)
	}
}

func (s *Store) ensureProfileLocked(target model.Target) *model.TargetProfile {
	key := profileKey(target)
	if profile, ok := s.profiles[key]; ok {
		return profile
	}

	profile := &model.TargetProfile{Target: target}
	s.profiles[key] = profile
	return profile
}

func profileKey(target model.Target) string {
	return fmt.Sprintf("%s:%d", target.IP, target.Port)
}
