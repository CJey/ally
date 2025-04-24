package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/kballard/go-shellquote"
	"github.com/patrickmn/go-cache"
	"github.com/spf13/cobra"
	"github.com/struCoder/pidusage"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/peer"

	"github.com/cjey/ally/internal"
	pb "github.com/cjey/ally/proto"
	"github.com/cjey/ally/release/app"
)

func init() {
	var cmd = &cobra.Command{
		Use:   "with {appname}",
		Run:   runWith,
		Short: "Run an app with ally",
		Long:  `Run an app with ally`,

		PersistentPreRun: parseEnvConfig,
	}

	cmd.PersistentFlags().String("app-family", "", "the app's family name")
	cmd.PersistentFlags().String("app-user", "", "start with this uid or user name")
	cmd.PersistentFlags().String("app-group", "", "start with this gid or group name")
	cmd.PersistentFlags().String("app-bin", "", "the app's program file path")
	cmd.PersistentFlags().String("app-pwd", "", "the app's working directory")
	cmd.PersistentFlags().String("app-path", "", "the app's PATH")
	cmd.PersistentFlags().StringSlice("app-env", []string{}, "the app's ENV")

	RootCommand.AddCommand(cmd)
}

func runWith(cmd *cobra.Command, args []string) {
	L.Printf("cmd: %s", shellquote.Join(os.Args...))
	if len(args) < 1 {
		Exit(1, "ERROR[Ally]: Appname not given\n")
	}
	var appname = strings.TrimSpace(args[0])
	if !IsGoodName(appname) {
		Exit(1, "ERROR[Ally]: Invalid appname format\n")
	} else if appname == ALL_FAMILY {
		Exit(1, "ERROR[Ally]: Reserved appname[%s]\n", ALL_FAMILY)
	}

	// --app-family
	var app_family string
	if val, _ := cmd.PersistentFlags().GetString("app-family"); val != "" {
		if !IsGoodName(val) {
			Exit(1, "ERROR[Ally]: Invalid family format\n")
		} else if val == ALL_FAMILY {
			Exit(1, "ERROR[Ally]: Reserved family[%s]\n", ALL_FAMILY)
		}
		app_family = val
	}

	var cuser, err = user.Current()
	if err != nil {
		Exit(1, "ERROR[Ally]: Get current user info failed, %s\n", err)
	}
	var uid, _ = strconv.Atoi(cuser.Uid)
	var gid, _ = strconv.Atoi(cuser.Gid)

	// --app-user
	var app_user string
	if val, _ := cmd.PersistentFlags().GetString("app-user"); val != "" {
		u, err := user.LookupId(val)
		if err != nil {
			if u, err = user.Lookup(val); err != nil {
				Exit(1, "ERROR[Ally]: Invalid user id or name\n")
			}
		}
		app_user = val
		uid, _ = strconv.Atoi(u.Uid)
		gid, _ = strconv.Atoi(u.Gid)
	}

	// --app-group
	var app_group string
	if val, _ := cmd.PersistentFlags().GetString("app-group"); val != "" {
		g, err := user.LookupGroupId(val)
		if err != nil {
			if g, err = user.LookupGroup(val); err != nil {
				Exit(1, "ERROR[Ally]: Invalid group id or name\n")
			}
		}
		app_group = val
		gid, _ = strconv.Atoi(g.Gid)
	}

	// --app-bin
	var app_bin = appname
	if val, _ := cmd.PersistentFlags().GetString("app-bin"); val != "" {
		app_bin = val
	}
	if abs, err := LookPath(app_bin); err != nil {
		Exit(2, "ERROR[Ally]: Lookup binary path failed, %s\n", err)
	} else {
		app_bin = abs
	}

	// --app-pwd
	var app_pwd string
	if val, _ := cmd.PersistentFlags().GetString("app-pwd"); val != "" {
		app_pwd = val
	}
	// --app-path
	var app_path string
	if val, _ := cmd.PersistentFlags().GetString("app-path"); val != "" {
		app_path = val
	}
	// --app-env
	var app_envs, _ = cmd.PersistentFlags().GetStringSlice("app-env")

	// prepare sock
	sock_path := filepath.Join(SocksPath, fmt.Sprintf("%d.sock", os.Getpid()))
	os.Remove(sock_path)
	defer os.Remove(sock_path)
	sock_addr, err := net.ResolveUnixAddr("unix", sock_path)
	if err != nil {
		Exit(2, "ERROR[Ally]: Resolve ally's socket address failed, %s\n", err)
	}
	sock_lsn, err := net.ListenUnix("unix", sock_addr)
	if err != nil {
		Exit(2, "ERROR[Ally]: Listen ally's socket failed, %s\n", err)
	}
	L.Printf("env: %s=%s", internal.GetInjectKey(), sock_lsn.Addr().String())
	if err := os.Chmod(sock_path, 0777); err != nil {
		Exit(2, "ERROR[Ally]: Change ally's socket mod failed, %s\n", err)
	}

	if W == nil {
		Exit(3, "ERROR[Ally]: Syslog not work")
	}

	// prepare server
	srv := NewServer(sock_lsn.Addr().String(), CrashWait)
	srv.RegisterApp(appname, app_family, uid, gid, app_user, app_group, app_bin, args[1:], app_pwd, app_path, app_envs)
	srv.L.Printf("cmd: %s %s", srv.bin, shellquote.Join(srv.args...))
	lsn_unix := &internal.UnixListener{UnixListener: sock_lsn}
	lsn_unix.HijackCtrlFd = srv.HijackCtrlFd

	// grpc server
	go func() {
		gsrv := grpc.NewServer(
			grpc.UnaryInterceptor(srv.UnaryInterceptor),
			grpc.ConnectionTimeout(30*time.Second),
			grpc.ConnectionTimeout(3*time.Second),
			grpc.KeepaliveParams(keepalive.ServerParameters{
				MaxConnectionIdle: 1 * time.Hour,
				MaxConnectionAge:  24 * time.Hour,
			}),
		)
		pb.RegisterAllyServer(gsrv, srv)
		pb.RegisterCacheServer(gsrv, srv)
		pb.RegisterAtomicServer(gsrv, srv)
		pb.RegisterLockerServer(gsrv, srv)
		gsrv.Serve(lsn_unix)
		gsrv.Stop()
	}()

	srv.untilBootOK(true)

	// signal
	chsig := make(chan os.Signal, 3)
	signal.Notify(chsig, syscall.SIGUSR2, syscall.SIGINT, syscall.SIGHUP, syscall.SIGTERM)
	for {
		select {
		case <-srv.Exited.Yes:
			srv.L.Printf("exited")
			return
		case sig := <-chsig:
			var str = sig.String()
			switch sig {
			case syscall.SIGUSR2:
				str = "SIGUSR2"
			case syscall.SIGINT:
				str = "SIGINT"
			case syscall.SIGHUP:
				str = "SIGHUP"
			case syscall.SIGTERM:
				str = "SIGTERM"
			}
			if sig == syscall.SIGUSR2 {
				srv.L.Printf("signal %s received, trigger Reload", str)
				srv.untilBootOK(true)
			} else {
				srv.L.Printf("signal %s received, trigger Stop", str)
				srv.Stop() // kill
			}
		}
	}
}

