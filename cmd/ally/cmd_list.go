package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/kballard/go-shellquote"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"

	"github.com/cjey/ally/internal"
	pb "github.com/cjey/ally/proto"
)

func init() {
	var cmd = &cobra.Command{
		Use:     "list [name or family]...",
		Aliases: []string{"l", "ls"},
		Run:     runList,
		Short:   "List apps with ally",
		Long:    `List apps with ally`,

		PersistentPreRun: parseEnvConfig,
	}

	RootCommand.AddCommand(cmd)
}

func runList(cmd *cobra.Command, args []string) {
	L.Printf("cmd: %s", shellquote.Join(os.Args...))

	var apps []*pb.GetAppInfoResponse
	if len(args) > 0 {
		var _apps = GetAppInfos()

		var cands []*pb.GetAppInfoResponse
		for _, arg := range args {
			as := findApp(arg, 0, true, _apps)
			if len(as) == 0 {
				Exit(1, "ERROR[Ally]: App [%s] not found\n", arg)
			}
			cands = append(cands, as...)
		}

		for _, app := range _apps {
			for _, cand := range cands {
				if app == cand {
					apps = append(apps, app)
					break
				}
			}
		}
	} else {
		apps = GetAppInfos()
	}

	listApp(apps)
}

func FindApp(cmd *cobra.Command, args []string, emptyok bool) (any string, apps, all []*pb.GetAppInfoResponse) {
	any = args[0]
	var by_pid, _ = cmd.PersistentFlags().GetBool("pid")
	var by_name, _ = cmd.PersistentFlags().GetBool("name")
	var by_family, _ = cmd.PersistentFlags().GetBool("family")

	if any == ALL_FAMILY {
		if !by_pid && !by_name && by_family {
			emptyok = true
			apps = findApp(any, 3, false, all)
		}
	} else {
		if by_pid {
			apps = findApp(any, 1, false, all)
		} else if by_name {
			apps = findApp(any, 2, false, all)
		} else if by_family {
			apps = findApp(any, 3, false, all)
		} else {
			apps = findApp(any, 0, false, all)
		}
	}
	if len(apps) == 0 && !emptyok {
		Exit(1, "ERROR[Ally]: App [%s] not found\n", any)
	}

	return any, apps, all
}

// mode 0:auto, 1:pid, 2:name, 3:family
func findApp(any string, mode int, dupok bool, in []*pb.GetAppInfoResponse) []*pb.GetAppInfoResponse {
	if mode == 1 {
		if pid, err := strconv.Atoi(any); err != nil || pid <= 0 {
			Exit(1, "ERROR[Ally]: Bad app pid [%s]\n", any)
		} else {
			return findAppByPid(uint64(pid), in)
		}
	}
	if mode == 2 {
		if apps := findAppByName(any, in); len(apps) > 1 && !dupok {
			Exit(1, "ERROR[Ally]: App name [%s] confusing among %d apps\n", any, len(apps))
		} else {
			return apps
		}
	}
	if mode == 3 {
		return findAppByFamily(any, in)
	}

	if pid, err := strconv.Atoi(any); err == nil {
		if pid <= 0 {
			Exit(1, "ERROR[Ally]: Bad app pid [%d]\n", pid)
		}
		return findAppByPid(uint64(pid), in)
	}
	if apps := findAppByName(any, in); len(apps) > 0 {
		if len(apps) > 1 && !dupok {
			Exit(1, "ERROR[Ally]: App name [%s] confusing among %d apps\n", any, len(apps))
		}
		return apps
	}
	if apps := findAppByFamily(any, in); len(apps) > 0 {
		return apps
	}
	return nil
}

func findAppByName(name string, apps []*pb.GetAppInfoResponse) []*pb.GetAppInfoResponse {
	if len(apps) == 0 {
		apps = GetAppInfos()
	}
	res := []*pb.GetAppInfoResponse{}
	for _, app := range apps {
		if app.Info.Name == name {
			res = append(res, app)
		}
	}
	return res
}

func findAppByFamily(family string, apps []*pb.GetAppInfoResponse) []*pb.GetAppInfoResponse {
	if len(apps) == 0 {
		apps = GetAppInfos()
	}
	res := []*pb.GetAppInfoResponse{}
	for _, app := range apps {
		if family == ALL_FAMILY || app.Info.Family == family {
			res = append(res, app)
		}
	}
	return res
}

