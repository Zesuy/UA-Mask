package firewall

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sync"
	"time"

	"UAmask/internal/clock"

	"github.com/sirupsen/logrus"
)

var validSetName = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
var ErrExecutorStopped = errors.New("firewall executor stopped")

type Applier interface {
	ApplyBatch(items []BypassTarget) error
}

type completionFunc func(error)

type queuedTarget struct {
	target     BypassTarget
	completion completionFunc
	flush      chan struct{}
}

type pendingTarget struct {
	target      BypassTarget
	completions []completionFunc
}

type Executor struct {
	queue        chan queuedTarget
	stopChan     chan struct{}
	stopOnce     sync.Once
	waitGroup    sync.WaitGroup
	lifecycleMu  sync.RWMutex
	stopped      bool
	log          *logrus.Logger
	applier      Applier
	maxBatchSize int
	maxBatchWait time.Duration
	clock        clock.Clock
}

func NewExecutor(log *logrus.Logger, queueSize int, applier Applier) *Executor {
	return NewExecutorWithClock(log, queueSize, applier, clock.RealClock{})
}

func NewExecutorWithClock(log *logrus.Logger, queueSize int, applier Applier, runtimeClock clock.Clock) *Executor {
	return &Executor{
		queue:        make(chan queuedTarget, queueSize),
		stopChan:     make(chan struct{}),
		log:          log,
		applier:      applier,
		maxBatchSize: 200,
		maxBatchWait: 100 * time.Millisecond,
		clock:        clock.OrReal(runtimeClock),
	}
}

func (e *Executor) Start() {
	e.waitGroup.Add(1)
	go e.worker()
	e.log.Info("[FirewallExecutor] worker started")
}

func (e *Executor) Stop() {
	e.stopOnce.Do(func() {
		e.lifecycleMu.Lock()
		e.stopped = true
		close(e.stopChan)
		e.lifecycleMu.Unlock()
	})
	e.waitGroup.Wait()
	e.log.Info("[FirewallExecutor] worker stopped")
}

func (e *Executor) Enqueue(target BypassTarget) {
	_ = e.TryEnqueue(target)
}

func (e *Executor) TryEnqueue(target BypassTarget) bool {
	return e.TryEnqueueWithResult(target, nil)
}

func (e *Executor) TryEnqueueWithResult(target BypassTarget, completion func(error)) bool {
	if !isValidTarget(target) {
		e.log.Warnf("[FirewallExecutor] Invalid bypass target: %+v", target)
		return false
	}
	e.lifecycleMu.RLock()
	defer e.lifecycleMu.RUnlock()
	if e.stopped {
		return false
	}

	select {
	case e.queue <- queuedTarget{target: target, completion: completion}:
		return true
	case <-e.clock.After(50 * time.Millisecond):
		e.log.Warnf("[FirewallExecutor] queue full, dropping target for %s:%d", target.IP, target.Port)
		return false
	}
}

func (e *Executor) Flush(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan struct{})

	e.lifecycleMu.RLock()
	if e.stopped {
		e.lifecycleMu.RUnlock()
		return ErrExecutorStopped
	}
	select {
	case e.queue <- queuedTarget{flush: done}:
		e.lifecycleMu.RUnlock()
	case <-ctx.Done():
		e.lifecycleMu.RUnlock()
		return ctx.Err()
	}

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Executor) worker() {
	defer e.waitGroup.Done()

	batches := make(map[string]map[string]*pendingTarget)
	batchTimer := e.clock.NewTimer(e.maxBatchWait)
	stopAndDrainTimer(batchTimer)

	for {
		select {
		case queued := <-e.queue:
			if queued.flush != nil {
				e.executeBatches(batches)
				batches = make(map[string]map[string]*pendingTarget)
				stopAndDrainTimer(batchTimer)
				close(queued.flush)
				continue
			}
			key := e.addToBatch(batches, queued)

			if len(batches) == 1 && len(batches[key]) == 1 {
				batchTimer.Reset(e.maxBatchWait)
			}

			if len(batches[key]) >= e.maxBatchSize {
				e.executeBatches(batches)
				batches = make(map[string]map[string]*pendingTarget)
				stopAndDrainTimer(batchTimer)
			}

		case <-batchTimer.C():
			if len(batches) > 0 {
				e.executeBatches(batches)
				batches = make(map[string]map[string]*pendingTarget)
			}

		case <-e.stopChan:
			batches = e.drainQueue(batches)
			e.executeBatches(batches)
			stopAndDrainTimer(batchTimer)
			return
		}
	}
}

func (e *Executor) drainQueue(batches map[string]map[string]*pendingTarget) map[string]map[string]*pendingTarget {
	for {
		select {
		case queued := <-e.queue:
			if queued.flush != nil {
				e.executeBatches(batches)
				batches = make(map[string]map[string]*pendingTarget)
				close(queued.flush)
				continue
			}
			e.addToBatch(batches, queued)
		default:
			return batches
		}
	}
}

func stopAndDrainTimer(timer clock.Timer) {
	if timer.Stop() {
		return
	}
	select {
	case <-timer.C():
	default:
	}
}

func (e *Executor) addToBatch(batches map[string]map[string]*pendingTarget, queued queuedTarget) string {
	key := batchKey(queued.target)
	dedupKey := fmt.Sprintf("%s:%d", queued.target.IP, queued.target.Port)
	if _, ok := batches[key]; !ok {
		batches[key] = make(map[string]*pendingTarget)
	}
	pending, ok := batches[key][dedupKey]
	if !ok {
		pending = &pendingTarget{}
		batches[key][dedupKey] = pending
	}
	pending.target = queued.target
	if queued.completion != nil {
		pending.completions = append(pending.completions, queued.completion)
	}
	return key
}

func (e *Executor) executeBatches(batches map[string]map[string]*pendingTarget) {
	if len(batches) == 0 {
		return
	}

	for key, itemsMap := range batches {
		if len(itemsMap) == 0 {
			continue
		}

		items := make([]BypassTarget, 0, len(itemsMap))
		pendingItems := make([]*pendingTarget, 0, len(itemsMap))
		for _, pending := range itemsMap {
			items = append(items, pending.target)
			pendingItems = append(pendingItems, pending)
		}

		err := e.applier.ApplyBatch(items)
		if err != nil {
			first := items[0]
			e.log.Warnf("[FirewallExecutor] Failed batch %s for set %s (%s): %v", key, first.SetName, first.Backend, err)
		} else {
			first := items[0]
			e.log.Debugf("[FirewallExecutor] Added %d unique targets to set %s (%s)", len(items), first.SetName, first.Backend)
		}

		for _, pending := range pendingItems {
			for _, completion := range pending.completions {
				e.notifyCompletion(completion, err)
			}
		}
	}
}

func (e *Executor) notifyCompletion(completion completionFunc, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			e.log.Errorf("[FirewallExecutor] completion callback panicked: %v", recovered)
		}
	}()
	completion(err)
}

func batchKey(target BypassTarget) string {
	return fmt.Sprintf("%s:%s", target.Backend, target.SetName)
}

func isValidTarget(target BypassTarget) bool {
	if target.IP == "" || target.SetName == "" {
		return false
	}
	if net.ParseIP(target.IP) == nil {
		return false
	}
	return validSetName.MatchString(target.SetName)
}
