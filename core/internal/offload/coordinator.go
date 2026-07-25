package offload

import (
	"fmt"
	"sync"

	"UAmask/internal/clock"
	"UAmask/internal/events"
	"UAmask/internal/model"
	"UAmask/internal/policy"
	"UAmask/internal/state"

	"github.com/sirupsen/logrus"
)

type FlowOffloader interface {
	Handle(signal model.OffloadSignal) error
}

type TargetOffloader interface {
	Handle(signal model.OffloadSignal, completion func(error)) error
}

type targetTimer struct {
	timer clock.Timer
}

type Coordinator struct {
	log             *logrus.Logger
	profiles        state.ProfileStore
	targetEvaluator policy.TargetEvaluator
	targetOffloader TargetOffloader
	flowOffloader   FlowOffloader
	events          *events.Dispatcher
	clock           clock.Clock

	mu           sync.Mutex
	targetTimers map[string]*targetTimer
	inFlight     map[string]struct{}
	stopped      bool
	timerWG      sync.WaitGroup
	activeWG     sync.WaitGroup
}

func NewCoordinator(
	log *logrus.Logger,
	profiles state.ProfileStore,
	targetEvaluator policy.TargetEvaluator,
	targetOffloader TargetOffloader,
	flowOffloader FlowOffloader,
	dispatcher *events.Dispatcher,
) *Coordinator {
	return NewCoordinatorWithClock(log, profiles, targetEvaluator, targetOffloader, flowOffloader, dispatcher, clock.RealClock{})
}

func NewCoordinatorWithClock(
	log *logrus.Logger,
	profiles state.ProfileStore,
	targetEvaluator policy.TargetEvaluator,
	targetOffloader TargetOffloader,
	flowOffloader FlowOffloader,
	dispatcher *events.Dispatcher,
	runtimeClock clock.Clock,
) *Coordinator {
	return &Coordinator{
		log:             log,
		profiles:        profiles,
		targetEvaluator: targetEvaluator,
		targetOffloader: targetOffloader,
		flowOffloader:   flowOffloader,
		events:          dispatcher,
		clock:           clock.OrReal(runtimeClock),
		targetTimers:    make(map[string]*targetTimer),
		inFlight:        make(map[string]struct{}),
	}
}

func (c *Coordinator) Append(event model.SessionEvent) {
	switch event.Type {
	case model.EventNonHTTPObserved, model.EventHTTPRequestObserved, model.EventRewriteSkipped:
		if c.isStopped() {
			return
		}
		c.observeTargetEvent(event)
	case model.EventTargetOffloadSucceeded, model.EventTargetOffloadFailed:
		c.clearTargetInFlight(event.Target)
	case model.EventSessionClosed, model.EventRewriteApplied,
		model.EventSessionOpened, model.EventUpstreamConnected, model.EventProtocolClassified,
		model.EventSessionForwardFallback, model.EventFlowOffloadSucceeded,
		model.EventFlowOffloadFailed:
		return
	}
}

func (c *Coordinator) Handle(signal model.OffloadSignal) {
	if !c.beginOperation() {
		return
	}
	defer c.activeWG.Done()
	switch signal.Kind {
	case model.SignalTryFlowOffload:
		c.handleFlowSignal(signal)
	case model.SignalTryTargetOffload:
		c.handleTargetSignal(signal)
	case model.SignalCancelTargetOffload, model.SignalNoAction:
		return
	}
}

func (c *Coordinator) Stop() {
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		c.timerWG.Wait()
		c.activeWG.Wait()
		return
	}
	c.stopped = true
	for key, scheduled := range c.targetTimers {
		scheduled.timer.Stop()
		delete(c.targetTimers, key)
	}
	c.mu.Unlock()
	c.timerWG.Wait()
	c.activeWG.Wait()
}

func (c *Coordinator) observeTargetEvent(event model.SessionEvent) {
	snapshot := c.profiles.Snapshot(event.Target)
	if !snapshot.Found {
		return
	}

	switch event.Type {
	case model.EventHTTPRequestObserved:
		c.cancelTargetTimer(event.Target)
	case model.EventNonHTTPObserved:
		c.scheduleTargetEvaluation(snapshot)
	case model.EventRewriteSkipped:
		signal := c.targetEvaluator.Evaluate(snapshot)
		c.Handle(signal)
	}
}

