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

type Executor struct {
	queue        chan BypassTarget
	stopChan     chan struct{}
	stopOnce     sync.Once
	waitGroup    sync.WaitGroup
	log          *logrus.Logger
	applier      Applier
	maxBatchSize int
	maxBatchWait time.Duration
}

func NewExecutor(log *logrus.Logger, queueSize int, applier Applier) *Executor {
	return &Executor{
		queue:        make(chan BypassTarget, queueSize),
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
		close(e.stopChan)
	})
	e.waitGroup.Wait()
	e.log.Info("[FirewallExecutor] worker stopped")
}

func (e *Executor) Enqueue(target BypassTarget) {
	if !isValidTarget(target) {
		e.log.Warnf("[FirewallExecutor] Invalid bypass target: %+v", target)
		return
	}

	select {
	case e.queue <- target:
	case <-time.After(50 * time.Millisecond):
		e.log.Warnf("[FirewallExecutor] queue full, dropping target for %s:%d", target.IP, target.Port)
	}
}

func (e *Executor) worker() {
	defer e.waitGroup.Done()

	batches := make(map[string]map[string]BypassTarget)
	batchTimer := time.NewTimer(e.maxBatchWait)
	if !batchTimer.Stop() {
		<-batchTimer.C
	}

	for {
		select {
		case target := <-e.queue:
			key := batchKey(target)
			dedupKey := fmt.Sprintf("%s:%d", target.IP, target.Port)
			if _, ok := batches[key]; !ok {
				batches[key] = make(map[string]BypassTarget)
			}
			batches[key][dedupKey] = target

			if len(batches) == 1 && len(batches[key]) == 1 {
				batchTimer.Reset(e.maxBatchWait)
			}

			if len(batches[key]) >= e.maxBatchSize {
				e.executeBatches(batches)
				batches = make(map[string]map[string]BypassTarget)
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
				batches = make(map[string]map[string]BypassTarget)
			}

		case <-e.stopChan:
			e.drainQueue(batches)
			e.executeBatches(batches)
			return
		}
	}
}

func (e *Executor) drainQueue(batches map[string]map[string]BypassTarget) {
	for {
		select {
		case target := <-e.queue:
			key := batchKey(target)
			dedupKey := fmt.Sprintf("%s:%d", target.IP, target.Port)
			if _, ok := batches[key]; !ok {
				batches[key] = make(map[string]BypassTarget)
			}
			batches[key][dedupKey] = target
		default:
			return
		}
	}
}

func (e *Executor) executeBatches(batches map[string]map[string]BypassTarget) {
	if len(batches) == 0 {
		return
	}

	for key, itemsMap := range batches {
		if len(itemsMap) == 0 {
			continue
		}

		items := make([]BypassTarget, 0, len(itemsMap))
		for _, item := range itemsMap {
			items = append(items, item)
		}

		if err := e.applier.ApplyBatch(items); err != nil {
			first := items[0]
			e.log.Warnf("[FirewallExecutor] Failed batch %s for set %s (%s): %v", key, first.SetName, first.Backend, err)
			continue
		}

		first := items[0]
		e.log.Debugf("[FirewallExecutor] Added %d unique targets to set %s (%s)", len(items), first.SetName, first.Backend)
	}
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