type Address struct {
	network string
	address string
	file    *os.File
	refs    uint64
	created time.Time
	updated time.Time
}

type Instance struct {
	mu sync.RWMutex

	id      uint64
	pid     int
	cmd     *exec.Cmd
	addrs   []*Address
	start   time.Time
	fd_chan *net.UnixConn
	closing bool
	closed  bool
	ready   *Event

	tasks       uint64
	commit      string
	version     string
	description string

	L *log.Logger
}

type Server struct {
	pb.UnimplementedAllyServer
	pb.UnimplementedCacheServer
	pb.UnimplementedAtomicServer
	pb.UnimplementedLockerServer

	mu sync.RWMutex
	// ally grpc server address
	sock string
	// 是否开启故障自动拉起，大于0表示开启，且时间为启动间隔时间
	forever  time.Duration
	last_err error
	booting  bool

	appname string
	family  string
	uid     int
	user    string
	gid     int
	group   string
	bin     string
	args    []string
	pwd     string
	path    string
	envs    []string

	addrs []*Address

	req   uint64
	seq   uint64
	inst  *Instance
	insts map[int]*Instance
	begin time.Time

	L *log.Logger
	// 标记app是否已经做出正式退出的决定(此时app实例可能还有没结束的)
	Giveup *Event
	// 标记所有的实例已经全部终结，也不会再有可能启动新实例了
	Exited *Event

	// cache
	mu_caches sync.RWMutex
	caches    map[string]*cache.Cache

	// atomic
	mu_atomics sync.RWMutex
	atomics    map[string]*int64

	// locker
	mu_lockers sync.RWMutex
	lockers    map[string]*Locker
}

func NewServer(sock string, forever time.Duration) *Server {
	srv := &Server{
		sock:    sock,
		forever: forever,
		insts:   make(map[int]*Instance, 128),
		addrs:   make([]*Address, 0, 16),

		Giveup: NewEvent(),
		Exited: NewEvent(),

		caches:  make(map[string]*cache.Cache),
		atomics: make(map[string]*int64),
		lockers: make(map[string]*Locker),
	}
	srv.background()
	return srv
}

func (s *Server) RegisterApp(appname, family string, uid, gid int, user, group, bin string, args []string, pwd, path string, envs []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.appname, s.family, s.uid, s.user, s.gid, s.group = appname, family, uid, user, gid, group
	s.bin, s.args, s.pwd, s.path, s.envs = bin, args, pwd, path, envs
	s.L = log.New(W, fmt.Sprintf("app[%s]: ", appname), 0)
}

func (s *Server) UnaryInterceptor(ctx context.Context, req any,
	info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (rep any, err error) {
	var session uint64
	switch info.FullMethod {
	case pb.Ally_Heartbeat_FullMethodName:
	case pb.Ally_GetAppInfo_FullMethodName:
	case pb.Ally_GetInstanceInfo_FullMethodName:
	default:
		session = atomic.AddUint64(&s.req, 1)
		ctx = context.WithValue(ctx, "session", session)
	}
	return handler(ctx, req)
}

func (s *Server) HijackCtrlFd(conn *net.UnixConn) {
	pid, _, _ := internal.GetUCred(conn)
	s.mu.RLock()
	var inst = s.insts[pid]
	s.mu.RUnlock()
	if inst == nil {
		s.L.Printf("received ctrl fd connection from pid %d, but not my ally, close it", pid)
		conn.Close()
		return
	}

	if inst.fd_chan != nil {
		inst.L.Printf("attached, replace old connection")
		inst.fd_chan.Close()
	} else {
		inst.L.Printf("attached")
	}
	inst.fd_chan = conn
}

func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// do not start new instance
	s.Giveup.Emit()

	// close all listeners
	for _, addr := range s.addrs {
		addr.file.Close()
	}
	for _, inst := range s.insts {
		for _, addr := range inst.addrs {
			addr.file.Close()
		}
	}

	// stop all instances
	for _, inst := range s.insts {
		inst.closing = true
		inst.cmd.Process.Signal(syscall.SIGTERM)
	}
}

func (s *Server) New() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.run()
}

