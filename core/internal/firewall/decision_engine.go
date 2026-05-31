package firewall

import (
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type DecisionConfig struct {
	NonHTTPThreshold       int
	HTTPCooldownPeriod     time.Duration
	DecisionDelay          time.Duration
	ProfileCleanupInterval time.Duration
	TargetTemplate         BypassTarget
}

type reportEvent struct {
	ip   string
	port int
}

type portProfile struct {
	nonHTTPScore    int
	httpLockExpires time.Time
	lastEvent       time.Time
	decisionTimer   *time.Timer
}

type DecisionEngine struct {
	log    *logrus.Logger
	sink   TargetSink
	config DecisionConfig

	httpEventChan    chan reportEvent
	nonHTTPEventChan chan reportEvent
	stopChan         chan struct{}
	stopOnce         sync.Once
	waitGroup        sync.WaitGroup

	profiles    map[string]*portProfile
	profileLock sync.Mutex
}

func NewDecisionEngine(log *logrus.Logger, queueSize int, cfg DecisionConfig, sink TargetSink) *DecisionEngine {
	if cfg.ProfileCleanupInterval <= 0 {
		cfg.ProfileCleanupInterval = 10 * time.Minute
	}

	return &DecisionEngine{
		log:              log,
		sink:             sink,
		config:           cfg,
		httpEventChan:    make(chan reportEvent, queueSize),
		nonHTTPEventChan: make(chan reportEvent, queueSize),
		stopChan:         make(chan struct{}),
		profiles:         make(map[string]*portProfile),
	}
}

func (e *DecisionEngine) Start() {
	e.waitGroup.Add(1)
	go e.worker()
	e.log.Info("[FirewallDecision] worker started")
}

func (e *DecisionEngine) Stop() {
	e.stopOnce.Do(func() {
		close(e.stopChan)
	})
	e.waitGroup.Wait()
	e.log.Info("[FirewallDecision] worker stopped")
}

func (e *DecisionEngine) ReportHTTP(ip string, port int) {
	select {
	case e.httpEventChan <- reportEvent{ip: ip, port: port}:
	default:
		e.log.Warnf("[FirewallDecision] HTTP event channel full, dropping event for %s:%d", ip, port)
	}
}

func (e *DecisionEngine) ReportNonHTTP(ip string, port int) {
	select {
	case e.nonHTTPEventChan <- reportEvent{ip: ip, port: port}:
	default:
		e.log.Warnf("[FirewallDecision] Non-HTTP event channel full, dropping event for %s:%d", ip, port)
	}
}

func (e *DecisionEngine) worker() {
	defer e.waitGroup.Done()

	cleanupTicker := time.NewTicker(e.config.ProfileCleanupInterval)
	defer cleanupTicker.Stop()

	for {
		select {
		case event := <-e.httpEventChan:
			e.handleHTTPEvent(event.ip, event.port)
		case event := <-e.nonHTTPEventChan:
			e.handleNonHTTPEvent(event.ip, event.port)
		case <-cleanupTicker.C:
			e.cleanupProfiles()
		case <-e.stopChan:
			e.stopAllTimers()
			return
		}
	}
}

func (e *DecisionEngine) stopAllTimers() {
	e.profileLock.Lock()
	defer e.profileLock.Unlock()

	for _, profile := range e.profiles {
		if profile.decisionTimer != nil {
			profile.decisionTimer.Stop()
		}
	}
}

func (e *DecisionEngine) handleHTTPEvent(ip string, port int) {
	e.profileLock.Lock()
	defer e.profileLock.Unlock()

	key := profileKey(ip, port)
	profile := e.ensureProfile(key)

	if time.Now().Before(profile.httpLockExpires) {
		return
	}

	profile.nonHTTPScore = 0
	profile.httpLockExpires = time.Now().Add(e.config.HTTPCooldownPeriod)
	profile.lastEvent = time.Now()

	if profile.decisionTimer != nil {
		profile.decisionTimer.Stop()
		profile.decisionTimer = nil
		e.log.Infof("[FirewallDecision] Cancelled bypass for %s due to HTTP activity.", key)
	}
}

func (e *DecisionEngine) handleNonHTTPEvent(ip string, port int) {
	e.profileLock.Lock()
	defer e.profileLock.Unlock()

	key := profileKey(ip, port)
	profile := e.ensureProfile(key)

	if time.Now().Before(profile.httpLockExpires) {
		e.log.Debugf("[FirewallDecision] Ignored non-HTTP event for %s during cooldown.", key)
		return
	}

	profile.nonHTTPScore++
	profile.lastEvent = time.Now()

	if profile.nonHTTPScore >= e.config.NonHTTPThreshold && profile.decisionTimer == nil {
		e.log.Infof("[FirewallDecision] Threshold reached for %s, waiting %s", key, e.config.DecisionDelay)
		profile.decisionTimer = time.AfterFunc(e.config.DecisionDelay, func() {
			e.finalizeDecision(ip, port)
		})
	}
}

func (e *DecisionEngine) finalizeDecision(ip string, port int) {
	e.profileLock.Lock()
	defer e.profileLock.Unlock()

	key := profileKey(ip, port)
	profile, ok := e.profiles[key]
	if !ok || profile.nonHTTPScore < e.config.NonHTTPThreshold || time.Now().Before(profile.httpLockExpires) {
		if ok {
			profile.decisionTimer = nil
		}
		return
	}

	target := e.config.TargetTemplate
	target.IP = ip
	target.Port = port
	if e.sink != nil {
		e.sink.Enqueue(target)
	}
	delete(e.profiles, key)
}

func (e *DecisionEngine) cleanupProfiles() {
	e.profileLock.Lock()
	defer e.profileLock.Unlock()

	now := time.Now()
	for key, profile := range e.profiles {
		if profile.decisionTimer != nil {
			continue
		}
		if !profile.httpLockExpires.IsZero() && now.Before(profile.httpLockExpires) {
			continue
		}
		if now.Sub(profile.lastEvent) > e.config.ProfileCleanupInterval {
			delete(e.profiles, key)
		}
	}
}

func (e *DecisionEngine) ensureProfile(key string) *portProfile {
	profile, ok := e.profiles[key]
	if !ok {
		profile = &portProfile{}
		e.profiles[key] = profile
	}
	return profile
}

func profileKey(ip string, port int) string {
	return fmt.Sprintf("%s:%d", ip, port)
}
