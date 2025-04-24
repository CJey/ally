package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/kballard/go-shellquote"
	"github.com/spf13/cobra"

	pb "github.com/cjey/ally/proto"
)

func init() {
	var cmd = &cobra.Command{
		Use:   "jlist [name or family]...",
		Run:   runJlist,
		Short: "List apps with ally, output json",
		Long:  `List apps with ally, output json`,

		PersistentPreRun: parseEnvConfig,
	}

	cmd.PersistentFlags().Bool("pretty", false, "pretty json")

	RootCommand.AddCommand(cmd)
}

func runJlist(cmd *cobra.Command, args []string) {
	L.Printf("cmd: %s", shellquote.Join(os.Args...))

	pretty, _ := cmd.PersistentFlags().GetBool("pretty")

	var apps []*pb.GetAppInfoResponse
	if len(args) > 0 {
		_apps := GetAppInfos()
		if len(_apps) == 0 {
			Exit(1, "ERROR[Ally]: App [%s] not found\n", args[0])
		}

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

	var output []byte
	if pretty {
		output, _ = json.MarshalIndent(apps, "", "   ")
	} else {
		output, _ = json.Marshal(apps)
	}
	fmt.Printf("%s", output)
	if IsTerminal {
		fmt.Printf("\n")
	}
}
