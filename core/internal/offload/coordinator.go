package offload

import (
	"fmt"
	"sync"
	"time"

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
	Handle(signal model.OffloadSignal) error
}

type Coordinator struct {
	log             *logrus.Logger
	profiles        state.ProfileStore
	targetEvaluator policy.TargetEvaluator
	targetOffloader TargetOffloader
	flowOffloader   FlowOffloader
	events          *events.Dispatcher

	mu           sync.Mutex
	targetTimers map[string]*time.Timer
}

func NewCoordinator(
	log *logrus.Logger,
	profiles state.ProfileStore,
	targetEvaluator policy.TargetEvaluator,
	targetOffloader TargetOffloader,
	flowOffloader FlowOffloader,
	dispatcher *events.Dispatcher,
) *Coordinator {
	return &Coordinator{
		log:             log,
		profiles:        profiles,
		targetEvaluator: targetEvaluator,
		targetOffloader: targetOffloader,
		flowOffloader:   flowOffloader,
		events:          dispatcher,
		targetTimers:    make(map[string]*time.Timer),
	}
}

func (c *Coordinator) Append(event model.SessionEvent) {
	switch event.Type {
	case model.EventNonHTTPObserved, model.EventHTTPRequestObserved, model.EventRewriteSkipped:
		c.observeTargetEvent(event)
	case model.EventSessionClosed, model.EventRewriteApplied,
		model.EventSessionOpened, model.EventUpstreamConnected, model.EventProtocolClassified,
		model.EventSessionForwardFallback, model.EventFlowOffloadSucceeded,
		model.EventFlowOffloadFailed, model.EventTargetOffloadSucceeded,
		model.EventTargetOffloadFailed:
		return
	}
}

func (c *Coordinator) Handle(signal model.OffloadSignal) {
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
	defer c.mu.Unlock()

	for key, timer := range c.targetTimers {
		timer.Stop()
		delete(c.targetTimers, key)
	}
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

	delay := time.Until(snapshot.PendingTargetOffloadAt)
	if delay < 0 {
		delay = 0
	}

	key := targetKey(snapshot.Target)

	c.mu.Lock()
	defer c.mu.Unlock()

	if timer, ok := c.targetTimers[key]; ok {
		timer.Stop()
	}

	c.targetTimers[key] = time.AfterFunc(delay, func() {
		latest := c.profiles.Snapshot(snapshot.Target)
		signal := c.targetEvaluator.Evaluate(latest)
		c.Handle(signal)

		c.mu.Lock()
		delete(c.targetTimers, key)
		c.mu.Unlock()
	})
}

func (c *Coordinator) cancelTargetTimer(target model.Target) {
	key := targetKey(target)

	c.mu.Lock()
	defer c.mu.Unlock()

	if timer, ok := c.targetTimers[key]; ok {
		timer.Stop()
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
			Timestamp: time.Now(),
		})
		return
	}

	c.emit(model.SessionEvent{
		Type:      model.EventFlowOffloadSucceeded,
		SessionID: signal.SessionID,
		Target:    signal.Target,
		Timestamp: time.Now(),
	})
}

func (c *Coordinator) handleTargetSignal(signal model.OffloadSignal) {
	if signal.Kind == model.SignalNoAction || c.targetOffloader == nil {
		return
	}

	if err := c.targetOffloader.Handle(signal); err != nil {
		c.log.Debugf("[Offload] target signal failed for %s: %v", signal.Target.Address, err)
		c.emit(model.SessionEvent{
			Type:      model.EventTargetOffloadFailed,
			SessionID: signal.SessionID,
			Target:    signal.Target,
			Reason:    err.Error(),
			Timeout:   signal.Timeout,
			Timestamp: time.Now(),
		})
		return
	}

	c.emit(model.SessionEvent{
		Type:      model.EventTargetOffloadSucceeded,
		SessionID: signal.SessionID,
		Target:    signal.Target,
		Reason:    signal.Reason,
		Timeout:   signal.Timeout,
		Timestamp: time.Now(),
	})
}

func (c *Coordinator) emit(event model.SessionEvent) {
	if c.events == nil {
		return
	}
	c.events.Append(event)
}

func targetKey(target model.Target) string {
	return fmt.Sprintf("%s:%d", target.IP, target.Port)
}
