//go:build !linux

package sysstat

import (
	"fmt"
	"runtime"
)

func getStat(pid int) (*SysInfo, error) {
	return nil, fmt.Errorf("sysstat not supported on %s", runtime.GOOS)
}

func forget(pid int) {}
