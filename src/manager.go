package main

import (
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type firewallAddItem struct {
	ip      string
	port    int
	setName string
	fwType  string
	timeout int
}

type portProfile struct {
	nonHttpScore    int         // 非 HTTP 事件计分
	httpLockExpires time.Time   // HTTP 豁免期截止时间
	lastEvent       time.Time   // 最近一次事件时间
	decisionTimer   *time.Timer // 延迟决策计时器
}

type reportEvent struct {
	ip   string
	port int
}

// FirewallSetManager 负责管理队列和唯一的 worker
type FirewallSetManager struct {
	queue    chan firewallAddItem
	stopChan chan struct{}
	wg       sync.WaitGroup
	log      *logrus.Logger

	// 端口画像
	nonHttpEventChan chan reportEvent
	httpEventChan    chan reportEvent
	portProfiles     map[string]*portProfile
	profileLock      sync.Mutex

	// 画像配置
	nonHttpThreshold       int           // 判定为非HTTP的连接数阈值
	httpCooldownPeriod     time.Duration // HTTP事件后的豁免期
	decisionDelay          time.Duration // 满足条件后的决策延迟
	profileCleanupInterval time.Duration // 清理陈旧画像的周期

	// 防火墙配置
	firewallIPSetName string
	firewallType      string
	defaultTimeout    int

	maxBatchSize int
	maxBatchWait time.Duration
}

func NewFirewallSetManager(log *logrus.Logger, queueSize int, cfg *Config) *FirewallSetManager {
	return &FirewallSetManager{
		queue:    make(chan firewallAddItem, queueSize),
		stopChan: make(chan struct{}),
		log:      log,

		// init
		nonHttpEventChan: make(chan reportEvent, queueSize),
		httpEventChan:    make(chan reportEvent, queueSize),
		portProfiles:     make(map[string]*portProfile),

		// default config
		nonHttpThreshold:       cfg.FirewallNonHttpThreshold,
		httpCooldownPeriod:     cfg.FirewallHttpCooldownPeriod,
		decisionDelay:          cfg.FirewallDecisionDelay,
		profileCleanupInterval: 5 * time.Minute,

		// 从配置中获取防火墙信息
		firewallIPSetName: cfg.FirewallIPSetName,
		firewallType:      cfg.FirewallType,
		defaultTimeout:    cfg.FirewallTimeout,

		maxBatchSize: 200,
		maxBatchWait: 100 * time.Millisecond,
	}
}

// ReportHttpEvent 一票否决
func (m *FirewallSetManager) ReportHttpEvent(ip string, port int) {
	select {
	case m.httpEventChan <- reportEvent{ip: ip, port: port}:
	default:
		m.log.Warnf("[Manager] HTTP event channel full, dropping event for %s:%d", ip, port)
	}
}

// ReportNonHttpEvent 累积计分
func (m *FirewallSetManager) ReportNonHttpEvent(ip string, port int) {
	select {
	case m.nonHttpEventChan <- reportEvent{ip: ip, port: port}:
	default:
		m.log.Warnf("[Manager] Non-HTTP event channel full, dropping event for %s:%d", ip, port)
	}
}

func (m *FirewallSetManager) Add(ip string, port int, setName, fwType string, timeout int) {
	if ip == "" || setName == "" {
		return
	}
	if net.ParseIP(ip) == nil {
		m.log.Warnf("[Manager] Invalid IP address: %s", ip)
		return
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9_]+$`).MatchString(setName) {
		m.log.Warnf("[Manager] Invalid set name: %s", setName)
		return
	}

	item := firewallAddItem{
		ip:      ip,
		port:    port,
		setName: setName,
		fwType:  fwType,
		timeout: timeout,
	}

	select {
	case m.queue <- item:
		// 成功
	case <-time.After(50 * time.Millisecond):
		m.log.Warnf("[Manager] Firewall add queue is full. Dropping item for %s", ip)
	}
}

// Start consumer worker
func (m *FirewallSetManager) Start() {
	m.wg.Add(1)
	go m.worker()
	m.log.Info("[Manager] FirewallSetManager worker started")
}

// Stop worker
func (m *FirewallSetManager) Stop() {
	m.log.Info("[Manager] Stopping FirewallSetManager worker...")
	close(m.stopChan)
	m.wg.Wait() // 等待 worker 完成
	m.log.Info("[Manager] FirewallSetManager worker stopped")
}

func (m *FirewallSetManager) batchKey(item firewallAddItem) string {
	return fmt.Sprintf("%s:%s", item.fwType, item.setName)
}

func (m *FirewallSetManager) worker() {
	defer m.wg.Done()
	batches := make(map[string]map[string]firewallAddItem)

	// 批处理计时器
	batchTimer := time.NewTimer(m.maxBatchWait)
	if !batchTimer.Stop() {
		<-batchTimer.C
	}

	// 画像清理计时器
	cleanupTicker := time.NewTicker(m.profileCleanupInterval)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-m.stopChan:
			// 收到停止信号
			close(m.queue)
			m.executeBatches(batches)
			// 清理所有画像计时器
			m.profileLock.Lock()
			for _, profile := range m.portProfiles {
				if profile.decisionTimer != nil {
					profile.decisionTimer.Stop()
				}
			}
			m.profileLock.Unlock()
			return

		case item := <-m.queue:
			key := m.batchKey(item)
			dedupKey := fmt.Sprintf("%s:%d", item.ip, item.port)
			if _, ok := batches[key]; !ok {
				batches[key] = make(map[string]firewallAddItem)
			}
			batches[key][dedupKey] = item
			if len(batches) == 1 && len(batches[key]) == 1 {
				batchTimer.Reset(m.maxBatchWait)
			}
			if len(batches[key]) >= m.maxBatchSize {
				m.executeBatches(batches)
				batches = make(map[string]map[string]firewallAddItem)
				if !batchTimer.Stop() {
					select {
					case <-batchTimer.C:
					default:
					}
				}
			}

		case <-batchTimer.C:
			if len(batches) > 0 {
				m.executeBatches(batches)
				batches = make(map[string]map[string]firewallAddItem)
			}

		case event := <-m.httpEventChan:
			m.handleHttpEvent(event.ip, event.port)

		case event := <-m.nonHttpEventChan:
			m.handleNonHttpEvent(event.ip, event.port)

		case <-cleanupTicker.C:
			m.cleanupProfiles()
		}
	}
}

func (m *FirewallSetManager) executeBatches(batches map[string]map[string]firewallAddItem) {
	if len(batches) == 0 {
		return
	}

	m.log.Debugf("[Manager] Executing %d batches...", len(batches))

	for key, itemsMap := range batches {
		if len(itemsMap) == 0 {
			continue
		}

		items := make([]firewallAddItem, 0, len(itemsMap))
		var firstFwType, firstSetName string
		for _, item := range itemsMap {
			items = append(items, item)
			if firstFwType == "" {
				firstFwType = item.fwType
				firstSetName = item.setName
			}
		}

		fwType := firstFwType
		setName := firstSetName
		itemCount := len(items)

		var cmd *exec.Cmd

		if fwType == "nft" {
			// nft add element inet fw4 <setName> { <ip1> . <port1> timeout <t1>, <ip2> . <port2> timeout <t2>, ... }
			args := []string{"add", "element", "inet", "fw4", setName, "{"}
			var elements []string
			for _, item := range items {
				elementStr := fmt.Sprintf("%s . %d", item.ip, item.port)
				if item.timeout > 0 {
					elementStr += fmt.Sprintf(" timeout %ds", item.timeout)
				}
				elements = append(elements, elementStr)
			}
			args = append(args, strings.Join(elements, ", "))
			args = append(args, "}")
			cmd = exec.Command("nft", args...)

		} else { // "ipset"
			cmd = exec.Command("ipset", "restore")
			var stdin strings.Builder
			for _, item := range items {
				if item.timeout > 0 {
					fmt.Fprintf(&stdin, "add %s %s,%d timeout %d -exist\n", setName, item.ip, item.port, item.timeout)
				} else {
					fmt.Fprintf(&stdin, "add %s %s,%d -exist\n", setName, item.ip, item.port)
				}
			}
			cmd.Stdin = strings.NewReader(stdin.String())
		}

		m.log.Debugf("[Manager] Executing [batch %s]: %s", key, cmd.String())

		errChan := make(chan error, 1)
		go func() {
			output, err := cmd.CombinedOutput()
			if err != nil {
				err = fmt.Errorf("error: %v, output: %s", err, string(output))
			}
			errChan <- err
		}()

		select {
		case err := <-errChan:
			if err != nil {
				m.log.Warnf("[Manager] Failed to execute batch for set %s (%s): %v",
					setName, fwType, err)
			} else {
				m.log.Debugf("[Manager] Successfully added %d unique IPs to firewall set %s (%s)",
					itemCount, setName, fwType)
			}
		case <-time.After(10 * time.Second):
			m.log.Warnf("[Manager] Timeout executing batch for set %s (%s) with %d unique items",
				setName, fwType, itemCount)
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
		}
	}
}

func (m *FirewallSetManager) handleHttpEvent(ip string, port int) {
	m.profileLock.Lock()
	defer m.profileLock.Unlock()

	key := fmt.Sprintf("%s:%d", ip, port)
	profile, ok := m.portProfiles[key]
	if !ok {
		profile = &portProfile{}
		m.portProfiles[key] = profile
	}

	// 豁免期内，直接返回
	if time.Now().Before(profile.httpLockExpires) {
		return
	}

	m.log.Debugf("[Manager] HTTP event for %s, resetting score and setting cooldown.", key)
	// 重置非HTTP分数，设置新的豁免期
	profile.nonHttpScore = 0
	profile.httpLockExpires = time.Now().Add(m.httpCooldownPeriod)
	profile.lastEvent = time.Now()

	// 如果存在决策计时器，说明之前已满足非HTTP条件，现在取消
	if profile.decisionTimer != nil {
		profile.decisionTimer.Stop()
		profile.decisionTimer = nil
		m.log.Infof("[Manager] Cancelled firewall add for %s due to new HTTP activity.", key)
	}
}

func (m *FirewallSetManager) handleNonHttpEvent(ip string, port int) {
	m.profileLock.Lock()
	defer m.profileLock.Unlock()

	key := fmt.Sprintf("%s:%d", ip, port)
	profile, ok := m.portProfiles[key]
	if !ok {
		profile = &portProfile{}
		m.portProfiles[key] = profile
	}

	// 在HTTP豁免期内，忽略此次非HTTP报告
	if time.Now().Before(profile.httpLockExpires) {
		m.log.Debugf("[Manager] Ignored non-HTTP event for %s during HTTP cooldown.", key)
		return
	}

	// 累积非HTTP分数
	profile.nonHttpScore++
	profile.lastEvent = time.Now()
	m.log.Debugf("[Manager] Non-HTTP event for %s, score is now %d.", key, profile.nonHttpScore)

	// 检查是否达到阈值
	if profile.nonHttpScore >= m.nonHttpThreshold {
		// 如果已有计时器，则重置它
		if profile.decisionTimer != nil {
			profile.decisionTimer.Reset(m.decisionDelay)
		} else {
			// 否则，启动新的决策计时器
			m.log.Infof("[Manager] Threshold reached for %s. Starting decision timer (%s).", key, m.decisionDelay)
			profile.decisionTimer = time.AfterFunc(m.decisionDelay, func() {
				m.finalizeDecision(ip, port)
			})
		}
	}
}

func (m *FirewallSetManager) finalizeDecision(ip string, port int) {
	m.profileLock.Lock()
	defer m.profileLock.Unlock()

	key := fmt.Sprintf("%s:%d", ip, port)
	profile, ok := m.portProfiles[key]

	// 再次检查条件，如果在延迟期间收到了HTTP事件，profile可能已被修改或删除
	if !ok || profile.nonHttpScore < m.nonHttpThreshold || time.Now().Before(profile.httpLockExpires) {
		m.log.Infof("[Manager] Final decision for %s aborted (conditions no longer met).", key)
		if ok {
			profile.decisionTimer = nil
		}
		return
	}

	m.log.Infof("[Manager] Decision final for %s. Adding to firewall.", key)
	m.Add(ip, port, m.firewallIPSetName, m.firewallType, m.defaultTimeout)

	// 从画像中删除，防止重复添加
	delete(m.portProfiles, key)
}

func (m *FirewallSetManager) cleanupProfiles() {
	m.profileLock.Lock()
	defer m.profileLock.Unlock()

	m.log.Debug("[Manager] Running port profile cleanup...")
	now := time.Now()
	cleanedCount := 0
	for key, profile := range m.portProfiles {
		// 清理那些长时间不活跃，且未进入决策流程的条目
		if profile.decisionTimer == nil && now.Sub(profile.lastEvent) > m.profileCleanupInterval {
			delete(m.portProfiles, key)
			cleanedCount++
		}
	}
	if cleanedCount > 0 {
		m.log.Debugf("[Manager] Cleaned up %d stale port profiles.", cleanedCount)
	}
}
