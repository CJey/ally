package main

import (
	"errors"
	"os"
	"syscall"
	"time"

	"github.com/kballard/go-shellquote"
	"github.com/spf13/cobra"

	pb "github.com/cjey/ally/proto"
)

func init() {
	var cmd = &cobra.Command{
		Use:   "restart {app}",
		Run:   runRestart,
		Short: "Restart an app with ally",
		Long:  `Restart an app with ally`,

		PersistentPreRun: parseEnvConfig,
	}

	cmd.PersistentFlags().Bool("shutup", false, "do restart")
	cmd.PersistentFlags().Bool("pid", false, "find app by pid")
	cmd.PersistentFlags().Bool("name", false, "find app by name")
	cmd.PersistentFlags().Bool("family", false, "find app by family")

	RootCommand.AddCommand(cmd)
}

// family, name, pid
func runRestart(cmd *cobra.Command, args []string) {
	L.Printf("cmd: %s", shellquote.Join(os.Args...))
	if len(args) < 1 {
		Exit(1, "ERROR[Ally]: App not given\n")
	}
	ensureLogsPath()

	shutup, _ := cmd.PersistentFlags().GetBool("shutup")

	var _, apps, all = FindApp(cmd, args, false)
	var names []string
	var idx_all = make(map[string][]*pb.GetAppInfoResponse)
	for _, app := range all {
		name := app.Info.Name
		idx_all[name] = append(idx_all[name], app)
	}
	for _, app := range apps {
		name := app.Info.Name
		if len(idx_all[name]) > 1 {
			Exit(2, "ERROR[Ally]: App name [%s] confusing among %d apps\n", name, len(idx_all[name]))
		}
		names = append(names, name)
	}

	if !shutup {
		Exit(1, "WARN[Ally]: Why not reload?\n")
	}

	// stop & wait
outer:
	for _, app := range apps {
		if app.Pid == 0 {
			continue
		}
		if p, err := os.FindProcess(int(app.Pid)); err == nil {
			if err = p.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
				Exit(2, "ERROR[Ally]: Send term to app [%s]'s ally[%d] failed, %s\n",
					app.Info.Name, app.Pid, err)
			}
		}

		for range 10 {
			time.Sleep(10 * time.Millisecond)
			if _, err := GetAllyAppInfo(app.Sock); err != nil {
				continue outer
			}
		}
		for range 100 {
			time.Sleep(100 * time.Millisecond)
			if _, err := GetAllyAppInfo(app.Sock); err != nil {
				continue outer
			}
		}
		for {
			time.Sleep(1000 * time.Millisecond)
			if _, err := GetAllyAppInfo(app.Sock); err != nil {
				continue outer
			}
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

	// 已持久化的按db配置重启，未持久化的沿用运行时参数
	for _, app := range apps {
		for _, cfg := range cfgs {
			if app.Info.Name == cfg.Name {
				app = ConvertConfigToApp(cfg)
				break
			}
		}
		if err := StartApp(app); err != nil {
			Exit(3, "ERROR[Ally]: Start app [%s] failed, %s\n", app.Info.Name, err)
		}
	}

	time.Sleep(RefreshWait)

	listApp(findAppByNames(names))
}
