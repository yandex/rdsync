package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/yandex/rdsync/internal/app"
)

var infoCmd = &cobra.Command{
	Use:   "info",
	Short: "Print information from DCS",
	Run: func(cmd *cobra.Command, args []string) {
		app, err := app.NewApp(configFile, logLevel)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		os.Exit(app.CliInfo(verbose))
	},
}

func init() {
	rootCmd.AddCommand(infoCmd)
}
