package main

import (
	"os"
	"syscall"
	"time"

	"github.com/kballard/go-shellquote"
	"github.com/spf13/cobra"
)

func init() {
	var cmd = &cobra.Command{
		Use:   "evict {appname}",
		Run:   runEvict,
		Short: "Evict an app",
		Long:  `Evict an app`,

		PersistentPreRun: parseEnvConfig,
	}

	RootCommand.AddCommand(cmd)
}

func runEvict(cmd *cobra.Command, args []string) {
	L.Printf("cmd: %s", shellquote.Join(os.Args...))
	if len(args) < 1 {
		Exit(1, "ERROR[Ally]: Appname not given\n")
	}
	ensureSocksPath()

	var appname = args[0]

	if apps := findAppByName(appname, nil); len(apps) == 0 {
		Exit(1, "ERROR[Ally]: App [%s] not found\n", appname)
	} else {
		for _, app := range apps {
			if app.Pid == 0 {
				continue
			}
			if p, err := os.FindProcess(int(app.Pid)); err == nil {
				if err := p.Signal(syscall.SIGKILL); err != nil {
					Exit(2, "ERROR[Ally]: Send kill to app [%s]'s ally[%d] failed, %s\n",
						app.Info.Name, app.Pid, err)
				}
			}
			for _, inst := range app.Info.Instances {
				if p, err := os.FindProcess(int(inst.Pid)); err == nil {
					if err := p.Signal(syscall.SIGKILL); err != nil {
						Exit(2, "ERROR[Ally]: Send kill to app [%s]'s instance[%d] failed, %s\n",
							app.Info.Name, inst.Pid, err)
					}
				}
			}
			if err := os.Remove(app.Sock); err != nil {
				Exit(2, "ERROR[Ally]: Remove app [%s]'s sock file[%s] failed, %s\n",
					app.Info.Name, app.Sock, err)
			}
		}
	}

	lock := GetFlock()
	if err := lock.Lock(); err != nil {
		Exit(3, "ERROR[Ally]: Get db lock failed, %s\n", err)
	}
	cfgs, err := LoadDB()
	if err != nil {
		Exit(3, "ERROR[Ally]: Load db file failed, %s\n", err)
	}
	for i, cfg := range cfgs {
		if cfg.Name != appname {
			continue
		}
		cfgs = append(cfgs[:i], cfgs[i+1:]...)
		if err := SaveDB(cfgs); err != nil {
			Exit(4, "ERROR[Ally]: Save db file failed, %s\n", err)
		}
		break
	}
	lock.Unlock()

	time.Sleep(RefreshWait)

	listApp(GetAppInfos())
}
