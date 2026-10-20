//go:build !linux

package l3rewrite

import "github.com/sirupsen/logrus"

// noopServer is returned on platforms that do not support netfilter/NFQUEUE.
// It allows the L3 rewrite configuration to be validated and logged without
// performing any packet rewriting.
type noopServer struct{}

func newPlatformServer(cfg Config, log *logrus.Logger) Server {
	return noopServer{}
}

func (noopServer) Start() error { return nil }
func (noopServer) Close() error { return nil }
