// Author: CJey Hou<cjey.hou@gmail.com>
package ally

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"github.com/cjey/ally/internal"
	pb "github.com/cjey/ally/proto"
)

var (
	_Mutex     sync.Mutex
	_Client    pb.AllyClient
	_FDChannel *net.UnixConn
	_FileChan  = make(chan *os.File, 0)
	_Ready     = false
)

var (
	// 如果本服务通过Ally管理程序启动，会被赋值为本实例在Ally中的实例递增ID
	// Ally管理程序的实例递增ID总是从1开始，如果ID值为0，说明本服务未通过Ally管理程序启动
	ID = uint64(0)

	// 如果本服务通过Ally管理程序启动，会被赋值为Ally管理定义的应用名称
	Appname = filepath.Base(os.Args[0])

	// 如果本服务通过Ally管理程序启动，会被赋值为Ally管理拉起本实例时的时间
	StartTime = time.Now()
)

var (
	// [*core*] Commit和Version，需要实例在服务启动时自行设置一次，Ally会在后台定期将此信息汇报给到Ally管理程序
	Commit  string
	Version string

	// [*core*] DynamicHook相对于Commit和Version，允许实例动态的向Ally管理程序报告自身的任务情况已经一些其他的动态信息
	// 这些信息，包括Commit和Version都会在ally命令行中被呈现
	DynamicHook func() (tasks uint64, description string)

	// [*core*] 当本实例每收到一次要求优雅停机的信号时，就会call一次ExitHook，实例需要在其中完成服务优雅停机的过程
	// 如果不配置此Hook，Ally默认直接做Exit(0)处理，否则只要配置了此Hook，Ally会完全把控制权交给实例自身，不做其他多余处理
	ExitHook func()
)

var (
	// Commit、Version、DynamicHook这些信息的上报周期配置，通常保持默认即可
	HeartbeatInterval = 1 * time.Second
)

// [*core*] Ready 用于向Ally通报本实例的准备工作已经全部准备就绪，可以接受并处理业务请求了
func Ready() {
	_Ready = true
	_Heartbeat()
}

func init() {
	seekAlly()
	go watchSignal()
}

func seekAlly() {
	// exchange key info
	addr_grpc := internal.GetInjectValue()
	if addr_grpc == "" {
		return
	}

	// fd channel
	if raddr, err := net.ResolveUnixAddr("unix", addr_grpc); err != nil {
		panic(fmt.Errorf("bad ally address, %w", err))
	} else if conn, err := net.DialUnix("unix", nil, raddr); err != nil {
		panic(fmt.Errorf("dial to ally [%s] failed, %w", addr_grpc, err))
	} else if pid, _, err := internal.GetUCred(conn); err != nil {
		panic(fmt.Errorf("get ally's pid failed, %w", err))
	} else if pid != os.Getppid() {
		panic(fmt.Errorf("fake ally"))
	} else {
		tconn := internal.TLSClientConn(conn)
		if err := internal.DuplexConnCtrlFd(tconn); err != nil {
			panic(fmt.Errorf("open ally fd channel failed, %w", err))
		} else {
			_FDChannel = conn
		}
	}

	// attach to ally
	var conn, err = getGrpcConn(addr_grpc)
	if err != nil {
		panic(fmt.Errorf("dial to ally [%s] failed, %w", addr_grpc, err))
	}
	_Client = pb.NewAllyClient(conn)
	var info = GetInstanceInfo()
	ID, Appname = info.Id, info.Appname
	StartTime = time.Unix(0, info.StartTime)

	_Cache = newAllyCacheBuckets(addr_grpc)
	_Atomic = newAllyAtomicBuckets(addr_grpc)
	_Locker = newAllyLockerBuckets(addr_grpc)

	go recvFd()
	go heartbeat()
}

func getGrpcConn(addr string) (*grpc.ClientConn, error) {
	var ctx, cfunc = context.WithTimeout(context.Background(), 3*time.Second)
	defer cfunc()
	var real_err error
	var conn, err = grpc.DialContext(ctx, addr, grpc.WithBlock(), grpc.WithInsecure(),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (conn net.Conn, err error) {
			defer func() {
				real_err = err
			}()
			if conn, err := net.DialTimeout("unix", addr, 3*time.Second); err != nil {
				return nil, err
			} else if pid, _, err := internal.GetUCred(conn.(*net.UnixConn)); err != nil {
				conn.Close()
				return nil, err
			} else if pid != os.Getppid() {
				conn.Close()
				return nil, fmt.Errorf("fake ally")
			} else {
				tconn := internal.TLSClientConn(conn.(*net.UnixConn))
				if err := internal.DuplexConnDataRpc(tconn); err != nil {
					tconn.Close()
					return nil, err
				} else {
					return tconn, nil
				}
			}
		}),
	)
	if err == nil {
		return conn, nil
	}
	if real_err != nil {
		return nil, real_err
	}
	return nil, err
}

func recvFd() {
	for {
		file, err := internal.RecvFile(_FDChannel)
		if err != nil {
			suicide()
			return
		}
		_FileChan <- file
	}
}

func watchSignal() {
	chsig := make(chan os.Signal, 3)
	signal.Notify(chsig, syscall.SIGUSR2, syscall.SIGINT, syscall.SIGHUP, syscall.SIGTERM)
	for {
		select {
		case sig := <-chsig:
			if sig == syscall.SIGUSR2 {
				ReloadApp()
				continue
			}
			if ExitHook != nil {
				ExitHook()
			} else {
				os.Exit(0)
			}
		}
	}
}

func heartbeat() {
	for {
		_Heartbeat()

		if HeartbeatInterval <= 0 {
			time.Sleep(1 * time.Second)
		} else {
			time.Sleep(HeartbeatInterval)
		}
	}
}

func runtimeError(origin error) error {
	// test ally
	addr_grpc := _FDChannel.RemoteAddr().String()
	raddr, _ := net.ResolveUnixAddr("unix", addr_grpc)
	if conn, err := net.DialUnix("unix", nil, raddr); err != nil {
		return fmt.Errorf("runtime: dial to ally [%s] failed, %w => %s", addr_grpc, err, origin)
	} else if pid, _, err := internal.GetUCred(conn); err != nil {
		return fmt.Errorf("runtime: get ally's pid failed, %w => %s", err, origin)
	} else if pid != os.Getppid() {
		return fmt.Errorf("runtime: fake ally, %w", origin)
	} else {
		conn.Close()
		return fmt.Errorf("runtime: ally ok, network err? %w", origin)
	}
}
