//go:build linux

package internal

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// GetUCred 从unix socket中提取pid和uid
func GetUCred(c *net.UnixConn) (int, int, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, 0, fmt.Errorf("SyscallConn failed, %w", err)
	}

	var cred *unix.Ucred
	cerr := raw.Control(func(fd uintptr) {
		cred, err = unix.GetsockoptUcred(int(fd),
			unix.SOL_SOCKET,
			unix.SO_PEERCRED)
	})
	if cerr != nil {
		return 0, 0, fmt.Errorf("raw.Control failed, %w", cerr)
	}
	if err != nil {
		return 0, 0, fmt.Errorf("unix.GetsockoptUcred failed, %w", err)
	}
	return int(cred.Pid), int(cred.Uid), nil
}