func (s *Server) run() error {
	if s.Giveup.When != nil {
		return fmt.Errorf("give up")
	}

	cmd := exec.Command(s.bin, s.args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(s.uid), Gid: uint32(s.gid),
		},
	}
	if s.pwd != "" {
		cmd.Dir = s.pwd
	}
	cmd.Env = append(
		os.Environ(),
		fmt.Sprintf("%s=%s", internal.GetInjectKey(), s.sock),
	)
	if s.path != "" {
		cmd.Env = append(cmd.Env, "PATH="+s.path)
	}
	cmd.Env = append(cmd.Env, s.envs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		s.last_err = err
		return err
	}
	s.last_err = nil

	s.seq++
	s.inst = &Instance{
		id:    s.seq,
		pid:   cmd.Process.Pid,
		cmd:   cmd,
		start: time.Now(),
		ready: NewEvent(),
	}
	s.inst.L = log.New(W, fmt.Sprintf("app[%s][%d@%d]: ", s.appname, s.inst.id, s.inst.pid), 0)
	if s.inst.id == 1 {
		s.begin = s.inst.start
	}
	s.insts[s.inst.pid] = s.inst
	s.L.Printf("new main [%d@%d] started", s.inst.id, s.inst.pid)

	go s.waitInstance(s.inst)

	return nil
}

func (s *Server) background() {
	go func() {
		for {
			select {
			case <-s.Giveup.Yes:
				return
			case <-time.After(time.Second):
			}

			s.mu.Lock()

			// revoke address
			revoked := 0
			for i, addr := range s.addrs {
				if addr.refs > 0 {
					continue
				}
				if time.Since(addr.updated) > 5*time.Minute && time.Since(s.inst.start) > 5*time.Minute {
					// inactive addr, auto revoke
					addr.file.Close()
					s.addrs = append(s.addrs[:i-revoked], s.addrs[i-revoked+1:]...)
					revoked++
					s.L.Printf("revoked %s %s", addr.network, addr.address)
				}
			}

			if s.inst != nil && s.inst.ready.When == nil {
				// 主实例超过3s还没有创建好fd_chan，肯定没有使用ally
				if s.inst.fd_chan == nil && time.Since(s.inst.start) > 3*time.Second {
					s.inst.L.Printf("ready after %s, bcz without ally", time.Since(s.inst.start).Truncate(1*time.Millisecond))
					s.inst.ready.Emit()
					s.killSeniors(s.inst.id)
				}
			}

			s.mu.Unlock()
		}
	}()
}

// 只可以给自己的先辈发送终止信号
func (s *Server) killSeniors(who uint64) {
	for _, inst := range s.insts {
		if inst.id >= who {
			continue
		}

		inst.closing = true
		inst.cmd.Process.Signal(syscall.SIGTERM)
		s.L.Printf("send SIGTERM to [%d@%d]", inst.id, inst.pid)
	}
}

func (s *Server) exit() {
	for _, addr := range s.addrs {
		addr.file.Close()
	}
	s.Exited.Emit()
}

func (s *Server) untilBootOK(runfirst bool) {
	go func() {
		// singleton
		s.mu.Lock()
		if s.booting {
			s.mu.Unlock()
			return
		} else {
			s.booting = true
			s.mu.Unlock()
		}
		defer func() {
			s.mu.Lock()
			s.booting = false
			s.mu.Unlock()
		}()

		for {
			if runfirst {
				if err := s.New(); err == nil {
					return
				} else if s.forever == 0 {
					s.mu.Lock()
					if len(s.insts) == 0 {
						s.L.Printf("exit, bcz start new main instance failed, %s", err)
						s.Giveup.Emit()
						s.exit()
					} else if s.inst == nil {
						s.L.Printf("giveup, bcz start new main instance failed, %s", err)
						s.Giveup.Emit()
					} else { // main存活，必然是reload
						s.L.Printf("ooops, reload failed, %s", err)
					}
					s.mu.Unlock()
					return
				}
				s.L.Printf("wait %s for retry", s.forever)
			}
			runfirst = true

			select {
			case <-s.Giveup.Yes:
				s.mu.Lock()
				if len(s.insts) == 0 {
					s.exit()
					s.L.Printf("exit, bcz all app instances exited")
				}
				s.mu.Unlock()
				return
			case <-time.After(s.forever):
			}
		}
	}()
}

func (s *Server) waitInstance(inst *Instance) {
	err := inst.cmd.Wait()
	if err == nil {
		inst.L.Printf("exited")
	} else {
		inst.L.Printf("exited, %s", err)
	}
	inst.closed = true
	// instance stopped, should release resource

	s.mu_lockers.Lock()
	for _, locker := range s.lockers {
		locker.mu.Lock()
		s.lockerCancel(locker, inst)
		locker.mu.Unlock()
	}
	s.mu_lockers.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.insts, inst.pid)
	for _, addr := range inst.addrs {
		addr.refs--
		addr.updated = time.Now()
	}
	if inst.fd_chan != nil {
		inst.fd_chan.Close()
	}

	if inst == s.inst { // 最新实例
		s.last_err = err
		s.inst = nil
		if err == nil {
			s.L.Printf("give up, bcz main app[%d@%d] exited", inst.id, inst.pid)
			s.Giveup.Emit() // 常规退出
		} else if s.forever == 0 {
			// logging
			s.L.Printf("give up, bcz main app[%d@%d] crashed, and crash wait disabled", inst.id, inst.pid)
			s.Giveup.Emit() // 异常退出，并不要求自动重启
		} else if s.Giveup.When == nil {
			// 异常退出，做自动重启
			if time.Since(inst.start) > s.forever {
				s.L.Printf("main app[%d@%d] crashed, auto reload now", inst.id, inst.pid)
				s.untilBootOK(true)
			} else {
				s.L.Printf("main app[%d@%d] crashed fast, auto reload after %s", inst.id, inst.pid, s.forever)
				s.untilBootOK(false)
			}
		}
	}

	// 最后一个实例退出，且不再要求启用新实例
	if s.Giveup.When != nil && len(s.insts) == 0 {
		s.exit()
		s.L.Printf("exit, bcz all app instances exited")
	}
}

func (s *Server) who(ctx context.Context) int {
	if p, ok := peer.FromContext(ctx); ok {
		pid, _ := internal.Addr2UCred(p.Addr.String())
		return pid
	}
	return 0
}

