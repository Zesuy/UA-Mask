//go:build linux

package l3rewrite

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"

	nfq "github.com/florianl/go-nfqueue/v2"

	"github.com/sirupsen/logrus"
)

const (
	// nfqueueNum is the NFQUEUE number L3 rewrite packets are queued to. It is
	// deliberately separate from UA-Mask's redirect port and the bypass set.
	nfqueueNum = 10301

	// tableFamily/tableName identify the dedicated nftables table used for L3
	// rewrite rules. A separate table keeps these rules isolated from the fw4
	// managed "inet fw4" table so a firewall reload does not wipe them.
	tableFamily = "inet"
	tableName   = "UAmask_l3"
)

const (
	maxQueueLen  = 4000
	maxPacketLen = 1600
	workerChan   = 2000
)

type linuxServer struct {
	cfg Config
	log *logrus.Logger
	nfq *nfqueueServer
}

func newPlatformServer(cfg Config, log *logrus.Logger) Server {
	return &linuxServer{cfg: cfg, log: log}
}

func (s *linuxServer) Start() error {
	if !s.cfg.Enabled() {
		return nil
	}

	// Best-effort cleanup of any rules left behind by a previous run.
	_ = s.cleanupRules()

	backend := s.cfg.Backend
	if backend != "nft" {
		backend = "ipt"
	}
	if backend == "nft" {
		if err := s.setupNFT(); err != nil {
			return fmt.Errorf("l3 rewrite nftables setup: %w", err)
		}
	} else {
		if err := s.setupIPT(); err != nil {
			return fmt.Errorf("l3 rewrite iptables setup: %w", err)
		}
	}

	s.log.Infof("[L3Rewrite] enabled (backend=%s ttl=%v ttl_value=%d ipid=%v tcpts=%v tcpwin=%v)",
		backend, s.cfg.TTL, s.cfg.TTLValue, s.cfg.IPID, s.cfg.TCPTS, s.cfg.TCPWIN)

	if s.cfg.needsNfqueue() {
		nfqServer := &nfqueueServer{queueNum: nfqueueNum, cfg: s.cfg, log: s.log}
		if err := nfqServer.Start(); err != nil {
			_ = s.cleanupRules()
			return fmt.Errorf("l3 rewrite nfqueue start: %w", err)
		}
		s.nfq = nfqServer
	}
	return nil
}

func (s *linuxServer) Close() error {
	if s.nfq != nil {
		s.nfq.Close()
		s.nfq = nil
	}
	return s.cleanupRules()
}

