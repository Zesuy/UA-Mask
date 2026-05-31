package proxy

import (
	"bytes"
	"net"
	"testing"
	"time"

	"UAmask/internal/config"
	"UAmask/internal/rewrite"
)

type bufferConn struct {
	reader *bytes.Buffer
	writer *bytes.Buffer
}

func newReadConn(data string) *bufferConn {
	return &bufferConn{
		reader: bytes.NewBufferString(data),
		writer: &bytes.Buffer{},
	}
}

func newWriteConn() *bufferConn {
	return &bufferConn{
		reader: &bytes.Buffer{},
		writer: &bytes.Buffer{},
	}
}

func (c *bufferConn) Read(p []byte) (int, error)       { return c.reader.Read(p) }
func (c *bufferConn) Write(p []byte) (int, error)      { return c.writer.Write(p) }
func (c *bufferConn) Close() error                     { return nil }
func (c *bufferConn) LocalAddr() net.Addr              { return dummyAddr("local") }
func (c *bufferConn) RemoteAddr() net.Addr             { return dummyAddr("remote") }
func (c *bufferConn) SetDeadline(time.Time) error      { return nil }
func (c *bufferConn) SetReadDeadline(time.Time) error  { return nil }
func (c *bufferConn) SetWriteDeadline(time.Time) error { return nil }

type dummyAddr string

func (d dummyAddr) Network() string { return "buffer" }
func (d dummyAddr) String() string  { return string(d) }

type reporterStub struct {
	httpCount    int
	nonHTTPCount int
}

func (r *reporterStub) ReportHTTP(string, int)    { r.httpCount++ }
func (r *reporterStub) ReportNonHTTP(string, int) { r.nonHTTPCount++ }

type metricsStub struct {
	httpRequests     int
	modifiedRequests int
	cacheHitModified int
	cacheHitPass     int
}

func (m *metricsStub) RecordHTTPRequest()         { m.httpRequests++ }
func (m *metricsStub) RecordModifiedRequest()     { m.modifiedRequests++ }
func (m *metricsStub) RecordCacheHitModified()    { m.cacheHitModified++ }
func (m *metricsStub) RecordCacheHitPassthrough() { m.cacheHitPass++ }

func TestProcessorRewritesHTTPRequests(t *testing.T) {
	t.Parallel()

	engine := rewrite.NewEngine(
		config.RewriteConfig{UserAgent: "MaskUA", ForceReplace: true},
		config.RewriteRuntimeConfig{},
		config.FirewallConfig{},
	)
	cache, err := rewrite.NewUACache(16)
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}

	reporter := &reporterStub{}
	metrics := &metricsStub{}
	processor := NewProcessor(
		config.ListenConfig{BufferSize: 4096},
		rewrite.NewRequestRewriter(engine, cache),
		reporter,
		metrics,
	)

	src := newReadConn("GET / HTTP/1.1\r\nHost: example.com\r\nUser-Agent: A\r\n\r\nGET /x HTTP/1.1\r\nHost: example.com\r\nUser-Agent: B\r\n\r\n")
	dst := newWriteConn()

	processor.Process(dst, src, Target{Address: "1.1.1.1:80", IP: "1.1.1.1", Port: 80})

	output := dst.writer.String()
	if !bytes.Contains([]byte(output), []byte("User-Agent: MaskUA")) {
		t.Fatalf("expected rewritten User-Agent in output: %s", output)
	}
	if metrics.httpRequests != 2 {
		t.Fatalf("expected 2 HTTP requests, got %d", metrics.httpRequests)
	}
	if metrics.modifiedRequests != 2 {
		t.Fatalf("expected 2 modified requests, got %d", metrics.modifiedRequests)
	}
	if reporter.httpCount != 2 {
		t.Fatalf("expected 2 HTTP reports, got %d", reporter.httpCount)
	}
}

func TestProcessorFallsBackForNonHTTP(t *testing.T) {
	t.Parallel()

	engine := rewrite.NewEngine(
		config.RewriteConfig{UserAgent: "MaskUA", ForceReplace: true},
		config.RewriteRuntimeConfig{},
		config.FirewallConfig{},
	)
	cache, err := rewrite.NewUACache(16)
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}

	reporter := &reporterStub{}
	metrics := &metricsStub{}
	processor := NewProcessor(
		config.ListenConfig{BufferSize: 4096},
		rewrite.NewRequestRewriter(engine, cache),
		reporter,
		metrics,
	)

	src := newReadConn("SSH-2.0-dropbear\r\n")
	dst := newWriteConn()

	processor.Process(dst, src, Target{Address: "1.1.1.1:22", IP: "1.1.1.1", Port: 22})

	if got := dst.writer.String(); got != "SSH-2.0-dropbear\r\n" {
		t.Fatalf("expected passthrough payload, got %q", got)
	}
	if reporter.nonHTTPCount != 1 {
		t.Fatalf("expected 1 non-HTTP report, got %d", reporter.nonHTTPCount)
	}
	if metrics.httpRequests != 0 {
		t.Fatalf("expected no HTTP requests, got %d", metrics.httpRequests)
	}
}
