package session

import (
	"bytes"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"UAmask/internal/config"
	"UAmask/internal/events"
	"UAmask/internal/model"
	"UAmask/internal/policy"
	"UAmask/internal/rewrite"
	"UAmask/internal/state"
	"UAmask/internal/stats"
)

type bufferConn struct {
	reader *bytes.Buffer
	writer *bytes.Buffer
}

func newReadConn(data string) *bufferConn {
	return &bufferConn{reader: bytes.NewBufferString(data), writer: &bytes.Buffer{}}
}

func newWriteConn() *bufferConn {
	return &bufferConn{reader: &bytes.Buffer{}, writer: &bytes.Buffer{}}
}

func (conn *bufferConn) Read(value []byte) (int, error)   { return conn.reader.Read(value) }
func (conn *bufferConn) Write(value []byte) (int, error)  { return conn.writer.Write(value) }
func (conn *bufferConn) Close() error                     { return nil }
func (conn *bufferConn) LocalAddr() net.Addr              { return dummyAddr("local") }
func (conn *bufferConn) RemoteAddr() net.Addr             { return dummyAddr("remote") }
func (conn *bufferConn) SetDeadline(time.Time) error      { return nil }
func (conn *bufferConn) SetReadDeadline(time.Time) error  { return nil }
func (conn *bufferConn) SetWriteDeadline(time.Time) error { return nil }

type dummyAddr string

func (address dummyAddr) Network() string { return "buffer" }
func (address dummyAddr) String() string  { return string(address) }

type eventCollector struct {
	events []model.SessionEvent
}

func (collector *eventCollector) Append(event model.SessionEvent) {
	collector.events = append(collector.events, event)
}

func (collector *eventCollector) Count(eventType model.SessionEventType) int {
	count := 0
	for _, event := range collector.events {
		if event.Type == eventType {
			count++
		}
	}
	return count
}