func (s *Server) ListenSocket(ctx context.Context, req *pb.ListenSocketRequest) (*pb.ListenSocketResponse, error) {
	var pid = s.who(ctx)
	var rep = &pb.ListenSocketResponse{}

	s.mu.Lock()
	defer s.mu.Unlock()

	// perm
	var inst = s.insts[pid]
	if inst == nil {
		rep.Code = pb.ListenSocketResponse_EC_PERMISSION
		rep.Error = "you are not my ally"
		return rep, nil
	}

	reused := false
	session := ctx.Value("session").(uint64)
	inst.L.Printf("rpc[%d] ListenSocket(%s, %s) ...", session, req.Network, req.Address)
	defer func() {
		if rep.Code == 0 {
			if reused {
				inst.L.Printf("rpc[%d] ListenSocket(%s, %s) === %s, Reused", session, req.Network, req.Address, rep.Code)
			} else {
				inst.L.Printf("rpc[%d] ListenSocket(%s, %s) === %s, BrandNew", session, req.Network, req.Address, rep.Code)
			}
		} else {
			inst.L.Printf("rpc[%d] ListenSocket(%s, %s) === %s, %s", session, req.Network, req.Address, rep.Code, rep.Error)
		}
	}()

	if inst.fd_chan == nil {
		rep.Code = pb.ListenSocketResponse_EC_PERMISSION
		rep.Error = "fd channel not ready"
		return rep, nil
	}

	// addr
	if _, err := internal.ResolveAddress(req.Network, req.Address); err != nil {
		rep.Code = pb.ListenSocketResponse_EC_BAD_ADDRESS
		rep.Error = err.Error()
		return rep, nil
	}

	// duplicate listen
	for _, addr := range inst.addrs {
		if addr.network != req.Network || addr.address != req.Address {
			continue
		}
		rep.Code = pb.ListenSocketResponse_EC_LISTEN
		rep.Error = "already listened by yourself"
		return rep, nil
	}

	// app closing
	if s.Giveup.When != nil {
		rep.Code = pb.ListenSocketResponse_EC_PERMISSION
		rep.Error = "app closing"
		return rep, nil
	}

	// match
	for _, addr := range s.addrs {
		if addr.network != req.Network || addr.address != req.Address {
			continue
		}
		if err := internal.SendFile(inst.fd_chan, addr.file); err != nil {
			rep.Code = pb.ListenSocketResponse_EC_SEND
			rep.Error = err.Error()
		} else {
			addr.refs++
			addr.updated = time.Now()
			inst.addrs = append(inst.addrs, addr)
			reused = true
		}
		return rep, nil
	}

	// new
	if file, address, err := internal.ListenFile(req.Network, req.Address); err != nil {
		rep.Code = pb.ListenSocketResponse_EC_LISTEN
		rep.Error = err.Error()
		return rep, nil
	} else if err := internal.SendFile(inst.fd_chan, file); err != nil {
		file.Close()
		rep.Code = pb.ListenSocketResponse_EC_SEND
		rep.Error = err.Error()
		return rep, nil
	} else {
		addr := &Address{network: req.Network, address: req.Address, file: file}
		if _, port, err := net.SplitHostPort(req.Address); err == nil && (port == "" || port == "0") {
			addr.address = address
		}
		addr.refs++
		addr.created = time.Now()
		addr.updated = addr.created
		s.addrs = append(s.addrs, addr)
		inst.addrs = append(inst.addrs, addr)
		return rep, nil
	}
}

func (s *Server) CloseSocket(ctx context.Context, req *pb.CloseSocketRequest) (*pb.CloseSocketResponse, error) {
	var pid = s.who(ctx)
	var rep = &pb.CloseSocketResponse{}

	s.mu.Lock()
	defer s.mu.Unlock()

	// perm
	var inst = s.insts[pid]
	if inst == nil {
		rep.Code = pb.CloseSocketResponse_EC_PERMISSION
		return rep, nil
	}

	session := ctx.Value("session").(uint64)
	inst.L.Printf("rpc[%d] CloseSocket(%s, %s) ...", session, req.Network, req.Address)
	defer func() {
		if rep.Code == 0 {
			inst.L.Printf("rpc[%d] CloseSocket(%s, %s) === %s", session, req.Network, req.Address, rep.Code)
		} else {
			inst.L.Printf("rpc[%d] CloseSocket(%s, %s) === %s, %s", session, req.Network, req.Address, rep.Code, rep.Error)
		}
	}()

	// addr
	if _, err := internal.ResolveAddress(req.Network, req.Address); err != nil {
		rep.Code = pb.CloseSocketResponse_EC_BAD_ADDRESS
		rep.Error = err.Error()
		return rep, nil
	}

	// match
	var match *Address
	var match_idx int
	for i, addr := range inst.addrs {
		if addr.network == req.Network && addr.address == req.Address {
			match, match_idx = addr, i
			break
		}
	}
	if match == nil {
		return rep, nil
	}

	// clean
	inst.addrs = append(inst.addrs[:match_idx], inst.addrs[match_idx+1:]...)
	match.refs--
	match.updated = time.Now()
	return rep, nil
}

func (s *Server) getInstanceInfo(inst *Instance) *pb.InstanceInfo {
	info := &pb.InstanceInfo{
		Id:        inst.id,
		Pid:       uint64(inst.pid),
		Appname:   s.appname,
		Family:    s.family,
		StartTime: inst.start.UnixNano(),

		Tasks:       inst.tasks,
		Commit:      inst.commit,
		Version:     inst.version,
		Description: inst.description,

		Addresses: make([]*pb.Address, len(inst.addrs)),
	}
	if stat, err := pidusage.GetStat(inst.pid); err == nil {
		info.Cpu, info.Ram = stat.CPU, stat.Memory
	}
	for i, addr := range inst.addrs {
		info.Addresses[i] = &pb.Address{
			Network: addr.network,
			Address: addr.address,

			Refs:    addr.refs,
			Created: addr.created.UnixNano(),
			Updated: addr.updated.UnixNano(),
		}
	}
	SortAddress(info.Addresses)

	if inst.closing {
		info.Stat = pb.InstanceStat_CLOSING
	} else if inst.ready.When != nil {
		info.Stat = pb.InstanceStat_RUNNING
	} else {
		info.Stat = pb.InstanceStat_PREPARING
	}

	return info
}

