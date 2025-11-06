//go:build linux
// +build linux

package main

import (
	"fmt"
	"net"
	"unsafe"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

// getOriginalDst 获取被 REDIRECT 规则重定向前的原始目标地址
// 使用 SO_ORIGINAL_DST socket 选项，这是 iptables REDIRECT 目标填充的
func getOriginalDst(conn *net.TCPConn) (*net.TCPAddr, error) {
	file, err := conn.File()
	if err != nil {
		return nil, fmt.Errorf("failed to get file descriptor: %w", err)
	}
	defer file.Close()

	fd := int(file.Fd())

	// SO_ORIGINAL_DST = 80
	const SO_ORIGINAL_DST = 80

	// 使用 sockaddr 结构获取原始目标地址
	var addr unix.RawSockaddrInet4
	addrLen := uint32(unsafe.Sizeof(addr))

	_, _, errno := unix.Syscall6(
		unix.SYS_GETSOCKOPT,
		uintptr(fd),
		uintptr(unix.SOL_IP),
		uintptr(SO_ORIGINAL_DST),
		uintptr(unsafe.Pointer(&addr)),
		uintptr(unsafe.Pointer(&addrLen)),
		0,
	)

	if errno != 0 {
		return nil, fmt.Errorf("getsockopt SO_ORIGINAL_DST failed: %v", errno)
	}

	ip := net.IPv4(addr.Addr[0], addr.Addr[1], addr.Addr[2], addr.Addr[3])
	port := int(addr.Port>>8 | addr.Port<<8)
	logrus.Debugf("[Tproxy] getOriginalDst raw value: %d, parsed port: %d", addr.Port, port)
	return &net.TCPAddr{
		IP:   ip,
		Port: port,
	}, nil
}
