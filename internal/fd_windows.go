//go:build windows

package internal

import (
	"fmt"
	"net"
	"os"
)

func SendFile(via *net.UnixConn, file *os.File) error {
	return fmt.Errorf("do not support")
}

func RecvFile(via *net.UnixConn) (*os.File, error) {
	return nil, fmt.Errorf("do not support")
}