func (s *Server) GetInstanceInfo(ctx context.Context, req *pb.GetInstanceInfoRequest) (*pb.GetInstanceInfoResponse, error) {
	var pid = s.who(ctx)
	var rep = &pb.GetInstanceInfoResponse{}

	s.mu.RLock()
	var inst = s.insts[pid]
	s.mu.RUnlock()
	if inst == nil { // 只响应app实例
		rep.Code = pb.GetInstanceInfoResponse_EC_PERMISSION
		return rep, nil
	}

	rep.Info = s.getInstanceInfo(inst)
	return rep, nil
}

func (s *Server) ReloadApp(ctx context.Context, req *pb.ReloadAppRequest) (*pb.ReloadAppResponse, error) {
	var pid = s.who(ctx)
	var rep = &pb.ReloadAppResponse{}

	s.mu.Lock()
	defer s.mu.Unlock()
	var inst = s.insts[pid]
	if inst != s.inst { // 只响应最新app实例
		rep.Code = pb.ReloadAppResponse_EC_PERMISSION
		return rep, nil
	}

	session := ctx.Value("session").(uint64)
	inst.L.Printf("rpc[%d] ReloadApp() ...", session)
	defer func() {
		if rep.Code == 0 {
			inst.L.Printf("rpc[%d] ReloadApp() === %s", session, rep.Code)
		} else {
			inst.L.Printf("rpc[%d] ReloadApp() === %s, %s", session, rep.Code, rep.Error)
		}
	}()

	if err := s.run(); err != nil {
		rep.Code = pb.ReloadAppResponse_EC_RELOAD
		rep.Error = err.Error()
	}
	return rep, nil
}

func (s *Server) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	var pid = s.who(ctx)
	var rep = &pb.HeartbeatResponse{}

	s.mu.Lock()
	var inst = s.insts[pid]
	s.mu.Unlock()
	if inst == nil { // 只响应app实例
		rep.Code = pb.HeartbeatResponse_EC_PERMISSION
		return rep, nil
	}

	if inst.ready.When == nil && req.Ready {
		inst.L.Printf("ready after %s", time.Since(inst.start).Truncate(1*time.Millisecond))
		inst.ready.Emit()
		s.killSeniors(inst.id)
	}
	inst.tasks = req.Tasks
	inst.version = req.Version
	inst.commit = req.Commit
	inst.description = req.Description
	return rep, nil
}

