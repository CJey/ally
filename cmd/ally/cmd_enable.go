package main

import (
	"os"

	"github.com/kballard/go-shellquote"
	"github.com/spf13/cobra"
)

func init() {
	var cmd = &cobra.Command{
		Use:   "enable {app}",
		Run:   runEnable,
		Short: "Enable an app that will be start by wakeup",
		Long:  "Enable an app that will be start by wakeup",

		PersistentPreRun: parseEnvConfig,
	}

	cmd.PersistentFlags().Bool("pid", false, "found app by pid")
	cmd.PersistentFlags().Bool("name", false, "found app by name")
	cmd.PersistentFlags().Bool("family", false, "found app by family")

	RootCommand.AddCommand(cmd)
}

func runEnable(cmd *cobra.Command, args []string) {
	L.Printf("cmd: %s", shellquote.Join(os.Args...))
	if len(args) < 1 {
		Exit(1, "ERROR[Ally]: App not given\n")
	}

	var _, apps, _ = FindApp(cmd, args, false)
	if len(apps) > 1 {
		Exit(1, "ERROR[Ally]: Only one app should be matched, but %d\n", len(apps))
	}

	var app = apps[0]
	if app.Info.Ephemeral {
		Exit(2, "ERROR[Ally]: Can not enable ephemeral app[%s]\n", app.Info.Name)
	}

	if !app.Info.Enable {
		lock := GetFlock()
		if err := lock.Lock(); err != nil {
			Exit(3, "ERROR[Ally]: Get db lock failed, %s\n", err)
		}
		cfgs, err := LoadDB()
		if err != nil {
			Exit(3, "ERROR[Ally]: Load db file failed, %s\n", err)
		}

		var found bool
		for _, cfg := range cfgs {
			if cfg.Name == app.Info.Name {
				found = true
				cfg.Enable = true
			}
		}
		if !found {
			Exit(1, "ERROR[Ally]: App [%s] not found\n", app.Info.Name)
		}

		if err := SaveDB(cfgs); err != nil {
			Exit(4, "ERROR[Ally]: Save db file failed, %s\n", err)
		}
		lock.Unlock()
	}

	statusApp(apps)
}