func TestManagerProcessesActiveTrafficPath(t *testing.T) {
	t.Run("PATH-HTTP-201 rewrites requests and projects facts", func(t *testing.T) {
		manager, profiles, metrics, collector := newTrafficHarness(t, config.RewriteConfig{
			UserAgent:    "MaskUA",
			ForceReplace: true,
		}, config.FirewallConfig{})
		sessionState := testSessionState(80)
		src := newReadConn("GET / HTTP/1.1\r\nHost: example.com\r\nUser-Agent: A\r\n\r\n" +
			"GET /x HTTP/1.1\r\nHost: example.com\r\nUser-Agent: B\r\n\r\n")
		dst := newWriteConn()

		manager.processClientToUpstream(&sessionState, src, dst)

		if got := strings.Count(dst.writer.String(), "User-Agent: MaskUA"); got != 2 {
			t.Fatalf("rewritten User-Agent count = %d, want 2; output: %s", got, dst.writer.String())
		}
		if snapshot := metrics.Snapshot(); snapshot.HTTPRequests != 2 || snapshot.ModifiedRequests != 2 {
			t.Fatalf("stats snapshot = %+v, want 2 requests and modifications", snapshot)
		}
		if got := collector.Count(model.EventRewriteApplied); got != 2 {
			t.Fatalf("rewrite facts = %d, want 2", got)
		}
		if snapshot := profiles.Snapshot(sessionState.Target); snapshot.LastEventType != model.EventRewriteApplied {
			t.Fatalf("profile snapshot = %+v, want rewrite applied", snapshot)
		}
	})

	t.Run("PATH-NONHTTP-201 falls back and updates target profile", func(t *testing.T) {
		manager, profiles, metrics, collector := newTrafficHarness(t, config.RewriteConfig{
			UserAgent:    "MaskUA",
			ForceReplace: true,
		}, config.FirewallConfig{EnableBypass: true, NonHTTPThreshold: 2})
		sessionState := testSessionState(22)
		src := newReadConn("SSH-2.0-dropbear\r\n")
		dst := newWriteConn()

		manager.processClientToUpstream(&sessionState, src, dst)

		if got := dst.writer.String(); got != "SSH-2.0-dropbear\r\n" {
			t.Fatalf("fallback payload = %q", got)
		}
		if got := profiles.Snapshot(sessionState.Target).NonHTTPScore; got != 1 {
			t.Fatalf("non-HTTP score = %d, want 1", got)
		}
		if got := collector.Count(model.EventNonHTTPObserved); got != 1 {
			t.Fatalf("non-HTTP facts = %d, want 1", got)
		}
		if snapshot := metrics.Snapshot(); snapshot.HTTPRequests != 0 {
			t.Fatalf("HTTP requests = %d, want 0", snapshot.HTTPRequests)
		}
	})

	t.Run("PATH-DROP-201 firewall whitelist stops forwarding", func(t *testing.T) {
		manager, profiles, _, collector := newTrafficHarness(t, config.RewriteConfig{
			UserAgent: "MaskUA",
		}, config.FirewallConfig{
			UAWhitelist: []string{"BypassMe"},
			DropOnMatch: true,
		})
		sessionState := testSessionState(443)
		src := newReadConn("GET / HTTP/1.1\r\nHost: example.com\r\nUser-Agent: BypassMe/1.0\r\n\r\n")
		dst := newWriteConn()

		manager.processClientToUpstream(&sessionState, src, dst)

		if got := dst.writer.String(); got != "" {
			t.Fatalf("dropped request was forwarded: %q", got)
		}
		if got := collector.Count(model.EventRewriteSkipped); got != 1 {
			t.Fatalf("rewrite skipped facts = %d, want 1", got)
		}
		snapshot := profiles.Snapshot(sessionState.Target)
		if !snapshot.LastFirewallHit || snapshot.FirewallWhitelistHits != 1 {
			t.Fatalf("firewall profile = %+v, want recorded whitelist hit", snapshot)
		}
	})

	t.Run("PATH-FALLBACK-201 short protocol hint is preserved", func(t *testing.T) {
		manager, profiles, _, collector := newTrafficHarness(t, config.RewriteConfig{}, config.FirewallConfig{})
		sessionState := testSessionState(9000)
		src := newReadConn("abc")
		dst := newWriteConn()

		manager.processClientToUpstream(&sessionState, src, dst)

		if got := dst.writer.String(); got != "abc" {
			t.Fatalf("fallback payload = %q, want abc", got)
		}
		if got := collector.Count(model.EventSessionForwardFallback); got != 1 {
			t.Fatalf("fallback facts = %d, want 1", got)
		}
		if snapshot := profiles.Snapshot(sessionState.Target); snapshot.Found {
			t.Fatalf("fallback-only event unexpectedly created profile: %+v", snapshot)
		}
	})
}

func newTrafficHarness(
	t *testing.T,
	rewriteConfig config.RewriteConfig,
	firewallConfig config.FirewallConfig,
) (*DefaultManager, *state.Store, *stats.Stats, *eventCollector) {
	t.Helper()
	cache, err := rewrite.NewUACache(16)
	if err != nil {
		t.Fatalf("new UA cache: %v", err)
	}
	profiles := state.NewStore(state.Config{
		NonHTTPEnabled:         firewallConfig.EnableBypass,
		NonHTTPThreshold:       firewallConfig.NonHTTPThreshold,
		HTTPCooldownPeriod:     time.Minute,
		DecisionDelay:          time.Second,
		ProfileCleanupInterval: time.Hour,
	})
	metrics := stats.New()
	collector := &eventCollector{}
	dispatcher := events.NewDispatcher(profiles, metrics, collector)
	manager := NewManager(
		config.ListenConfig{BufferSize: 4096},
		policy.NewSessionPolicy(),
		rewrite.NewRequestRewriter(rewrite.NewEngine(rewriteConfig, config.RewriteRuntimeConfig{}, firewallConfig), cache),
		profiles,
		nil,
		dispatcher,
	)
	return manager, profiles, metrics, collector
}

func testSessionState(port int) model.SessionState {
	return model.SessionState{
		ID:          1,
		IngressKind: model.IngressKindRedirect,
		Target: model.Target{
			Address: "203.0.113.30:" + strconv.Itoa(port),
			IP:      "203.0.113.30",
			Port:    port,
		},
	}
}
