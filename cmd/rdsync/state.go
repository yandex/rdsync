package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/yandex/rdsync/internal/app"
)

var stateCmd = &cobra.Command{
	Use:   "state",
	Short: "Print information from valkey hosts",
	Run: func(cmd *cobra.Command, args []string) {
		app, err := app.NewApp(configFile, logLevel)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		os.Exit(app.CliState(verbose))
	},
}

func init() {
	rootCmd.AddCommand(stateCmd)
}
