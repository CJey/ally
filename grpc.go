package ally

import (
	"context"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/struCoder/pidusage"

	"github.com/cjey/ally/internal"
	pb "github.com/cjey/ally/proto"
)

type simAddress struct {
	lsn     net.Listener
	conn    net.PacketConn
	network string
	address string
	created time.Time
}

var (
	_SimAddresses = []*simAddress{}
)

func simInstanceInfo() *pb.InstanceInfo {
	info := &pb.InstanceInfo{
		Id:        ID,
		Pid:       uint64(os.Getpid()),
		Appname:   Appname,
		Family:    "",
		StartTime: StartTime.UnixNano(),
		Commit:    Commit,
		Version:   Version,
	}
	if stat, err := pidusage.GetStat(os.Getpid()); err == nil {
		info.Cpu, info.Ram = stat.CPU, stat.Memory
	}
	if _Ready {
		info.Stat = pb.InstanceStat_RUNNING
	} else {
		info.Stat = pb.InstanceStat_PREPARING
	}
	if DynamicHook != nil {
		info.Tasks, info.Description = DynamicHook()
	}
	for _, saddr := range _SimAddresses {
		info.Addresses = append(info.Addresses, &pb.Address{
			Network: saddr.network,
			Address: saddr.address,

			Refs:    1,
			Created: saddr.created.UnixNano(),
			Updated: saddr.created.UnixNano(),
		})
	}
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
		panic(runtimeError(fmt.Errorf("[%s]%s", rep.Code, rep.Error)))
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
		panic(runtimeError(err))
	}
	if rep.Code != pb.ReloadAppResponse_EC_OK {
		if rep.Code == pb.ReloadAppResponse_EC_PERMISSION {
			panic(runtimeError(fmt.Errorf("[%s]%s", rep.Code, rep.Error)))
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
				_SimAddresses = append(_SimAddresses, &simAddress{
					lsn: l, network: laddr.Network(), address: laddr.String(),
					created: time.Now(),
				})
			} else {
				var laddr = c.LocalAddr()
				_SimAddresses = append(_SimAddresses, &simAddress{
					conn: c, network: laddr.Network(), address: laddr.String(),
					created: time.Now(),
				})
			}
		}
		return l, c, e
	}

	var rep, err = _Client.ListenSocket(context.Background(),
		&pb.ListenSocketRequest{Network: network, Address: address})
	if err != nil {
		panic(runtimeError(err))
	}
	if rep.Code != pb.ListenSocketResponse_EC_OK {
		if rep.Code == pb.ListenSocketResponse_EC_PERMISSION {
			panic(runtimeError(fmt.Errorf("[%s]%s", rep.Code, rep.Error)))
		}
		return nil, nil, fmt.Errorf("[%s]%s", rep.Code, rep.Error)
	}

	for {
		file := <-_FileChan
		if lsn, err := net.FileListener(file); err == nil {
			file.Close()
			if l, ok := lsn.(*net.TCPListener); ok {
				return l, nil, nil
			} else if l, ok := lsn.(*net.UnixListener); ok {
				return l, nil, nil
			}
		} else if conn, err := net.FilePacketConn(file); err == nil {
			file.Close()
			if c, ok := conn.(*net.UDPConn); ok {
				return nil, c, nil
			} else if c, ok := conn.(*net.IPConn); ok {
				return nil, c, nil
			} else if c, ok := conn.(*net.UnixConn); ok {
				return nil, c, nil
			}
		} else {
			file.Close()
		}
	}
}

func _CloseSocket(network, address string) error {
	_Mutex.Lock()
	defer _Mutex.Unlock()

	if _Client == nil { // fallback
		for i, saddr := range _SimAddresses {
			if saddr.network == network && saddr.address == address {
				_SimAddresses = append(_SimAddresses[:i], _SimAddresses[i+1:]...)
			}
		}
		return nil
	}

	var rep, err = _Client.CloseSocket(context.Background(),
		&pb.CloseSocketRequest{Network: network, Address: address})
	if err != nil {
		panic(runtimeError(err))
	}
	if rep.Code != pb.CloseSocketResponse_EC_OK {
		if rep.Code == pb.CloseSocketResponse_EC_PERMISSION {
			panic(runtimeError(fmt.Errorf("[%s]%s", rep.Code, rep.Error)))
		}
		return fmt.Errorf("[%s]%s", rep.Code, rep.Error)
	}
	return nil
}

func _Heartbeat() {
	if _Client == nil {
		return
	}
	var req = &pb.HeartbeatRequest{Commit: Commit, Version: Version, Ready: _Ready}
	if DynamicHook != nil {
		req.Tasks, req.Description = DynamicHook()
	}
	if rep, err := _Client.Heartbeat(context.Background(), req); err != nil {
		panic(runtimeError(err))
	} else if rep.Code != pb.HeartbeatResponse_EC_OK {
		panic(runtimeError(fmt.Errorf("[%s]%s", rep.Code, rep.Error)))
	}
}
