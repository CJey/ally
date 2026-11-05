//go:build linux

package internal

import (
	"fmt"
	"net"
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

// listenUnixAs 以app的文件系统身份清理残留、bind并记下所建文件，结果与app自建一致，也杜绝借ally越权建文件
func listenUnixAs(network, path string, cred *Cred) (lsn net.Listener, conn net.PacketConn, sock os.FileInfo, err error) {
	if ferr := withFsCred(cred, func() {
		removeStaleSocket(network, path)
		if lsn, conn, err = ListenSocket(network, path); err == nil {
			sock, _ = os.Lstat(path)
		}
	}); ferr != nil {
		return nil, nil, nil, ferr
	}
	return lsn, conn, sock, err
}

// withFsCred 在app的文件系统身份下执行fn，cred为nil表示与ally相同
func withFsCred(cred *Cred, fn func()) error {
	if cred == nil {
		fn()
		return nil
	}
	var ch = make(chan error, 1)
	go func() {
		// 先锁线程再改身份且不还原：锁定期间运行时不会从本线程克隆新线程，goroutine带锁退出时线程随之销毁
		runtime.LockOSThread()
		if err := setFsCred(cred); err != nil {
			ch <- err
			return
		}
		fn()
		ch <- nil
	}()
	return <-ch
}

// setFsCred 只改当前线程，x/sys/unix的这几个调用不会像syscall包那样同步到全部线程
func setFsCred(cred *Cred) error {
	var groups = make([]int, len(cred.Groups))
	for i, g := range cred.Groups {
		groups[i] = int(g)
	}
	if err := unix.Setgroups(groups); err != nil {
		return fmt.Errorf("setgroups failed, %w", err)
	}
	// setfsgid/setfsuid不报告失败，只能回读确认
	unix.SetfsgidRetGid(int(cred.Gid))
	unix.SetfsuidRetUid(int(cred.Uid))
	if gid, _ := unix.SetfsgidRetGid(-1); gid != int(cred.Gid) {
		return fmt.Errorf("setfsgid %d failed", cred.Gid)
	}
	if uid, _ := unix.SetfsuidRetUid(-1); uid != int(cred.Uid) {
		return fmt.Errorf("setfsuid %d failed", cred.Uid)
	}
	return nil
}
