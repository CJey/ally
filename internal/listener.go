package internal

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"time"
)

// ResolveAddress 解析全部支持的协议地址
func ResolveAddress(network, address string) (net.Addr, error) {
	afnet, _, _ := strings.Cut(network, ":") // IP的网络名带协议号，如ip4:icmp
	switch afnet {
	case "tcp", "tcp4", "tcp6":
		if laddr, err := net.ResolveTCPAddr(network, address); err != nil {
			return nil, fmt.Errorf("resolve %s address %s failed, %w", network, address, err)
		} else {
			return laddr, nil
		}
	case "udp", "udp4", "udp6":
		if laddr, err := net.ResolveUDPAddr(network, address); err != nil {
			return nil, fmt.Errorf("resolve %s address %s failed, %w", network, address, err)
		} else {
			return laddr, nil
		}
	case "unix", "unixpacket", "unixgram":
		if laddr, err := net.ResolveUnixAddr(network, address); err != nil {
			return nil, fmt.Errorf("resolve %s address %s failed, %w", network, address, err)
		} else {
			return laddr, nil
		}
	case "ip", "ip4", "ip6":
		if laddr, err := net.ResolveIPAddr(network, address); err != nil {
			return nil, fmt.Errorf("resolve %s address %s failed, %w", network, address, err)
		} else {
			return laddr, nil
		}
	default:
		return nil, net.UnknownNetworkError(network)
	}
}

// ListenSocket 统一Listener和PacketConn的Listen方式
func ListenSocket(network, address string) (net.Listener, net.PacketConn, error) {
	if _, err := ResolveAddress(network, address); err != nil {
		return nil, nil, err
	}

	switch network {
	case "tcp", "tcp4", "tcp6", "unix", "unixpacket":
		lsn, err := net.Listen(network, address)
		return lsn, nil, err
	default:
		conn, err := net.ListenPacket(network, address)
		return nil, conn, err
	}
}

// Cred 即syscall.Credential，windows下没有该类型故另行定义
type Cred struct {
	Uid    uint32
	Gid    uint32
	Groups []uint32
}

// ListenFile 监听给定地址，返回所得Listener或PacketConn的*os.File，unix socket另返回所建文件供RemoveSocket校验
// cred为app的运行身份，nil表示与ally相同
func ListenFile(network, address string, cred *Cred) (*os.File, string, os.FileInfo, error) {
	var lsn net.Listener
	var conn net.PacketConn
	var sock os.FileInfo
	var err error
	var path = SocketPath(network, address)
	if path != "" {
		lsn, conn, sock, err = listenUnixAs(network, path, cred)
	} else {
		lsn, conn, err = ListenSocket(network, address)
	}
	if err != nil {
		return nil, "", nil, err
	}

	var src interface{ File() (*os.File, error) }
	var laddr net.Addr
	if lsn != nil {
		defer lsn.Close()
		if l, ok := lsn.(*net.UnixListener); ok {
			l.SetUnlinkOnClose(false) // 路径须随交出的fd继续有效
		}
		src, _ = lsn.(interface{ File() (*os.File, error) })
		laddr = lsn.Addr()
	} else {
		defer conn.Close()
		src, _ = conn.(interface{ File() (*os.File, error) })
		laddr = conn.LocalAddr()
	}
	var file *os.File
	if src == nil {
		err = net.UnknownNetworkError(network)
	} else if file, err = src.File(); err == nil {
		return file, laddr.String(), sock, nil
	}
	if sock != nil {
		RemoveSocket(path, sock, cred) // fd未能交出，所建文件无人清理
	}
	return nil, "", nil, err
}

// RemoveSocket 以app的文件系统身份删除ListenFile所建的socket文件，已被替换的不删
func RemoveSocket(path string, sock os.FileInfo, cred *Cred) {
	withFsCred(cred, func() {
		if fi, err := os.Lstat(path); err == nil && os.SameFile(fi, sock) {
			os.Remove(path)
		}
	})
}

// SocketPath 返回unix socket在文件系统中的路径，非unix或抽象地址返回空
func SocketPath(network, address string) string {
	switch network {
	case "unix", "unixpacket", "unixgram":
		if address != "" && address[0] != '@' {
			return address
		}
	}
	return ""
}

// removeStaleSocket 删除已无人监听的socket文件：fd回收或进程退出都不会删除路径，残留会导致再次bind失败
func removeStaleSocket(network, path string) {
	if fi, err := os.Lstat(path); err != nil || fi.Mode()&os.ModeSocket == 0 {
		return
	}
	if conn, err := net.DialTimeout(network, path, time.Second); err == nil {
		conn.Close()
	} else if errors.Is(err, syscall.ECONNREFUSED) {
		os.Remove(path)
	}
}
