package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cjey/ally/release/app"
)

func init() {
	var cmd = &cobra.Command{
		Use:   "version",
		Run:   runVersion,
		Short: "Show my version",
		Long:  "Show my version",
	}
	// flags

	RootCommand.AddCommand(cmd)
}

func runVersion(cmd *cobra.Command, args []string) {
	fmt.Printf("%s", app.TraceInfo())
}
