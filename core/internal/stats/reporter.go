package stats

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type FileReporter struct {
	stats     *Stats
	filePath  string
	interval  time.Duration
	stopChan  chan struct{}
	stopOnce  sync.Once
	waitGroup sync.WaitGroup
}

func NewFileReporter(stats *Stats, filePath string, interval time.Duration) *FileReporter {
	return &FileReporter{
		stats:    stats,
		filePath: filePath,
		interval: interval,
		stopChan: make(chan struct{}),
	}
}

func (r *FileReporter) Start() {
	if r.filePath == "" || r.interval <= 0 {
		return
	}

	r.waitGroup.Add(1)
	go r.loop()
}

func (r *FileReporter) Stop() {
	r.stopOnce.Do(func() {
		close(r.stopChan)
	})
	r.waitGroup.Wait()
}

func (r *FileReporter) loop() {
	defer r.waitGroup.Done()

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	var lastRequests uint64
	lastCheckTime := time.Now()

	for {
		select {
		case <-ticker.C:
			snapshot := r.stats.Snapshot()
			content := renderSnapshot(snapshot, &lastRequests, &lastCheckTime)
			if err := os.WriteFile(r.filePath, []byte(content), 0644); err != nil {
				logrus.Warnf("Failed to write stats file: %v", err)
			}
		case <-r.stopChan:
			return
		}
	}
}

func renderSnapshot(snapshot Snapshot, lastRequests *uint64, lastCheckTime *time.Time) string {
	now := time.Now()
	intervalSeconds := now.Sub(*lastCheckTime).Seconds()

	var rps float64
	if intervalSeconds > 0 {
		requestsSinceLast := snapshot.HTTPRequests - *lastRequests
		rps = float64(requestsSinceLast) / intervalSeconds
	}

	*lastRequests = snapshot.HTTPRequests
	*lastCheckTime = now

	totalCacheHits := snapshot.CacheHits + snapshot.CacheHitNoModify

	var ruleProcessing uint64
	if snapshot.HTTPRequests > totalCacheHits {
		ruleProcessing = snapshot.HTTPRequests - totalCacheHits
	}

	var directPass uint64
	if snapshot.HTTPRequests > snapshot.ModifiedRequests {
		directPass = snapshot.HTTPRequests - snapshot.ModifiedRequests
	}

	var totalCacheRatio float64
	if snapshot.HTTPRequests > 0 {
		totalCacheRatio = (float64(totalCacheHits) * 100) / float64(snapshot.HTTPRequests)
	}

	return fmt.Sprintf(
		"current_connections:%d\n"+
			"total_requests:%d\n"+
			"rps:%.2f\n"+
			"successful_modifications:%d\n"+
			"direct_passthrough:%d\n"+
			"rule_processing:%d\n"+
			"cache_hit_modify:%d\n"+
			"cache_hit_pass:%d\n"+
			"total_cache_ratio:%.2f\n",
		snapshot.ActiveConnections,
		snapshot.HTTPRequests,
		rps,
		snapshot.ModifiedRequests,
		directPass,
		ruleProcessing,
		snapshot.CacheHits,
		snapshot.CacheHitNoModify,
		totalCacheRatio,
	)
}