func findAppByPid(pid uint64, apps []*pb.GetAppInfoResponse) []*pb.GetAppInfoResponse {
	if len(apps) == 0 {
		apps = GetAppInfos()
	}
	for _, app := range apps {
		if app.Pid == pid {
			return []*pb.GetAppInfoResponse{app}
		}
		for _, inst := range app.Info.Instances {
			if inst.Pid == pid {
				return []*pb.GetAppInfoResponse{app}
			}
		}
	}
	return nil
}

func listApp(apps []*pb.GetAppInfoResponse) {
	var idx_apps = make(map[string][]*pb.GetAppInfoResponse)
	for _, app := range apps {
		idx_apps[app.Info.Name] = append(idx_apps[app.Info.Name], app)
	}

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{
		"family", "app name", "inst id", "status", "pid", "uptime", "tasks", "cpu", "ram", "version", "commit",
	})

	var families = make(map[string]bool)
	for _, app := range apps {
		name, pid, info := app.Info.Name, app.Pid, app.Info
		families[info.Family] = true

		if len(idx_apps[name]) > 1 {
			name = fmt.Sprintf("%d:%s", pid, name)
		}

		has_main := false
		for _, inst := range info.Instances {
			if inst.Id == info.Main {
				has_main = true
			}
		}

		// app
		if pid == 0 { // not running
			row := table.Row{
				info.Family, Cappname(name, info),
				"-",
				AppStat(pb.AppStat_STOPPED),
				"-", "-", "-", "-", "-", "-",
				"-",
			}
			Cgrey(row[2:])
			t.AppendRow(row)
			t.AppendSeparator()
		} else {
			cpu, ram := Stat(app.Cpu, app.Ram)
			uptime := time.Since(time.Unix(0, app.StartTime)).Truncate(time.Second)

			row := table.Row{
				info.Family, Cappname(name, info),
				"   0",
				AppStat(info.Stat),
				pid, uptime, len(info.Instances), cpu, ram, app.Version,
				ShortCommit(app.Commit),
			}
			if !has_main && info.LastError != "" {
				row = append(row, info.LastError)
			}
			Cgrey(row[2:])
			t.AppendRow(row)
			t.AppendSeparator()
		}

		// main
		for _, inst := range info.Instances {
			if inst.Id != info.Main {
				continue
			}
			cpu, ram := Stat(inst.Cpu, inst.Ram)
			uptime := time.Since(time.Unix(0, inst.StartTime)).Truncate(time.Second)

			row := table.Row{
				info.Family, Cappname(name, info),
				fmt.Sprintf("%s %d", Cappname("->", info), inst.Id),
				InstanceStat(inst.Stat),
				inst.Pid, uptime, inst.Tasks, cpu, ram, inst.Version,
				ShortCommit(inst.Commit),
			}
			if inst.Description != "" {
				row = append(row, inst.Description)
			}
			if len(info.Instances) > 1 {
				Cwhite(row[2:])
			}
			t.AppendRow(row)
		}
		if pid != 0 && !has_main {
			row := table.Row{
				info.Family, Cappname(name, info),
				fmt.Sprintf("%s %d", Cappname("->", info), info.Main),
				"dead",
				"-", "-", "-", "-", "-", "-",
				"-",
			}
			if info.LastError != "" {
				row = append(row, info.LastError)
			}
			t.AppendRow(row)
			t.AppendSeparator()
		}
		// other
		for _, inst := range info.Instances {
			if inst.Id == info.Main {
				continue
			}
			cpu, ram := Stat(inst.Cpu, inst.Ram)
			uptime := time.Since(time.Unix(0, inst.StartTime)).Truncate(time.Second)

			row := table.Row{
				info.Family, Cappname(name, info),
				fmt.Sprintf("   %d", inst.Id),
				InstanceStat(inst.Stat),
				inst.Pid, uptime, inst.Tasks, cpu, ram, inst.Version,
				ShortCommit(inst.Commit),
			}
			if inst.Description != "" {
				row = append(row, inst.Description)
			}
			t.AppendRow(row)
		}
		t.AppendSeparator()
	}

	t.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1, AutoMerge: true, Hidden: len(families) < 2},
		{Number: 2, AutoMerge: true},
	})
	t.Render()
}

