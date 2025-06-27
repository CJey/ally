//go:build unix

package internal

import (
	"net"
	"os"
	"syscall"
)

// SendFile 将给定*os.File的fd通过unix socket发送出去
func SendFile(via *net.UnixConn, file *os.File) error {
	viaf, err := via.File()
	if err != nil {
		return err
	}
	socket := int(viaf.Fd())
	defer viaf.Close()

	rights := syscall.UnixRights(int(file.Fd()))
	return syscall.Sendmsg(socket, nil, rights, nil, 0)
}

// RecvFile 从给定的unix socket中接收一个fd并转换为*os.File
func RecvFile(via *net.UnixConn) (*os.File, error) {
	viaf, err := via.File()
	if err != nil {
		return nil, err
	}
	socket := int(viaf.Fd())
	defer viaf.Close()

	buf := make([]byte, syscall.CmsgSpace(4))
	if _, _, _, _, err := syscall.Recvmsg(socket, nil, buf, 0); err != nil {
		return nil, err
	}

	if msgs, err := syscall.ParseSocketControlMessage(buf); err != nil {
		return nil, err
	} else if fds, err := syscall.ParseUnixRights(&msgs[0]); err != nil {
		return nil, err
	} else {
		return os.NewFile(uintptr(fds[0]), ""), nil
	}
}
