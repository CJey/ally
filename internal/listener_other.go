//go:build !linux

package internal

import (
	"errors"
	"net"
	"os"
)

// listenUnixAs 没有线程级的文件系统身份可用，只能bind后转交属主，存在竞态，仅供开发环境
func listenUnixAs(network, path string, cred *Cred) (net.Listener, net.PacketConn, os.FileInfo, error) {
	removeStaleSocket(network, path)
	lsn, conn, err := ListenSocket(network, path)
	if err != nil {
		return nil, nil, nil, err
	}
	if cred != nil {
		if err := os.Lchown(path, int(cred.Uid), int(cred.Gid)); err != nil {
			if lsn != nil {
				lsn.Close()
			} else {
				conn.Close()
			}
			os.Remove(path)
			return nil, nil, nil, err
		}
	}
	sock, _ := os.Lstat(path)
	return lsn, conn, sock, nil
}

// withFsCred 没有线程级的文件系统身份可用，身份不同时不代为执行，以免越权
func withFsCred(cred *Cred, fn func()) error {
	if cred != nil {
		return errors.ErrUnsupported
	}
	fn()
	return nil
}
