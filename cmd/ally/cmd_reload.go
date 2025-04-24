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
		Use:   "reload {app}",
		Run:   runReload,
		Short: "Reload an app with ally",
		Long:  `Reload an app with ally`,

		PersistentPreRun: parseEnvConfig,
	}

	cmd.PersistentFlags().Bool("pid", false, "found app by pid")
	cmd.PersistentFlags().Bool("name", false, "found app by name")
	cmd.PersistentFlags().Bool("family", false, "found app by family")

	RootCommand.AddCommand(cmd)
}

func runReload(cmd *cobra.Command, args []string) {
	L.Printf("cmd: %s", shellquote.Join(os.Args...))
	if len(args) < 1 {
		Exit(1, "ERROR[Ally]: App not given\n")
	}

	var _, apps, _ = FindApp(cmd, args, false)
	for _, app := range apps {
		if app.Pid == 0 {
			if err := StartApp(app); err != nil {
				Exit(2, "ERROR[Ally]: Start app [%s] failed, %s\n", app.Info.Name, err)
			}
		} else {
			if p, err := os.FindProcess(int(app.Pid)); err == nil {
				if err := p.Signal(syscall.SIGUSR2); err != nil {
					Exit(2, "ERROR[Ally]: Send usr2 to app [%s]'s ally[%d] failed, %s\n",
						app.Info.Name, app.Pid, err)
				}
			}
		}
	}

	time.Sleep(RefreshWait)

	// refresh
	_, apps, _ = FindApp(cmd, args, false)
	listApp(apps)
}