func Stat(cpu, ram float64) (string, string) {
	var cpustr, ramstr string
	cpustr = fmt.Sprintf("%.1f", cpu)
	if ram < 1024*1024 {
		ramstr = fmt.Sprintf("%.0fK", ram/1024)
	} else if ram < 1024*1024*1024 {
		ramstr = fmt.Sprintf("%.0fM", ram/1024/1024)
	} else {
		ramstr = fmt.Sprintf("%.1fG", ram/1024/1024/1024)
	}
	return cpustr, ramstr
}

func Cgrey(val []any) {
	if IsTerminal {
		for i := range val {
			val[i] = text.Colors{text.Faint}.Sprint(val[i])
		}
	}
}

func Cwhite(val []any) {
	if IsTerminal {
		for i := range val {
			val[i] = text.Colors{text.Bold, text.FgHiWhite}.Sprint(val[i])
		}
	}
}

func Cyellow(val []any) {
	if IsTerminal {
		for i := range val {
			val[i] = text.Colors{text.Bold, text.FgHiYellow}.Sprint(val[i])
		}
	}
}

func Cgreen(val []any) {
	if IsTerminal {
		for i := range val {
			val[i] = text.Colors{text.Bold, text.FgHiGreen}.Sprint(val[i])
		}
	}
}

func AppStat(stat pb.AppStat_Enum) string {
	switch stat {
	case pb.AppStat_STOPPED:
		return "stopped"
	case pb.AppStat_BOOTING1, pb.AppStat_BOOTING2:
		return "booting"
	case pb.AppStat_CLOSING:
		return "closing"
	case pb.AppStat_RUNNING:
		return "gazing"
	case pb.AppStat_RELOADING1, pb.AppStat_RELOADING2, pb.AppStat_RELOADING3:
		return "reloading"
	case pb.AppStat_RECOVERING:
		return "recovering"
	default:
		return stat.String()
	}
}

func InstanceStat(stat pb.InstanceStat_Enum) string {
	switch stat {
	case pb.InstanceStat_PREPARING:
		return "preparing"
	case pb.InstanceStat_RUNNING:
		return "running"
	case pb.InstanceStat_CLOSING:
		return "closing"
	default:
		return stat.String()
	}
}

func Cappname(name string, info *pb.AppInfo) any {
	if info.Ephemeral {
		if name == info.Name || strings.Index(name, ":") >= 0 {
			name = "* " + name
		}
	}
	if !IsTerminal {
		return name
	}

	var stillok = false
	for _, inst := range info.Instances {
		if inst.Stat == pb.InstanceStat_RUNNING {
			stillok = true
			break
		}
	}

	switch info.Stat {
	case pb.AppStat_STOPPED:
		return name
	case pb.AppStat_BOOTING1, pb.AppStat_BOOTING2:
		return text.Colors{text.Bold, text.FgHiYellow}.Sprint(name)
	case pb.AppStat_RUNNING, pb.AppStat_RELOADING1, pb.AppStat_RELOADING2, pb.AppStat_RELOADING3:
		if stillok {
			return text.Colors{text.Bold, text.FgHiGreen}.Sprint(name)
		} else {
			return text.Colors{text.Bold, text.FgHiYellow}.Sprint(name)
		}
	case pb.AppStat_CLOSING:
		return text.Colors{text.Bold, text.FgHiMagenta}.Sprint(name)
	case pb.AppStat_RECOVERING:
		return text.Colors{text.Bold, text.FgHiRed}.Sprint(name)
	default:
		return name
	}
}

func ShortCommit(c string) string {
	if len(c) > 7 {
		return c[:7]
	}
	return c
}

