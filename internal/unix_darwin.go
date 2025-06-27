//go:build darwin

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
		return 0, 0, fmt.Errorf("SyscallConn: %w", err)
	}

	var cred *unix.Xucred
	var pid int
	cerr := raw.Control(func(fd uintptr) {
		cred, err = unix.GetsockoptXucred(int(fd),
			unix.SOL_LOCAL,
			unix.LOCAL_PEERCRED)
		if err != nil {
			err = fmt.Errorf("unix.GetsockoptXucred failed, %w", err)
			return
		}
		pid, err = unix.GetsockoptInt(int(fd),
			unix.SOL_LOCAL,
			unix.LOCAL_PEERPID)
		if err != nil {
			err = fmt.Errorf("unix.GetsockoptInt failed, %w", err)
		}
	})
	if cerr != nil {
		return 0, 0, fmt.Errorf("raw.Control failed, %w", cerr)
	}
	if err != nil {
		return 0, 0, err
	}
	return pid, int(cred.Uid), nil
}
