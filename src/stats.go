package main

import (
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// Stats 封装了所有统计计数器
type Stats struct {
	ActiveConnections atomic.Uint64 // 当前活跃连接数
	HttpRequests      atomic.Uint64 // 已处理 HTTP 请求总数
	ModifiedRequests  atomic.Uint64 // 成功篡改总数
	CacheHits         atomic.Uint64 // 缓存命中(修改)
	CacheHitNoModify  atomic.Uint64 // 缓存命中(放行)
}

// NewStats 创建一个新的 Stats 实例
func NewStats() *Stats {
	return &Stats{}
}

func (s *Stats) AddActiveConnections(val uint64) {
	s.ActiveConnections.Add(val)
}

func (s *Stats) IncHttpRequests() {
	s.HttpRequests.Add(1)
}

func (s *Stats) IncModifiedRequests() {
	s.ModifiedRequests.Add(1)
}

func (s *Stats) IncCacheHits() {
	s.CacheHits.Add(1)
}

func (s *Stats) IncCacheHitNoModify() {
	s.CacheHitNoModify.Add(1)
}

func (s *Stats) StartWriter(filePath string, interval time.Duration) {
	ticker := time.NewTicker(interval)

	var lastHttpRequests uint64
	lastCheckTime := time.Now()

	go func() {
		defer ticker.Stop()
		for range ticker.C {
			activeConn := s.ActiveConnections.Load()
			httpRequests := s.HttpRequests.Load()
			modified := s.ModifiedRequests.Load()
			cacheHitModify := s.CacheHits.Load()
			cacheHitPass := s.CacheHitNoModify.Load()

			// --- 2. 计算派生指标 ---

			// 处理速率 (RPS)
			now := time.Now()
			intervalSeconds := now.Sub(lastCheckTime).Seconds()
			var rps float64
			if intervalSeconds > 0 {
				requestsSinceLast := httpRequests - lastHttpRequests
				rps = float64(requestsSinceLast) / intervalSeconds
			}
			// 更新下次计算所需的状态
			lastHttpRequests = httpRequests
			lastCheckTime = now

			// 总缓存命中 = 缓存命中(修改) + 缓存命中(放行)
			totalCacheHits := cacheHitModify + cacheHitPass

			// 规则处理 = 请求总数 - 总缓存命中
			var ruleProcessing uint64
			if httpRequests > totalCacheHits {
				ruleProcessing = httpRequests - totalCacheHits
			}

			// 直接放行 = 请求总数 - 成功修改
			var directPass uint64
			if httpRequests > modified {
				directPass = httpRequests - modified
			}

			// 总缓存率 = 总缓存命中 / 请求总数 * 100%
			var totalCacheRatio float64
			if httpRequests > 0 {
				totalCacheRatio = (float64(totalCacheHits) * 100) / float64(httpRequests)
			}

			content := fmt.Sprintf(
				"current_connections:%d\n"+
					"total_requests:%d\n"+
					"rps:%.2f\n"+
					"successful_modifications:%d\n"+
					"direct_passthrough:%d\n"+
					"rule_processing:%d\n"+
					"cache_hit_modify:%d\n"+
					"cache_hit_pass:%d\n"+
					"total_cache_ratio:%.2f\n",
				activeConn,
				httpRequests,
				rps,
				modified,
				directPass,
				ruleProcessing,
				cacheHitModify,
				cacheHitPass,
				totalCacheRatio,
			)

			err := os.WriteFile(filePath, []byte(content), 0644)
			if err != nil {
				logrus.Warnf("Failed to write stats file: %v", err)
			}
		}
	}()
}