func GetAppInfos() (apps []*pb.GetAppInfoResponse) {
	files, err := filepath.Glob(fmt.Sprintf("%s/*.sock", SocksPath))
	if err != nil {
		Exit(1, "ERROR[Ally]: Search ally's sock files failed, %s\n", err)
	}

	// dynamic
	for _, file := range files {
		if app, err := GetAllyAppInfo(file); err != nil {
			if !errors.Is(err, ErrNotAlly) {
				L.Printf("access %s failed, %s", file, err)
			}
			continue
		} else {
			app.Info.Ephemeral = true
			apps = append(apps, app)
		}
	}

	lock := GetFlock()
	if err := lock.RLock(); err != nil {
		Exit(3, "ERROR[Ally]: Get db rlock failed, %s\n", err)
	}
	cfgs, err := LoadDB()
	if err != nil {
		Exit(3, "ERROR[Ally]: Load db file failed, %s\n", err)
	}
	lock.Unlock()

	for _, cfg := range cfgs {
		found := false
		for _, app := range apps {
			if cfg.Name == app.Info.Name {
				app.Info.Ephemeral = false
				app.Info.Enable = cfg.Enable
				found = true
			}
		}
		if !found {
			apps = append(apps, ConvertConfigToApp(cfg))
		}
	}

	sort.Slice(apps, func(i, j int) bool {
		// family
		if apps[i].Info.Family < apps[j].Info.Family {
			return true
		}
		if apps[i].Info.Family > apps[j].Info.Family {
			return false
		}

		// static first
		if apps[i].Pid == 0 && apps[j].Pid != 0 {
			return true
		}
		if apps[i].Pid != 0 && apps[j].Pid == 0 {
			return false
		}

		// static, name
		if apps[i].Pid == 0 && apps[j].Pid == 0 {
			return apps[i].Info.Name < apps[j].Info.Name
		}

		// dynamic, start
		if apps[i].StartTime < apps[j].StartTime {
			return true
		}
		if apps[i].StartTime > apps[j].StartTime {
			return false
		}

		return apps[i].Pid < apps[j].Pid
	})

	return apps
}

func GetAllyAppInfo(sock string) (*pb.GetAppInfoResponse, error) {
	conn, err := GetAllyConn(sock)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var ctx, cfunc = context.WithTimeout(context.Background(), 3*time.Second)
	defer cfunc()
	if rep, err := pb.NewAllyClient(conn).GetAppInfo(ctx, &pb.GetAppInfoRequest{}); err != nil {
		return nil, err
	} else {
		return rep, nil
	}
}

func GetAllyConn(sock string) (*grpc.ClientConn, error) {
	var pid_in_file int
	if v := strings.TrimSuffix(filepath.Base(sock), ".sock"); v == sock {
		return nil, fmt.Errorf("bad ally sock file name, %w", ErrNotAlly)
	} else if v, err := strconv.Atoi(v); err != nil || v <= 0 {
		return nil, fmt.Errorf("bad ally sock file name, %w", ErrNotAlly)
	} else {
		pid_in_file = v
	}
	var ctx, cfunc = context.WithTimeout(context.Background(), 3*time.Second)
	defer cfunc()
	var real_err error
	var conn, err = grpc.DialContext(ctx, sock, grpc.WithBlock(), grpc.WithInsecure(), grpc.FailOnNonTempDialError(true),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (conn net.Conn, err error) {
			defer func() {
				real_err = err
			}()

			if conn, err := net.DialTimeout("unix", addr, 3*time.Second); err != nil {
				return nil, err
			} else if pid, _, err := internal.GetUCred(conn.(*net.UnixConn)); err != nil {
				conn.Close()
				return nil, err
			} else if pid != pid_in_file {
				conn.Close()
				return nil, fmt.Errorf("fake ally")
			} else {
				tconn := internal.TLSClientConn(conn.(*net.UnixConn))
				if err := internal.DuplexConnDataRpc(tconn); err != nil {
					return nil, err
				} else {
					return tconn, nil
				}
			}
		}),
	)
	if err != nil {
		if ope, ok := real_err.(*net.OpError); ok {
			if ope.Op == "dial" && strings.Contains(ope.Err.Error(), "connection refused") {
				if finfo, err := os.Lstat(sock); err == nil {
					if time.Since(finfo.ModTime()) > 24*time.Hour {
						if err := os.Remove(sock); err == nil {
							L.Printf("unusable sock auto removed, %s", sock)
						} else {
							L.Printf("auto remove unusable sock failed, %s, %s", sock, err)
						}
					}
				}
			}
		}
		return nil, fmt.Errorf("dial to ally [%s] failed, %s, %w", sock, real_err, ErrNotAlly)
	}
	return conn, nil
}
