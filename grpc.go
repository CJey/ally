package ally

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"github.com/cjey/ally/internal"
	"github.com/cjey/ally/internal/sysstat"
	pb "github.com/cjey/ally/proto"
)

const GRPC_MESSAGE_LIMIT = internal.GRPC_MESSAGE_LIMIT

// 经由Ally创建的socket，关闭时须按listen时的参数匹配，Addr()的网络类型与通配地址写法都可能对不上
type listened struct {
	sock    any // net.Listener或net.PacketConn
	network string
	address string
	created time.Time
}

var (
	_Listened = []*listened{}
)

func simInstanceInfo() *pb.InstanceInfo {
	info := &pb.InstanceInfo{
		Id:        ID,
		Pid:       uint64(os.Getpid()),
		Appname:   Appname,
		Family:    "",
		StartTime: StartTime.UnixNano(),
	}
	if stat, err := sysstat.GetStat(os.Getpid()); err == nil {
		info.Cpu, info.Ram = stat.CPU, stat.Memory
	}
	if _Ready.Load() {
		info.Stat = pb.InstanceStat_RUNNING
	} else {
		info.Stat = pb.InstanceStat_PREPARING
	}
	if s := _Settings.Load(); s != nil {
		info.Commit, info.Version = s.commit, s.version
		if s.dynamicHook != nil {
			info.Tasks, info.Description = s.dynamicHook()
		}
	}
	_Mutex.Lock()
	for _, saddr := range _Listened {
		info.Addresses = append(info.Addresses, &pb.Address{
			Network: saddr.network,
			Address: saddr.address,

			Refs:    1,
			Created: saddr.created.UnixNano(),
			Updated: saddr.created.UnixNano(),
		})
	}
	_Mutex.Unlock()
	return info
}

func simAppInfo() *pb.AppInfo {
	inst := simInstanceInfo()
	return &pb.AppInfo{
		Name:      Appname,
		Family:    inst.Family,
		Bin:       os.Args[0],
		Args:      os.Args[1:],
		Main:      inst.Id,
		Stat:      pb.AppStat_RUNNING,
		StartTime: inst.StartTime,
		Instances: []*pb.InstanceInfo{inst},
		Addresses: inst.Addresses,
		Ephemeral: true,
	}
}

// GetAppInfo 获取App的描述信息，包括了全部实例的信息
func GetAppInfo() *pb.AppInfo {
	if _Client == nil { // simulate
		return simAppInfo()
	}

	var rep, err = _Client.GetAppInfo(context.Background(), &pb.GetAppInfoRequest{})
	if err != nil {
		panic(runtimeError(err))
	}
	return rep.GetInfo()
}

// GetInstanceInfo 获取本实例自身的描述信息
func GetInstanceInfo() *pb.InstanceInfo {
	if _Client == nil { // simulate
		return simInstanceInfo()
	}

	var rep, err = _Client.GetInstanceInfo(context.Background(), &pb.GetInstanceInfoRequest{})
	if err != nil {
		panic(runtimeError(err))
	}
	if rep.Code != pb.GetInstanceInfoResponse_EC_OK {
		panic(fmt.Errorf("runtime: rejected by ally, [%s]%s", rep.Code, rep.Error))
	}
	return rep.GetInfo()
}

func suicide() {
	if simReloadApp() != nil {
		os.Exit(0)
	}
}

func simReloadApp() error {
	if p, err := os.FindProcess(os.Getpid()); err != nil {
		return err
	} else {
		return p.Signal(syscall.SIGTERM)
	}
}

// ReloadApp 向Ally主动触发一次reload
func ReloadApp() error {
	if _Client == nil { // simulate
		return simReloadApp()
	}
	var rep, err = _Client.ReloadApp(context.Background(), &pb.ReloadAppRequest{})
	if err != nil {
		if allyGone() {
			return errAllyGone
		}
		panic(runtimeError(err))
	}
	if rep.Code != pb.ReloadAppResponse_EC_OK {
		if rep.Code == pb.ReloadAppResponse_EC_PERMISSION {
			panic(fmt.Errorf("runtime: rejected by ally, [%s]%s", rep.Code, rep.Error))
		}
		return fmt.Errorf("[%s]%s", rep.Code, rep.Error)
	}
	return nil
}

