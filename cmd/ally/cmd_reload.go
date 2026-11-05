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
		Use:   "reload {app}",
		Run:   runReload,
		Short: "Reload an app with ally",
		Long:  `Reload an app with ally`,

		PersistentPreRun: parseEnvConfig,
	}

	cmd.PersistentFlags().Bool("pid", false, "find app by pid")
	cmd.PersistentFlags().Bool("name", false, "find app by name")
	cmd.PersistentFlags().Bool("family", false, "find app by family")

	RootCommand.AddCommand(cmd)
}

func runReload(cmd *cobra.Command, args []string) {
	L.Printf("cmd: %s", shellquote.Join(os.Args...))
	if len(args) < 1 {
		Exit(1, "ERROR[Ally]: App not given\n")
	}

	var _, apps, _ = FindApp(cmd, args, false)
	var names []string
	for _, app := range apps {
		names = append(names, app.Info.Name)
		if app.Pid == 0 {
			if err := StartApp(app); err != nil {
				Exit(2, "ERROR[Ally]: Start app [%s] failed, %s\n", app.Info.Name, err)
			}
		} else {
			if p, err := os.FindProcess(int(app.Pid)); err == nil {
				if err := p.Signal(syscall.SIGUSR2); err != nil && !errors.Is(err, os.ErrProcessDone) {
					Exit(2, "ERROR[Ally]: Send usr2 to app [%s]'s ally[%d] failed, %s\n",
						app.Info.Name, app.Pid, err)
				}
			}
		}
	}

	time.Sleep(RefreshWait)

	// refresh
	listApp(findAppByNames(names))
}
