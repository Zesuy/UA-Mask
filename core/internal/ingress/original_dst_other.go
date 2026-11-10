//go:build !linux
// +build !linux

package ingress

import (
	"errors"
	"net"
)

// ErrUnsupportedPlatform 表示当前平台不支持 SO_ORIGINAL_DST。
//
// REDIRECT 模式的入口依赖 netfilter 的 SO_ORIGINAL_DST socket option，
// 该能力只存在于 Linux。非 Linux 平台保留这个 stub 的目的是让
// `go build ./...` 与 `go test ./...` 在开发机上可运行（开发者通常是
// macOS / Windows），只在真正尝试 Accept 时返回错误。
var ErrUnsupportedPlatform = errors.New("ingress: redirect mode requires Linux (SO_ORIGINAL_DST unavailable)")

func getOriginalDst(conn *net.TCPConn) (*net.TCPAddr, error) {
	return nil, ErrUnsupportedPlatform
}