func (s *Server) GetAppInfo(ctx context.Context, req *pb.GetAppInfoRequest) (*pb.GetAppInfoResponse, error) {
	var rep = &pb.GetAppInfoResponse{}
	rep.Pid = uint64(os.Getpid())
	rep.Sock = s.sock
	rep.Commit = app.App.Git.CommitHash
	rep.Version = app.Version
	rep.StartTime = StartTime.UnixNano()
	if stat, err := pidusage.GetStat(os.Getpid()); err == nil {
		rep.Cpu, rep.Ram = stat.CPU, stat.Memory
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	rep.Info = &pb.AppInfo{
		Name:      s.appname,
		Family:    s.family,
		User:      s.user,
		Group:     s.group,
		Bin:       s.bin,
		Pwd:       s.pwd,
		Path:      s.path,
		CrashWait: uint64(s.forever / time.Millisecond),
		Envs:      s.envs,
		Args:      s.args,

		Main:      s.seq,
		StartTime: s.begin.UnixNano(),
		LastError: "",
		Addresses: make([]*pb.Address, len(s.addrs)),

		Ephemeral: true,
	}
	if s.last_err != nil {
		rep.Info.LastError = s.last_err.Error()
	}
	for i, addr := range s.addrs {
		rep.Info.Addresses[i] = &pb.Address{
			Network: addr.network,
			Address: addr.address,

			Refs:    addr.refs,
			Created: addr.created.UnixNano(),
			Updated: addr.updated.UnixNano(),
		}
	}
	SortAddress(rep.Info.Addresses)

	if s.Giveup.When != nil {
		rep.Info.Stat = pb.AppStat_CLOSING
	} else if s.seq == 0 {
		rep.Info.Stat = pb.AppStat_BOOTING1
	} else if s.inst == nil {
		rep.Info.Stat = pb.AppStat_RECOVERING
	} else if s.booting {
		rep.Info.Stat = pb.AppStat_RELOADING1
	} else if s.inst.ready.When != nil {
		if len(s.insts) > 1 {
			rep.Info.Stat = pb.AppStat_RELOADING3
		} else {
			rep.Info.Stat = pb.AppStat_RUNNING
		}
	} else if s.seq == 1 {
		rep.Info.Stat = pb.AppStat_BOOTING2
	} else {
		rep.Info.Stat = pb.AppStat_RELOADING2
	}

	for _, inst := range s.insts {
		rep.Info.Instances = append(rep.Info.Instances, s.getInstanceInfo(inst))
	}
	SortInstanceInfo(rep.Info.Instances)

	return rep, nil
}

func (s *Server) nsCache(ns string) *cache.Cache {
	s.mu_caches.RLock()
	c := s.caches[ns]
	s.mu_caches.RUnlock()
	if c == nil {
		s.mu_caches.Lock()
		if c = s.caches[ns]; c == nil {
			c = cache.New(cache.NoExpiration, 60*time.Second)
			s.caches[ns] = c
		}
		s.mu_caches.Unlock()
	}
	return c
}

func (s *Server) auth(ctx context.Context) (*Instance, error) {
	var pid = s.who(ctx)
	var inst = s.insts[pid]
	if inst == nil {
		return nil, fmt.Errorf("permission denied")
	}
	return inst, nil
}

func (s *Server) CacheSet(ctx context.Context, req *pb.CacheSetRequest) (*pb.CacheSetResponse, error) {
	if _, err := s.auth(ctx); err != nil {
		return nil, err
	}

	s.nsCache(req.Ns).Set(req.Key, req.Val, time.Duration(req.Ttl)*time.Millisecond)
	return new(pb.CacheSetResponse), nil
}

func (s *Server) CacheGet(ctx context.Context, req *pb.CacheGetRequest) (*pb.CacheGetResponse, error) {
	if _, err := s.auth(ctx); err != nil {
		return nil, err
	}

	var rep = new(pb.CacheGetResponse)
	if v, ok := s.nsCache(req.Ns).Get(req.Key); ok {
		rep.Val, rep.Found = v.([]byte), true
	}
	return rep, nil
}

func (s *Server) CacheDel(ctx context.Context, req *pb.CacheDelRequest) (*pb.CacheDelResponse, error) {
	if _, err := s.auth(ctx); err != nil {
		return nil, err
	}

	s.nsCache(req.Ns).Delete(req.Key)
	return new(pb.CacheDelResponse), nil
}

func (s *Server) CacheKeys(ctx context.Context, req *pb.CacheKeysRequest) (*pb.CacheKeysResponse, error) {
	if _, err := s.auth(ctx); err != nil {
		return nil, err
	}

	var rep = new(pb.CacheKeysResponse)
	items := s.nsCache(req.Ns).Items()
	rep.Keys = make([]string, 0, len(items))
	for key := range items {
		rep.Keys = append(rep.Keys, key)
	}
	return rep, nil
}

func (s *Server) CacheCount(ctx context.Context, req *pb.CacheCountRequest) (*pb.CacheCountResponse, error) {
	if _, err := s.auth(ctx); err != nil {
		return nil, err
	}

	var rep = new(pb.CacheCountResponse)
	rep.Count = uint64(s.nsCache(req.Ns).ItemCount())
	return rep, nil
}

func (s *Server) CacheItems(ctx context.Context, req *pb.CacheItemsRequest) (*pb.CacheItemsResponse, error) {
	if _, err := s.auth(ctx); err != nil {
		return nil, err
	}

	var rep = new(pb.CacheItemsResponse)
	items := s.nsCache(req.Ns).Items()
	rep.Items = make(map[string][]byte, len(items))
	for key, item := range items {
		rep.Items[key] = item.Object.([]byte)
	}
	return rep, nil
}

func (s *Server) CacheFlush(ctx context.Context, req *pb.CacheFlushRequest) (*pb.CacheFlushResponse, error) {
	if _, err := s.auth(ctx); err != nil {
		return nil, err
	}

	s.nsCache(req.Ns).Flush()
	return new(pb.CacheFlushResponse), nil
}

func (s *Server) CacheExists(ctx context.Context, req *pb.CacheExistsRequest) (*pb.CacheExistsResponse, error) {
	if _, err := s.auth(ctx); err != nil {
		return nil, err
	}

	var rep = new(pb.CacheExistsResponse)
	if _, ok := s.nsCache(req.Ns).Get(req.Key); ok {
		rep.Found = true
	}
	return rep, nil
}

func (s *Server) atomicNumber(name string) *int64 {
	s.mu_atomics.RLock()
	num := s.atomics[name]
	s.mu_atomics.RUnlock()
	if num == nil {
		s.mu_atomics.Lock()
		if num = s.atomics[name]; num == nil {
			var n int64
			num = &n
			s.atomics[name] = num
		}
		s.mu_atomics.Unlock()
	}
	return num
}
func (s *Server) AtomicAdd(ctx context.Context, req *pb.AtomicAddRequest) (*pb.AtomicAddResponse, error) {
	if _, err := s.auth(ctx); err != nil {
		return nil, err
	}
	num := s.atomicNumber(req.Name)
	return &pb.AtomicAddResponse{
		Val: atomic.AddInt64(num, req.Delta),
	}, nil
}
func (s *Server) AtomicLoad(ctx context.Context, req *pb.AtomicLoadRequest) (*pb.AtomicLoadResponse, error) {
	if _, err := s.auth(ctx); err != nil {
		return nil, err
	}
	num := s.atomicNumber(req.Name)
	return &pb.AtomicLoadResponse{
		Val: atomic.LoadInt64(num),
	}, nil
}
func (s *Server) AtomicStore(ctx context.Context, req *pb.AtomicStoreRequest) (*pb.AtomicStoreResponse, error) {
	if _, err := s.auth(ctx); err != nil {
		return nil, err
	}
	num := s.atomicNumber(req.Name)
	atomic.StoreInt64(num, req.Val)
	return &pb.AtomicStoreResponse{}, nil
}
func (s *Server) AtomicSwap(ctx context.Context, req *pb.AtomicSwapRequest) (*pb.AtomicSwapResponse, error) {
	if _, err := s.auth(ctx); err != nil {
		return nil, err
	}
	num := s.atomicNumber(req.Name)
	return &pb.AtomicSwapResponse{
		Old: atomic.SwapInt64(num, req.New),
	}, nil
}
func (s *Server) AtomicCompareAndSwap(ctx context.Context, req *pb.AtomicCompareAndSwapRequest) (
	*pb.AtomicCompareAndSwapResponse, error) {
	if _, err := s.auth(ctx); err != nil {
		return nil, err
	}
	num := s.atomicNumber(req.Name)
	return &pb.AtomicCompareAndSwapResponse{
		Swapped: atomic.CompareAndSwapInt64(num, req.Old, req.New),
	}, nil
}

type Locker struct {
	mu sync.RWMutex

	name string

	cnt  int64
	head *LockerQueue
	tail *LockerQueue

	rcnt  int64
	rhead *LockerQueue
	rtail *LockerQueue
}

type LockerQueue struct {
	inst *Instance
	stat int // 0: wait, 1: acquired, 2: cancelled
	wait chan struct{}
	next *LockerQueue
}

func (s *Server) lockerCancel(locker *Locker, inst *Instance) {
	if locker.head != nil {
		fake_head := &LockerQueue{next: locker.head}
		pre, cur := fake_head, fake_head.next
		for cur != nil {
			if cur.inst == inst {
				if cur.stat == 0 {
					cur.stat = 2 // cancelled
					close(cur.wait)
				}
				cur = cur.next
				pre.next = cur
			} else {
				pre = cur
				cur = cur.next
			}
		}
		locker.head = fake_head.next
		if locker.head == nil {
			locker.tail = nil
		} else {
			locker.tail = pre
		}
	}
	if locker.rhead != nil {
		fake_rhead := &LockerQueue{next: locker.rhead}
		pre, cur := fake_rhead, fake_rhead.next
		for cur != nil {
			if cur.inst == inst {
				if cur.stat == 0 {
					cur.stat = 2 // cancelled
					close(cur.wait)
				}
				cur = cur.next
				pre.next = cur
			} else {
				pre = cur
				cur = cur.next
			}
		}
		locker.rhead = fake_rhead.next
		if locker.rhead == nil {
			locker.rtail = nil
		} else {
			locker.rtail = pre
		}
	}

	if locker.rhead != nil && locker.rhead.stat == 1 {
		// keep RLock
	} else if locker.head != nil && locker.head.stat == 1 {
		// keep Lock
	} else {
		if locker.rhead != nil { // awake all RLock
			for cur := locker.rhead; cur != nil; cur = cur.next {
				cur.stat = 1
				close(cur.wait)
			}
		} else if locker.head != nil { // awak first Lock
			locker.head.stat = 1
			close(locker.head.wait)
		}
	}
}

func (s *Server) getLocker(ctx context.Context, name string) (*Instance, *Locker, error) {
	inst, err := s.auth(ctx)
	if err != nil {
		return nil, nil, err
	}
	s.mu_lockers.RLock()
	locker := s.lockers[name]
	s.mu_lockers.RUnlock()
	if locker == nil {
		s.mu_lockers.Lock()
		if locker = s.lockers[name]; locker == nil {
			locker = &Locker{name: name}
			s.lockers[name] = locker
		}
		s.mu_lockers.Unlock()
	}
	return inst, locker, nil
}

func (s *Server) LockerLock(ctx context.Context, req *pb.LockerLockRequest) (*pb.LockerLockResponse, error) {
	inst, locker, err := s.getLocker(ctx, req.Name)
	if err != nil {
		return nil, err
	}

	var me = &LockerQueue{inst: inst}

	locker.mu.Lock()
	if inst.closed {
		locker.mu.Unlock()
		return nil, fmt.Errorf("your are dead, locker[%s] pid[%d]",
			locker.name, inst.pid)
	}

	if locker.head == nil && locker.rhead == nil {
		me.stat = 1 // acquired
	} else {
		me.wait = make(chan struct{}) // blocked
	}
	if locker.head == nil {
		locker.head = me
	} else {
		locker.tail.next = me
	}
	locker.tail = me
	locker.cnt++
	locker.mu.Unlock()

	if me.wait != nil {
		<-me.wait
		if me.stat != 1 {
			return nil, fmt.Errorf("ally.Locker.Lock cancelled, locker[%s] pid[%d]",
				locker.name, inst.pid)
		}
	}
	return &pb.LockerLockResponse{}, nil
}

func (s *Server) LockerRLock(ctx context.Context, req *pb.LockerRLockRequest) (*pb.LockerRLockResponse, error) {
	inst, locker, err := s.getLocker(ctx, req.Name)
	if err != nil {
		return nil, err
	}

	var me = &LockerQueue{inst: inst}

	locker.mu.Lock()
	if inst.closed {
		locker.mu.Unlock()
		return nil, fmt.Errorf("your are dead, locker[%s] pid[%d]",
			locker.name, inst.pid)
	}

	if locker.head == nil || locker.head.stat == 0 {
		me.stat = 1 // acquired
	} else {
		me.wait = make(chan struct{}) // blocked
	}
	if locker.rhead == nil {
		locker.rhead = me
	} else {
		locker.rtail.next = me
	}
	locker.rtail = me
	locker.rcnt++
	locker.mu.Unlock()

	if me.wait != nil {
		<-me.wait
		if me.stat != 1 {
			return nil, fmt.Errorf("ally.Locker.RLock cancelled, locker[%s] pid[%d]",
				locker.name, inst.pid)
		}
	}
	return &pb.LockerRLockResponse{}, nil
}

func (s *Server) LockerTryLock(ctx context.Context, req *pb.LockerTryLockRequest) (*pb.LockerTryLockResponse, error) {
	inst, locker, err := s.getLocker(ctx, req.Name)
	if err != nil {
		return nil, err
	}

	var rep = &pb.LockerTryLockResponse{}

	locker.mu.Lock()
	if inst.closed {
		locker.mu.Unlock()
		return nil, fmt.Errorf("your are dead, locker[%s] pid[%d]",
			locker.name, inst.pid)
	}
	var me = &LockerQueue{inst: inst}
	if locker.head == nil && locker.rhead == nil {
		me.stat = 1
		locker.head = me
		locker.tail = me
		locker.cnt++
		rep.Acquired = true
	} else if req.Timeout > 0 {
		me.wait = make(chan struct{})
		if locker.head == nil {
			locker.head = me
		} else {
			locker.tail.next = me
		}
		locker.tail = me
		locker.cnt++
	}
	locker.mu.Unlock()

	if me.wait != nil {
		select {
		case <-me.wait:
		case <-time.After(time.Duration(req.Timeout) * time.Millisecond):
			locker.mu.Lock()
			if me.stat == 0 {
				fake_head := &LockerQueue{next: locker.head}
				pre, cur := fake_head, fake_head.next
				for cur != nil {
					if cur == me {
						pre.next = me.next
						if pre == fake_head {
							locker.head = pre.next
						}
						if locker.head == nil {
							locker.tail = nil
						} else if locker.tail == me {
							locker.tail = pre
						}
						locker.cnt--
						break
					}
					pre = cur
					cur = cur.next
				}
			}
			locker.mu.Unlock()
		}
		rep.Acquired = me.stat == 1
	}

	return rep, nil
}

func (s *Server) LockerTryRLock(ctx context.Context, req *pb.LockerTryRLockRequest) (*pb.LockerTryRLockResponse, error) {
	inst, locker, err := s.getLocker(ctx, req.Name)
	if err != nil {
		return nil, err
	}

	var rep = &pb.LockerTryRLockResponse{}

	locker.mu.Lock()
	if inst.closed {
		locker.mu.Unlock()
		return nil, fmt.Errorf("your are dead, locker[%s] pid[%d]",
			locker.name, inst.pid)
	}
	var me = &LockerQueue{inst: inst}
	if locker.head == nil || locker.head.stat == 0 {
		me.stat = 1
		if locker.rhead == nil {
			locker.rhead = me
		} else {
			locker.rtail.next = me
		}
		locker.rtail = me
		locker.rcnt++
		rep.Acquired = true
	} else if req.Timeout > 0 {
		me.wait = make(chan struct{})
		if locker.rhead == nil {
			locker.rhead = me
		} else {
			locker.rtail.next = me
		}
		locker.rtail = me
		locker.rcnt++
	}
	locker.mu.Unlock()

	if me.wait != nil {
		select {
		case <-me.wait:
		case <-time.After(time.Duration(req.Timeout) * time.Millisecond):
			locker.mu.Lock()
			if me.stat == 0 {
				fake_rhead := &LockerQueue{next: locker.rhead}
				pre, cur := fake_rhead, fake_rhead.next
				for cur != nil {
					if cur == me {
						pre.next = me.next
						if pre == fake_rhead {
							locker.rhead = pre.next
						}
						if locker.rhead == nil {
							locker.rtail = nil
						} else if locker.rtail == me {
							locker.rtail = pre
						}
						locker.rcnt--
						break
					}
					pre = cur
					cur = cur.next
				}
			}
			locker.mu.Unlock()
		}
		rep.Acquired = me.stat == 1
	}

	return rep, nil
}

func (s *Server) LockerUnlock(ctx context.Context, req *pb.LockerUnlockRequest) (*pb.LockerUnlockResponse, error) {
	inst, locker, err := s.getLocker(ctx, req.Name)
	if err != nil {
		return nil, err
	}

	locker.mu.Lock()
	if inst.closed {
		locker.mu.Unlock()
		return nil, fmt.Errorf("your are dead, locker[%s] pid[%d]",
			locker.name, inst.pid)
	}
	if locker.head == nil || locker.head.inst != inst || locker.head.stat != 1 {
		locker.mu.Unlock()
		return nil, fmt.Errorf("invalid ally.Locker.Unlock call, locker[%s] pid[%d]",
			locker.name, inst.pid)
	}

	locker.head = locker.head.next
	locker.cnt--
	if locker.head == nil {
		locker.tail = nil
	}
	if locker.rhead != nil { // awake all RLock first
		for cur := locker.rhead; cur != nil; cur = cur.next {
			cur.stat = 1 // acquired
			close(cur.wait)
		}
	} else if locker.head != nil { // awk first Lock
		locker.head.stat = 1 // acquired
		close(locker.head.wait)
	}
	locker.mu.Unlock()

	if err != nil {
		return nil, err
	}
	return &pb.LockerUnlockResponse{}, nil
}

func (s *Server) LockerRUnlock(ctx context.Context, req *pb.LockerRUnlockRequest) (*pb.LockerRUnlockResponse, error) {
	inst, locker, err := s.getLocker(ctx, req.Name)
	if err != nil {
		return nil, err
	}

	locker.mu.Lock()
	if inst.closed {
		locker.mu.Unlock()
		return nil, fmt.Errorf("your are dead, locker[%s] pid[%d]",
			locker.name, inst.pid)
	}
	var pre, cur *LockerQueue
	if locker.rhead != nil && locker.rhead.stat == 1 {
		fake_rhead := &LockerQueue{next: locker.rhead}
		pre, cur = fake_rhead, fake_rhead.next
		for cur != nil {
			if cur.inst == inst {
				break
			}
			pre = cur
			cur = cur.next
		}
	}
	if cur == nil {
		locker.mu.Unlock()
		return nil, fmt.Errorf("invalid ally.Locker.RUnlock call, locker[%s] pid[%d]",
			locker.name, inst.pid)
	}

	if cur == locker.rhead {
		locker.rhead = cur.next
		if locker.rhead == nil {
			locker.rtail = nil
		}
	} else {
		pre.next = cur.next
		if pre.next == nil {
			locker.rtail = pre
		}
	}
	locker.rcnt--
	if locker.rhead == nil && locker.head != nil { // awake Lock
		locker.head.stat = 1 // acquired
		close(locker.head.wait)
	}
	locker.mu.Unlock()

	if err != nil {
		return nil, err
	}
	return &pb.LockerRUnlockResponse{}, nil
}

func (s *Server) LockerQueues(ctx context.Context, req *pb.LockerQueuesRequest) (*pb.LockerQueuesResponse, error) {
	inst, locker, err := s.getLocker(ctx, req.Name)
	if err != nil {
		return nil, err
	}

	var rep = &pb.LockerQueuesResponse{}

	locker.mu.RLock()
	if inst.closed {
		locker.mu.Unlock()
		return nil, fmt.Errorf("your are dead, locker[%s] pid[%d]",
			locker.name, inst.pid)
	}
	if locker.rhead != nil && locker.rhead.stat != 0 {
		rep.R = locker.rcnt
	} else {
		rep.R = -1 * locker.rcnt
	}
	if locker.head != nil && locker.head.stat != 0 {
		rep.W = locker.cnt
	} else {
		rep.W = -1 * locker.cnt
	}
	locker.mu.RUnlock()
	return rep, nil
}
