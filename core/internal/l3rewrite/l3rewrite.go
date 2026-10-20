// Package l3rewrite implements Layer-3 (IP/TCP header) rewriting, ported from
// the UA3F project. It is independent of the HTTP User-Agent rewrite: TTL is
// normalized through a firewall rule, while IPv4 Identification zeroing, TCP
// timestamp removal and TCP initial-window rewriting are performed in
// userspace by queuing matching packets through NFQUEUE.
//
// The package is platform-aware. On Linux it installs firewall rules and runs
// an NFQUEUE packet handler; on other platforms it is a no-op so the rest of
// the application still compiles and runs.
package l3rewrite

import (
	"UAmask/internal/config"

	"github.com/sirupsen/logrus"
)

// Config carries the L3 rewrite settings derived from the application
// configuration together with the firewall backend used to install rules.
type Config struct {
	TTL      bool
	TTLValue uint
	IPID     bool
	TCPTS    bool
	TCPWIN   bool
	Backend  string
}

// FromConfig builds a l3rewrite.Config from the application configuration.
func FromConfig(cfg *config.Config) Config {
	return Config{
		TTL:      cfg.L3Rewrite.TTL,
		TTLValue: cfg.L3Rewrite.TTLValue,
		IPID:     cfg.L3Rewrite.IPID,
		TCPTS:    cfg.L3Rewrite.TCPTS,
		TCPWIN:   cfg.L3Rewrite.TCPWIN,
		Backend:  cfg.Firewall.Backend,
	}
}

// Enabled reports whether any L3 rewrite feature is turned on.
func (c Config) Enabled() bool {
	return c.TTL || c.IPID || c.TCPTS || c.TCPWIN
}

// needsNfqueue reports whether the userspace packet handler must be started.
// TTL rewriting is applied purely by a firewall rule and does not require the
// NFQUEUE path.
func (c Config) needsNfqueue() bool {
	return c.IPID || c.TCPTS || c.TCPWIN
}

// Server owns the L3 rewrite runtime lifecycle.
type Server interface {
	Start() error
	Close() error
}

// New constructs an L3 rewrite server appropriate for the current platform.
// The returned server is always safe to Start/Close; on platforms without
// netfilter support it is a no-op.
func New(cfg Config, log *logrus.Logger) Server {
	return newPlatformServer(cfg, log)
}
