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

// FirewallSetManager 负责管理队列和唯一的 worker
type FirewallSetManager struct {
	queue    chan firewallAddItem
	stopChan chan struct{}
	wg       sync.WaitGroup
	log      *logrus.Logger

	maxBatchSize int
	maxBatchWait time.Duration
}

func NewFirewallSetManager(log *logrus.Logger, queueSize int) *FirewallSetManager {
	return &FirewallSetManager{
		queue:    make(chan firewallAddItem, queueSize),
		stopChan: make(chan struct{}),
		log:      log,

		maxBatchSize: 200,
		maxBatchWait: 100 * time.Millisecond,
	}
}

func (m *FirewallSetManager) Add(ip string, port int, setName, fwType string, timeout int) {
	if ip == "" || setName == "" {
		return
	}
	if net.ParseIP(ip) == nil {
		m.log.Warnf("Invalid IP address: %s", ip)
		return
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9_]+$`).MatchString(setName) {
		m.log.Warnf("Invalid set name: %s", setName)
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
		m.log.Warnf("Firewall add queue is full. Dropping item for %s", ip)
	}
}

// Start consumer worker
func (m *FirewallSetManager) Start() {
	m.wg.Add(1)
	go m.worker()
	m.log.Info("FirewallSetManager worker started")
}

// Stop worker
func (m *FirewallSetManager) Stop() {
	m.log.Info("Stopping FirewallSetManager worker...")
	close(m.stopChan)
	m.wg.Wait() // 等待 worker 完成
	m.log.Info("FirewallSetManager worker stopped")
}

func (m *FirewallSetManager) batchKey(item firewallAddItem) string {
	return fmt.Sprintf("%s:%s", item.fwType, item.setName)
}

func (m *FirewallSetManager) worker() {
	defer m.wg.Done()
	batches := make(map[string]map[string]firewallAddItem)

	// 批处理计时器
	timer := time.NewTimer(m.maxBatchWait)
	if !timer.Stop() {
		<-timer.C
	}

	for {
		select {
		case <-m.stopChan:
			// 收到停止信号
			close(m.queue)
			for item := range m.queue {
				key := m.batchKey(item)
				dedupKey := fmt.Sprintf("%s:%d", item.ip, item.port)
				if _, ok := batches[key]; !ok {
					batches[key] = make(map[string]firewallAddItem)
				}
				batches[key][dedupKey] = item
			}
			m.executeBatches(batches)
			return

		case item := <-m.queue:
			key := m.batchKey(item)
			dedupKey := fmt.Sprintf("%s:%d", item.ip, item.port)

			// 检查外层 map 是否已初始化
			if _, ok := batches[key]; !ok {
				batches[key] = make(map[string]firewallAddItem)
			}

			// 添加到内层 map (自动去重)
			batches[key][dedupKey] = item
			if len(batches) == 1 && len(batches[key]) == 1 {
				timer.Reset(m.maxBatchWait)
			}

			if len(batches[key]) >= m.maxBatchSize {
				m.log.Debugf("Batch full for %s, executing all pending batches", key)
				m.executeBatches(batches)
				batches = make(map[string]map[string]firewallAddItem)
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			}

		case <-timer.C:
			// 计时器触发
			if len(batches) > 0 {
				m.log.Debugf("Batch timer expired, executing %d pending batches", len(batches))
				m.executeBatches(batches)
				batches = make(map[string]map[string]firewallAddItem)
			}
		}
	}
}

func (m *FirewallSetManager) executeBatches(batches map[string]map[string]firewallAddItem) {
	if len(batches) == 0 {
		return
	}

	m.log.Debugf("Executing %d batches...", len(batches))

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
					fmt.Fprintf(&stdin, "add %s %s,%d timeout %d\n", setName, item.ip, item.port, item.timeout)
				} else {
					fmt.Fprintf(&stdin, "add %s %s,%d\n", setName, item.ip, item.port)
				}
			}
			cmd.Stdin = strings.NewReader(stdin.String())
		}

		m.log.Debugf("Executing [batch %s]: %s", key, cmd.String())

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
				m.log.Warnf("Failed to execute batch for set %s (%s): %v",
					setName, fwType, err)
			} else {
				m.log.Debugf("Successfully added %d unique IPs to firewall set %s (%s)",
					itemCount, setName, fwType)
			}
		case <-time.After(10 * time.Second):
			m.log.Warnf("Timeout executing batch for set %s (%s) with %d unique items",
				setName, fwType, itemCount)
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
		}
	}
}
