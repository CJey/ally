package main

import (
	"fmt"
	"os"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/kballard/go-shellquote"
	"github.com/spf13/cobra"

	pb "github.com/cjey/ally/proto"
)

func init() {
	var cmd = &cobra.Command{
		Use:     "status {app}",
		Aliases: []string{"s", "st", "stat", "show", "desc"},
		Run:     runStatus,
		Short:   "Check an app's status",
		Long:    `Check an app's status`,

		PersistentPreRun: parseEnvConfig,
	}

	cmd.PersistentFlags().Bool("pid", false, "found app by pid")
	cmd.PersistentFlags().Bool("name", false, "found app by name")
	cmd.PersistentFlags().Bool("family", false, "found app by family")

	RootCommand.AddCommand(cmd)
}

func runStatus(cmd *cobra.Command, args []string) {
	L.Printf("cmd: %s", shellquote.Join(os.Args...))
	if len(args) < 1 {
		Exit(1, "ERROR[Ally]: App not given\n")
	}

	var _, apps, _ = FindApp(cmd, args, false)

	statusApp(apps)
}

func statusApp(apps []*pb.GetAppInfoResponse) {
	if len(apps) > 1 {
		Exit(1, "ERROR[Ally]: Only one app should be matched, but %d\n", len(apps))
	}

	listApp(apps)
	fmt.Printf("+\n")

	var app = apps[0]

	var cfg_db *AppConfig
	if !app.Info.Ephemeral {
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
			if cfg.Name == app.Info.Name {
				cfg_db = cfg
				break
			}
		}
		if cfg_db == nil {
			Exit(3, "ERROR[Ally]: Can not find app [%s]'s config at db\n", app.Info.Name)
		}
	}

	var cfg_run = &AppConfig{
		Name:   app.Info.Name,
		Family: app.Info.Family,
		User:   app.Info.User,
		Group:  app.Info.Group,
		Bin:    app.Info.Bin,
		Pwd:    app.Info.Pwd,
		Path:   app.Info.Path,
		Envs:   app.Info.Envs,
		Args:   app.Info.Args,
	}

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{
		"property", "value",
	})

	if !app.Info.Ephemeral {
		row := table.Row{"Enable", cfg_db.Enable}
		if cfg_db.Enable {
			Cgreen(row[1:])
		} else {
			Cyellow(row[1:])
		}
		t.AppendRow(row)
	} else {
		row := table.Row{"Enable", "N/A"}
		Cgrey(row[1:])
		t.AppendRow(row)
	}
	t.AppendSeparator()

	if app.Pid != 0 {
		addrs := ""
		for i, addr := range app.Info.Addresses {
			if i > 0 {
				addrs += fmt.Sprintf(", ")
			}
			addrs += fmt.Sprintf("%s://%s", addr.Network, addr.Address)
		}
		t.AppendRow(table.Row{"Listen", addrs})
		t.AppendSeparator()

		t.AppendRow(table.Row{"Stdout", fmt.Sprintf("%s/%s.log", LogsPath, app.Info.Name)})
		t.AppendSeparator()
	}

	if app.Pid != 0 && !app.Info.Ephemeral {
		if cfg_run.String() == cfg_db.String() {
			row := table.Row{"Runtime", cfg_run.String()}
			Cgreen(row[1:])
			t.AppendRow(row)
		} else {
			row := table.Row{"Runtime", cfg_run.String()}
			Cyellow(row[1:])
			t.AppendRow(row)
		}
		t.AppendSeparator()

		if cfg_run.String() == cfg_db.String() {
			row := table.Row{"AppConfig", cfg_db.String()}
			Cgrey(row[1:])
			t.AppendRow(row)
		} else {
			row := table.Row{"AppConfig", cfg_db.String()}
			Cyellow(row[1:])
			t.AppendRow(row)
		}
		t.AppendSeparator()
	} else {
		if app.Pid == 0 {
			row := table.Row{"Runtime", "<stopped>"}
			Cgrey(row[1:])
			t.AppendRow(row)
		} else {
			row := table.Row{"Runtime", cfg_run.String()}
			Cgreen(row[1:])
			t.AppendRow(row)
		}
		t.AppendSeparator()

		if app.Info.Ephemeral {
			row := table.Row{"AppConfig", "<ephemeral>"}
			Cgrey(row[1:])
			t.AppendRow(row)
		} else {
			row := table.Row{"AppConfig", cfg_db.String()}
			Cwhite(row[1:])
			t.AppendRow(row)
		}
		t.AppendSeparator()
	}

	t.Render()
}