func (c *Coordinator) scheduleTargetEvaluation(snapshot model.ProfileSnapshot) {
	if snapshot.PendingTargetOffloadAt.IsZero() {
		return
	}

	delay := snapshot.PendingTargetOffloadAt.Sub(c.clock.Now())
	if delay < 0 {
		delay = 0
	}

	key := targetKey(snapshot.Target)

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return
	}

	if scheduled, ok := c.targetTimers[key]; ok {
		scheduled.timer.Stop()
	}

	scheduled := &targetTimer{}
	scheduled.timer = c.clock.AfterFunc(delay, func() {
		c.mu.Lock()
		current, ok := c.targetTimers[key]
		if !ok || current != scheduled || c.stopped {
			c.mu.Unlock()
			return
		}
		delete(c.targetTimers, key)
		c.timerWG.Add(1)
		c.mu.Unlock()
		defer c.timerWG.Done()

		latest := c.profiles.Snapshot(snapshot.Target)
		signal := c.targetEvaluator.Evaluate(latest)
		c.Handle(signal)
	})
	c.targetTimers[key] = scheduled
}

func (c *Coordinator) cancelTargetTimer(target model.Target) {
	key := targetKey(target)

	c.mu.Lock()
	defer c.mu.Unlock()

	if scheduled, ok := c.targetTimers[key]; ok {
		scheduled.timer.Stop()
		delete(c.targetTimers, key)
	}
}

func (c *Coordinator) handleFlowSignal(signal model.OffloadSignal) {
	if signal.Kind == model.SignalNoAction || c.flowOffloader == nil {
		return
	}

	if err := c.flowOffloader.Handle(signal); err != nil {
		c.log.Debugf("[Offload] flow signal failed for %s: %v", signal.Target.Address, err)
		c.emit(model.SessionEvent{
			Type:      model.EventFlowOffloadFailed,
			SessionID: signal.SessionID,
			Target:    signal.Target,
			Reason:    err.Error(),
			Timestamp: c.clock.Now(),
		})
		return
	}

	c.emit(model.SessionEvent{
		Type:      model.EventFlowOffloadSucceeded,
		SessionID: signal.SessionID,
		Target:    signal.Target,
		Timestamp: c.clock.Now(),
	})
}

func (c *Coordinator) handleTargetSignal(signal model.OffloadSignal) {
	if signal.Kind == model.SignalNoAction || c.targetOffloader == nil {
		return
	}
	if !c.markTargetInFlight(signal.Target) {
		return
	}

	var once sync.Once
	complete := func(err error) {
		once.Do(func() {
			c.completeTargetSignal(signal, err)
		})
	}
	if err := c.targetOffloader.Handle(signal, complete); err != nil {
		complete(err)
	}
}

func (c *Coordinator) completeTargetSignal(signal model.OffloadSignal, err error) {
	event := model.SessionEvent{
		Type:      model.EventTargetOffloadSucceeded,
		SessionID: signal.SessionID,
		Target:    signal.Target,
		Reason:    signal.Reason,
		Timeout:   signal.Timeout,
		Timestamp: c.clock.Now(),
	}
	if err != nil {
		c.log.Debugf("[Offload] target signal failed for %s: %v", signal.Target.Address, err)
		event.Type = model.EventTargetOffloadFailed
		event.Reason = err.Error()
	}
	if !c.emit(event) {
		c.clearTargetInFlight(signal.Target)
	}
}

func (c *Coordinator) markTargetInFlight(target model.Target) bool {
	key := targetKey(target)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return false
	}
	if _, exists := c.inFlight[key]; exists {
		return false
	}
	c.inFlight[key] = struct{}{}
	return true
}

func (c *Coordinator) isStopped() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stopped
}

func (c *Coordinator) beginOperation() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return false
	}
	c.activeWG.Add(1)
	return true
}

func (c *Coordinator) clearTargetInFlight(target model.Target) {
	c.mu.Lock()
	delete(c.inFlight, targetKey(target))
	c.mu.Unlock()
}

func (c *Coordinator) emit(event model.SessionEvent) bool {
	if c.events == nil {
		return false
	}
	if !c.events.TryAppend(event) {
		c.log.Warnf("[Offload] event queue full, dropping %s for %s", event.Type, event.Target.Address)
		return false
	}
	return true
}

func targetKey(target model.Target) string {
	return fmt.Sprintf("%s:%d", target.IP, target.Port)
}
