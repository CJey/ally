package internal

import (
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"
)

const (
	_DUPLEX_MAGIC_PREFIX = "ALLY_UNIX_SOCK" // 14bytes + 2bytes type
	_CONN_TYPE_DATA_RPC  = uint16(0)
	_CONN_TYPE_CTRL_FD   = uint16(1)
)

func sendMagic(conn net.Conn, tp uint16) error {
	magic := make([]byte, 16)
	copy(magic, _DUPLEX_MAGIC_PREFIX)
	binary.BigEndian.PutUint16(magic[14:16], uint16(tp))
	if _, err := conn.Write(magic); err != nil {
		return err
	}
	return nil
}

// DuplexConnDataRpc 将连接配置为数据信道
func DuplexConnDataRpc(conn net.Conn) error {
	return sendMagic(conn, _CONN_TYPE_DATA_RPC)
}

// DuplexConnCtrlFd 将连接配置为控制信道
func DuplexConnCtrlFd(conn net.Conn) error {
	return sendMagic(conn, _CONN_TYPE_CTRL_FD)
}

// UnixListener 包装了标准库的UnixListener，主要是为了能在grpc场景中，能拦截grpc创建的连接。
// grpc对连接的包装层次较深，无法简单的得到请求归属的原始连接，只能出此下策
type UnixListener struct {
	*net.UnixListener

	// HijackCtrlFd 允许从连接中分类控制信道
	HijackCtrlFd func(*net.UnixConn)
}

// Accept 获取新连接，如果是数据信道，则返回上层共grpc使用，如果是控制信道，则交给HijackCtrlFd处理
func (l *UnixListener) Accept() (net.Conn, error) {
	magic := make([]byte, 16)
	for {
		conn, err := l.UnixListener.Accept()
		if err != nil {
			return conn, err
		}

		uconn := conn.(*net.UnixConn)
		tconn := TLSServerConn(uconn)

		if _, err := io.ReadFull(tconn, magic); err != nil {
			tconn.Close()
			continue
		} else if string(magic[:14]) != _DUPLEX_MAGIC_PREFIX {
			tconn.Close()
			continue
		}

		if tp := binary.BigEndian.Uint16(magic[14:16]); tp == _CONN_TYPE_DATA_RPC {
			pid, uid, _ := GetUCred(uconn)
			return newUnixConn(tconn, pid, uid), nil
		} else if tp == _CONN_TYPE_CTRL_FD && l.HijackCtrlFd != nil {
			l.HijackCtrlFd(uconn)
		} else {
			tconn.Close()
		}
	}
}

// UnixConn 包装了原始的tls.Conn，目标是为了能够将连接对端的pid和uid给拿到，并能传递给到grpc的业务功能
type UnixConn struct {
	*tls.Conn
	addr net.Addr
}

func newUnixConn(tconn *tls.Conn, pid, uid int) *UnixConn {
	// 将连接对端的pid和uid编码成地址的Name。
	// 知晓连接的pid，可以帮助Ally简单的处理好rpc接口的访问权限问题。
	uaddr := &net.UnixAddr{Net: "unix", Name: UCred2Addr(pid, uid)}
	return &UnixConn{Conn: tconn, addr: uaddr}
}

func (c *UnixConn) RemoteAddr() net.Addr {
	return c.addr
}
