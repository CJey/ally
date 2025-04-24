package ally

import (
	"net"
)

// Listen 通过Ally获得Listener，当前支持TCP、Unix Packet
func Listen(network, address string) (net.Listener, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
		if laddr, err := net.ResolveTCPAddr(network, address); err != nil {
			return nil, err
		} else {
			return ListenTCP(network, laddr)
		}
	case "unix", "unixpacket":
		if laddr, err := net.ResolveUnixAddr(network, address); err != nil {
			return nil, err
		} else {
			return ListenUnix(network, laddr)
		}
	default:
		return nil, net.UnknownNetworkError(network)
	}
}

// Close 关闭掉经由Ally创建的Listener
func Close(lsn net.Listener) error {
	if l, ok := lsn.(*net.TCPListener); ok {
		return CloseTCP(l)
	}
	if l, ok := lsn.(*net.UnixListener); ok {
		return CloseUnix(l)
	}
	return lsn.Close()
}

// ListenTCP 通过Ally获得TCPListener
func ListenTCP(network string, laddr *net.TCPAddr) (*net.TCPListener, error) {
	if lsn, _, err := _ListenSocket(network, laddr.String()); err != nil {
		return nil, err
	} else {
		return lsn.(*net.TCPListener), nil
	}
}

// CloseTCP 关掉经由Ally创建的TCPListener
func CloseTCP(lsn *net.TCPListener) error {
	var laddr = lsn.Addr()
	if err := _CloseSocket(laddr.Network(), laddr.String()); err != nil {
		return err
	}
	return lsn.Close()
}

// ListenUnix 通过Ally获得UnixListener，面向连接的unix socket
func ListenUnix(network string, laddr *net.UnixAddr) (*net.UnixListener, error) {
	if lsn, _, err := _ListenSocket(network, laddr.String()); err != nil {
		return nil, err
	} else {
		return lsn.(*net.UnixListener), nil
	}
}

// CloseUnix 关掉经由Ally创建的UnixListener
func CloseUnix(lsn *net.UnixListener) error {
	var laddr = lsn.Addr()
	if err := _CloseSocket(laddr.Network(), laddr.String()); err != nil {
		return err
	}
	return lsn.Close()
}

// ListenPacket 通过Ally获得PacketConn，目前支持UDPConn、IPConn、Unixgram
func ListenPacket(network, address string) (net.PacketConn, error) {
	switch network {
	case "udp", "udp4", "udp6":
		if laddr, err := net.ResolveUDPAddr(network, address); err != nil {
			return nil, err
		} else {
			return ListenUDP(network, laddr)
		}
	case "ip", "ip4", "ip6":
		if laddr, err := net.ResolveIPAddr(network, address); err != nil {
			return nil, err
		} else {
			return ListenIP(network, laddr)
		}
	case "unixgram":
		if laddr, err := net.ResolveUnixAddr(network, address); err != nil {
			return nil, err
		} else {
			return ListenUnixgram(network, laddr)
		}
	default:
		return nil, net.UnknownNetworkError(network)
	}
}

// ClosePacket 关闭经由Ally创建的PacketConn
func ClosePacket(conn net.PacketConn) error {
	if c, ok := conn.(*net.UDPConn); ok {
		return CloseUDP(c)
	}
	if c, ok := conn.(*net.IPConn); ok {
		return CloseIP(c)
	}
	if c, ok := conn.(*net.UnixConn); ok {
		return CloseUnixgram(c)
	}
	return conn.Close()
}

// ListenUDP 通过Ally获得UDPConn
func ListenUDP(network string, laddr *net.UDPAddr) (*net.UDPConn, error) {
	if _, conn, err := _ListenSocket(network, laddr.String()); err != nil {
		return nil, err
	} else {
		return conn.(*net.UDPConn), nil
	}
}

// CloseUDP 关闭经由Ally创建的UDPConn
func CloseUDP(conn *net.UDPConn) error {
	var laddr = conn.LocalAddr()
	if err := _CloseSocket(laddr.Network(), laddr.String()); err != nil {
		return err
	}
	return conn.Close()
}

// ListenIP 通过Ally获得IPConn
func ListenIP(network string, laddr *net.IPAddr) (*net.IPConn, error) {
	if _, conn, err := _ListenSocket(network, laddr.String()); err != nil {
		return nil, err
	} else {
		return conn.(*net.IPConn), nil
	}
}

// CloseIP 关闭经由Ally创建的IPConn
func CloseIP(conn *net.IPConn) error {
	var laddr = conn.LocalAddr()
	if err := _CloseSocket(laddr.Network(), laddr.String()); err != nil {
		return err
	}
	return conn.Close()
}

// ListenUnixgram 通过Ally获得UnixConn，非面向连接的unix socket
func ListenUnixgram(network string, laddr *net.UnixAddr) (*net.UnixConn, error) {
	if _, conn, err := _ListenSocket(network, laddr.String()); err != nil {
		return nil, err
	} else {
		return conn.(*net.UnixConn), nil
	}
}

// CloseUnixgram 关闭经由Ally创建的UnixConn
func CloseUnixgram(conn *net.UnixConn) error {
	var laddr = conn.LocalAddr()
	if err := _CloseSocket(laddr.Network(), laddr.String()); err != nil {
		return err
	}
	return conn.Close()
}