func _ListenSocket(network, address string) (net.Listener, net.PacketConn, error) {
	_Mutex.Lock()
	defer _Mutex.Unlock()

	if _Client == nil { // fallback
		l, c, e := internal.ListenSocket(network, address)
		if e == nil {
			if l != nil {
				var laddr = l.Addr()
				_Listened = append(_Listened, &listened{
					sock: l, network: laddr.Network(), address: laddr.String(),
					created: time.Now(),
				})
			} else {
				var laddr = c.LocalAddr()
				_Listened = append(_Listened, &listened{
					sock: c, network: laddr.Network(), address: laddr.String(),
					created: time.Now(),
				})
			}
		}
		return l, c, e
	}

	// ally按自身的工作目录解析相对路径，须先按本实例的工作目录展开
	if path := internal.SocketPath(network, address); path != "" && !filepath.IsAbs(path) {
		if abs, err := filepath.Abs(path); err != nil {
			return nil, nil, fmt.Errorf("resolve %s address %s failed, %w", network, address, err)
		} else {
			address = abs
		}
	}

	var rep, err = _Client.ListenSocket(context.Background(),
		&pb.ListenSocketRequest{Network: network, Address: address})
	if err != nil {
		if allyGone() {
			return nil, nil, errAllyGone
		}
		panic(runtimeError(err))
	}
	if rep.Code != pb.ListenSocketResponse_EC_OK {
		if rep.Code == pb.ListenSocketResponse_EC_PERMISSION {
			panic(fmt.Errorf("runtime: rejected by ally, [%s]%s", rep.Code, rep.Error))
		}
		return nil, nil, fmt.Errorf("[%s]%s", rep.Code, rep.Error)
	}

	var file *os.File
	select {
	case file = <-_FileChan:
	case <-_Gone:
		return nil, nil, errAllyGone
	}
	var sock any
	var laddr net.Addr
	// 须按network区分：FileListener对unixgram的fd也会成功，返回的却是UnixListener
	switch network {
	case "tcp", "tcp4", "tcp6", "unix", "unixpacket":
		var lsn net.Listener
		if lsn, err = net.FileListener(file); err == nil {
			sock, laddr = lsn, lsn.Addr()
		}
	default:
		var conn net.PacketConn
		if conn, err = net.FilePacketConn(file); err == nil {
			sock, laddr = conn, conn.LocalAddr()
		}
	}
	file.Close()
	if err != nil {
		// ally端已按本实例登记，不撤销则无法再次监听此地址
		_Client.CloseSocket(context.Background(), &pb.CloseSocketRequest{Network: network, Address: address})
		return nil, nil, fmt.Errorf("convert %s %s failed, %w", network, address, err)
	}

	var key = address
	// 与ally端登记规则一致：未指定端口时按实际地址登记
	if _, port, err := net.SplitHostPort(address); err == nil && (port == "" || port == "0") {
		key = laddr.String()
	}
	_Listened = append(_Listened, &listened{
		sock: sock, network: network, address: key,
		created: time.Now(),
	})
	if lsn, ok := sock.(net.Listener); ok {
		return lsn, nil, nil
	}
	return nil, sock.(net.PacketConn), nil
}

func _CloseSocket(sock any) error {
	_Mutex.Lock()
	defer _Mutex.Unlock()

	var idx = slices.IndexFunc(_Listened, func(l *listened) bool { return l.sock == sock })
	if idx < 0 { // 非经由Ally创建，或已关闭
		return nil
	}

	if _Client != nil {
		var saddr = _Listened[idx]
		var rep, err = _Client.CloseSocket(context.Background(),
			&pb.CloseSocketRequest{Network: saddr.network, Address: saddr.address})
		if err != nil {
			if !allyGone() { // ally已退出则登记随之作废，本地移除即可
				panic(runtimeError(err))
			}
		} else if rep.Code != pb.CloseSocketResponse_EC_OK {
			if rep.Code == pb.CloseSocketResponse_EC_PERMISSION {
				panic(fmt.Errorf("runtime: rejected by ally, [%s]%s", rep.Code, rep.Error))
			}
			return fmt.Errorf("[%s]%s", rep.Code, rep.Error)
		}
	}
	_Listened = slices.Delete(_Listened, idx, idx+1)
	return nil
}

func _Heartbeat() {
	if _Client == nil {
		return
	}
	var req = &pb.HeartbeatRequest{Ready: _Ready.Load()}
	if s := _Settings.Load(); s != nil {
		req.Commit, req.Version = s.commit, s.version
		if s.dynamicHook != nil {
			req.Tasks, req.Description = s.dynamicHook()
		}
	}
	if rep, err := _Client.Heartbeat(context.Background(), req); err != nil {
		if allyGone() {
			return
		}
		panic(runtimeError(err))
	} else if rep.Code != pb.HeartbeatResponse_EC_OK {
		panic(fmt.Errorf("runtime: rejected by ally, [%s]%s", rep.Code, rep.Error))
	}
}