func (s *linuxServer) run(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s *linuxServer) setupNFT() error {
	if err := s.run("nft", "add", "table", tableFamily, tableName); err != nil {
		return err
	}

	chainSpec := "{ type filter hook postrouting priority mangle; policy accept; }"

	if s.cfg.TTL {
		if err := s.run("nft", "add", "chain", tableFamily, tableName, "ttl", chainSpec); err != nil {
			return err
		}
		if err := s.run("nft", "add", "rule", tableFamily, tableName, "ttl", "ip ttl set "+strconv.FormatUint(uint64(s.cfg.TTLValue), 10)); err != nil {
			return err
		}
	}

	needSynQueue := s.cfg.TCPTS || s.cfg.TCPWIN
	needIPIDQueue := s.cfg.IPID
	if needSynQueue || needIPIDQueue {
		if err := s.run("nft", "add", "chain", tableFamily, tableName, "rewrite", chainSpec); err != nil {
			return err
		}
		if needSynQueue {
			rule := "tcp flags syn counter queue num " + strconv.Itoa(nfqueueNum) + " bypass"
			if err := s.run("nft", "add", "rule", tableFamily, tableName, "rewrite", rule); err != nil {
				return err
			}
		}
		if needIPIDQueue {
			rule := "meta l4proto tcp ip id != 0 counter queue num " + strconv.Itoa(nfqueueNum) + " bypass"
			if err := s.run("nft", "add", "rule", tableFamily, tableName, "rewrite", rule); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *linuxServer) setupIPT() error {
	ipt := "iptables"
	table := "mangle"
	chain := "POSTROUTING"
	queueNumStr := strconv.Itoa(nfqueueNum)

	if s.cfg.TTL {
		if err := s.run(ipt, "-t", table, "-A", chain, "-j", "TTL", "--ttl-set", strconv.FormatUint(uint64(s.cfg.TTLValue), 10)); err != nil {
			return err
		}
	}
	if (s.cfg.TCPTS || s.cfg.TCPWIN) && !s.cfg.IPID {
		if err := s.run(ipt, "-t", table, "-A", chain, "-p", "tcp", "--tcp-flags", "SYN", "SYN", "-j", "NFQUEUE", "--queue-num", queueNumStr, "--queue-bypass"); err != nil {
			return err
		}
	}
	if s.cfg.IPID {
		if err := s.run(ipt, "-t", table, "-A", chain, "-p", "tcp", "-j", "NFQUEUE", "--queue-num", queueNumStr, "--queue-bypass"); err != nil {
			return err
		}
	}
	return nil
}

func (s *linuxServer) cleanupRules() error {
	if s.cfg.Backend == "nft" {
		_ = s.run("nft", "delete", "table", tableFamily, tableName)
		return nil
	}

	ipt := "iptables"
	table := "mangle"
	chain := "POSTROUTING"
	queueNumStr := strconv.Itoa(nfqueueNum)

	if s.cfg.IPID {
		_ = s.run(ipt, "-t", table, "-D", chain, "-p", "tcp", "-j", "NFQUEUE", "--queue-num", queueNumStr, "--queue-bypass")
	}
	if (s.cfg.TCPTS || s.cfg.TCPWIN) && !s.cfg.IPID {
		_ = s.run(ipt, "-t", table, "-D", chain, "-p", "tcp", "--tcp-flags", "SYN", "SYN", "-j", "NFQUEUE", "--queue-num", queueNumStr, "--queue-bypass")
	}
	if s.cfg.TTL {
		_ = s.run(ipt, "-t", table, "-D", chain, "-j", "TTL", "--ttl-set", strconv.FormatUint(uint64(s.cfg.TTLValue), 10))
	}
	return nil
}

// nfqueueServer receives packets queued by the firewall rules and applies the
// configured TCP/IP header rewrites before re-injecting them.
type nfqueueServer struct {
	queueNum uint16
	cfg      Config
	log      *logrus.Logger
	nf       *nfq.Nfqueue
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	chans    []chan *nfq.Attribute
}

func (s *nfqueueServer) Start() error {
	config := nfq.Config{
		NfQueue:      s.queueNum,
		MaxQueueLen:  maxQueueLen,
		MaxPacketLen: maxPacketLen,
		Copymode:     nfq.NfQnlCopyPacket,
	}
	nf, err := nfq.Open(&config)
	if err != nil {
		return fmt.Errorf("nfq.Open: %w", err)
	}
	s.nf = nf

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	workers := runtime.NumCPU()
	if workers < 2 {
		workers = 2
	}
	if workers > 8 {
		workers = 8
	}
	s.chans = make([]chan *nfq.Attribute, workers)
	for i := range s.chans {
		s.chans[i] = make(chan *nfq.Attribute, workerChan)
		s.wg.Add(1)
		go s.worker(s.chans[i])
	}

	err = nf.RegisterWithErrorFunc(ctx,
		func(a nfq.Attribute) int {
			if a.Payload == nil || a.PacketID == nil {
				if a.PacketID != nil {
					_ = nf.SetVerdict(*a.PacketID, nfq.NfAccept)
				}
				return 0
			}
			idx := int(flowHash(*a.Payload)) % workers
			select {
			case s.chans[idx] <- &a:
			default:
				// Queue full: accept the packet without processing to avoid
				// stalling traffic.
				_ = nf.SetVerdict(*a.PacketID, nfq.NfAccept)
			}
			return 0
		},
		func(e error) int {
			if ctx.Err() != nil {
				return 0
			}
			s.log.Debugf("[L3Rewrite] nfqueue error: %v", e)
			return 0
		},
	)
	if err != nil {
		return fmt.Errorf("nf.RegisterWithErrorFunc: %w", err)
	}
	return nil
}

func (s *nfqueueServer) Close() {
	if s.cancel != nil {
		s.cancel()
	}
	for _, ch := range s.chans {
		if ch != nil {
			close(ch)
		}
	}
	s.wg.Wait()
	if s.nf != nil {
		_ = s.nf.Close()
	}
}

func (s *nfqueueServer) worker(ch <-chan *nfq.Attribute) {
	defer s.wg.Done()
	for a := range ch {
		s.handle(a)
	}
}

func (s *nfqueueServer) handle(a *nfq.Attribute) {
	if a == nil || a.PacketID == nil {
		return
	}
	if a.Payload == nil {
		_ = s.nf.SetVerdict(*a.PacketID, nfq.NfAccept)
		return
	}

	modified, changed, err := rewrite(*a.Payload, s.cfg)
	if err != nil {
		s.log.Debugf("[L3Rewrite] packet rewrite failed: %v", err)
		_ = s.nf.SetVerdict(*a.PacketID, nfq.NfAccept)
		return
	}
	if !changed {
		_ = s.nf.SetVerdict(*a.PacketID, nfq.NfAccept)
		return
	}
	if err := s.nf.SetVerdictWithOption(*a.PacketID, nfq.NfAccept, nfq.WithAlteredPacket(modified)); err != nil {
		s.log.Debugf("[L3Rewrite] set altered packet verdict failed: %v", err)
		_ = s.nf.SetVerdict(*a.PacketID, nfq.NfAccept)
	}
}
