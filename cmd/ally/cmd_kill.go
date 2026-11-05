package main

import (
	"errors"
	"os"
	"syscall"
	"time"

	"github.com/kballard/go-shellquote"
	"github.com/spf13/cobra"
)

func init() {
	var cmd = &cobra.Command{
		Use:   "kill {app}",
		Run:   runKill,
		Short: "Kill an app",
		Long:  `Kill an app`,

		PersistentPreRun: parseEnvConfig,
	}

	cmd.PersistentFlags().Bool("pid", false, "find app by pid")
	cmd.PersistentFlags().Bool("name", false, "find app by name")
	cmd.PersistentFlags().Bool("family", false, "find app by family")

	RootCommand.AddCommand(cmd)
}

func runKill(cmd *cobra.Command, args []string) {
	L.Printf("cmd: %s", shellquote.Join(os.Args...))
	if len(args) < 1 {
		Exit(1, "ERROR[Ally]: App not given\n")
	}

	var _, apps, _ = FindApp(cmd, args, false)
	for _, app := range apps {
		if app.Pid == 0 {
			continue
		}
		if p, err := os.FindProcess(int(app.Pid)); err == nil {
			if err := p.Signal(syscall.SIGKILL); err != nil && !errors.Is(err, os.ErrProcessDone) {
				Exit(2, "ERROR[Ally]: Send kill to app [%s]'s ally[%d] failed, %s\n",
					app.Info.Name, app.Pid, err)
			}
		}
		for _, inst := range app.Info.Instances {
			if p, err := os.FindProcess(int(inst.Pid)); err == nil {
				if err := p.Signal(syscall.SIGKILL); err != nil && !errors.Is(err, os.ErrProcessDone) {
					Exit(2, "ERROR[Ally]: Send kill to app [%s]'s instance[%d] failed, %s\n",
						app.Info.Name, inst.Pid, err)
				}
			}
		}
		if err := os.Remove(app.Sock); err != nil && !errors.Is(err, os.ErrNotExist) {
			Exit(2, "ERROR[Ally]: Remove app [%s]'s sock file[%s] failed, %s\n",
				app.Info.Name, app.Sock, err)
		}
	}

	time.Sleep(RefreshWait)

	// refresh
	_, apps, all := FindApp(cmd, args, true)
	if len(apps) > 0 {
		listApp(apps)
	} else {
		listApp(all)
	}
}
