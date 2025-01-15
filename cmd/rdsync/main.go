package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/yandex/rdsync/internal/app"
)

var configFile string
var logLevel string
var verbose bool

var rootCmd = &cobra.Command{
	Use:   "rdsync",
	Short: "Rdsync is a Valkey HA cluster coordination tool",
	Long:  `Running without additional arguments will start rdsync service for current node.`,
	Run: func(cmd *cobra.Command, args []string) {
		app, err := app.NewApp(configFile, "")
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		os.Exit(app.Run())
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "/etc/rdsync.yaml", "config file")
	rootCmd.PersistentFlags().StringVarP(&logLevel, "loglevel", "l", "Warn", "logging level (Debug|Info|Warn|Error)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
