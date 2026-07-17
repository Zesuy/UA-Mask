package firewall

import (
	"fmt"
	"net"
	"regexp"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

var validSetName = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

type Applier interface {
	ApplyBatch(items []BypassTarget) error
}

type completionFunc func(error)

type queuedTarget struct {
	target     BypassTarget
	completion completionFunc
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
}

func NewExecutor(log *logrus.Logger, queueSize int, applier Applier) *Executor {
	return &Executor{
		queue:        make(chan queuedTarget, queueSize),
		stopChan:     make(chan struct{}),
		log:          log,
		applier:      applier,
		maxBatchSize: 200,
		maxBatchWait: 100 * time.Millisecond,
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
	case <-time.After(50 * time.Millisecond):
		e.log.Warnf("[FirewallExecutor] queue full, dropping target for %s:%d", target.IP, target.Port)
		return false
	}
}

func (e *Executor) worker() {
	defer e.waitGroup.Done()

	batches := make(map[string]map[string]*pendingTarget)
	batchTimer := time.NewTimer(e.maxBatchWait)
	if !batchTimer.Stop() {
		<-batchTimer.C
	}

	for {
		select {
		case queued := <-e.queue:
			key := e.addToBatch(batches, queued)

			if len(batches) == 1 && len(batches[key]) == 1 {
				batchTimer.Reset(e.maxBatchWait)
			}

			if len(batches[key]) >= e.maxBatchSize {
				e.executeBatches(batches)
				batches = make(map[string]map[string]*pendingTarget)
				if !batchTimer.Stop() {
					select {
					case <-batchTimer.C:
					default:
					}
				}
			}

		case <-batchTimer.C:
			if len(batches) > 0 {
				e.executeBatches(batches)
				batches = make(map[string]map[string]*pendingTarget)
			}

		case <-e.stopChan:
			e.drainQueue(batches)
			e.executeBatches(batches)
			return
		}
	}
}

func (e *Executor) drainQueue(batches map[string]map[string]*pendingTarget) {
	for {
		select {
		case queued := <-e.queue:
			e.addToBatch(batches, queued)
		default:
			return
		}
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
