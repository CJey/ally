//go:build windows

package internal

import (
	"fmt"
	"net"
)

func GetUCred(c *net.UnixConn) (int, int, error) {
	return 0, 0, fmt.Errorf("do not support")
}
