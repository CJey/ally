package internal

import (
	"fmt"
	"net"
	"os"
)

// ResolveAddress 解析全部支持的协议地址
func ResolveAddress(network, address string) (net.Addr, error) {
	switch network {
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

// ListenFile 将Listen给定的地址，并将得到的Listenr或者PacketConn的提取*os.File返回
func ListenFile(network, address string) (*os.File, string, error) {
	lsn, conn, err := ListenSocket(network, address)
	if err != nil {
		return nil, "", err
	}
	if lsn != nil {
		defer lsn.Close()
		if l, ok := lsn.(*net.TCPListener); ok {
			file, err := l.File()
			return file, lsn.Addr().String(), err
		}
		if l, ok := lsn.(*net.UnixListener); ok {
			file, err := l.File()
			return file, lsn.Addr().String(), err
		}
	}
	if conn != nil {
		defer conn.Close()
		if c, ok := conn.(*net.UDPConn); ok {
			file, err := c.File()
			return file, c.LocalAddr().String(), err
		}
		if c, ok := conn.(*net.UnixConn); ok {
			file, err := c.File()
			return file, c.LocalAddr().String(), err
		}
		if c, ok := conn.(*net.IPConn); ok {
			file, err := c.File()
			return file, c.LocalAddr().String(), err
		}
	}
	return nil, "", net.UnknownNetworkError(network)
}
