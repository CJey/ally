package main

import (
	"os"
	"time"

	"github.com/kballard/go-shellquote"
	"github.com/spf13/cobra"

	pb "github.com/cjey/ally/proto"
)

func init() {
	var cmd = &cobra.Command{
		Use:   "wakeup",
		Run:   runWakeup,
		Short: "Start all enabled apps",
		Long:  "Start all enabled apps",

		PersistentPreRun: parseEnvConfig,
	}

	RootCommand.AddCommand(cmd)
}

func runWakeup(cmd *cobra.Command, args []string) {
	L.Printf("cmd: %s", shellquote.Join(os.Args...))
	ensureLogsPath()

	var all = GetAppInfos()
	var stopped []*pb.GetAppInfoResponse
	var idx_all = make(map[string][]*pb.GetAppInfoResponse)
	for _, app := range all {
		name := app.Info.Name
		idx_all[name] = append(idx_all[name], app)
	}

	var ignored = make(map[string]bool)
	for _, app := range all {
		if !app.Info.Enable {
			continue
		}
		var name = app.Info.Name
		if ignored[name] {
			continue
		}
		if len(idx_all[name]) > 1 {
			ignored[name] = true
			L.Printf("WARN[Ally]: App name [%s] confusing among %d apps, ignore\n", name, len(idx_all[name]))
		} else if app.Pid == 0 {
			stopped = append(stopped, app)
		}
	}

	for _, app := range stopped {
		if err := StartApp(app); err != nil {
			L.Printf("WARN[Ally]: Start app [%s] failed, ignore, %s\n", app.Info.Name, err)
		} else {
			L.Printf("app[%s]: awake now", app.Info.Name)
		}
	}

	if len(stopped) > 0 {
		time.Sleep(RefreshWait)
	}

	listApp(GetAppInfos())
}
