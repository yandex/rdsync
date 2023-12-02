package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/yandex/rdsync/internal/app"
)

var maintWait time.Duration

var maintCmd = &cobra.Command{
	Use:     "maintenance",
	Aliases: []string{"maint", "mnt"},
	Short:   "Enables or disables maintenance mode",
	Long:    "When maintenance is enabled RdSync manager/candidates will not perform any actions.",
	Run: func(cmd *cobra.Command, args []string) {
		app, err := app.NewApp(configFile, logLevel)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		os.Exit(app.CliGetMaintenance())
	},
}

var maintOnCmd = &cobra.Command{
	Use:     "on",
	Aliases: []string{"enable"},
	Run: func(cmd *cobra.Command, args []string) {
		app, err := app.NewApp(configFile, logLevel)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		os.Exit(app.CliEnableMaintenance(maintWait))
	},
}

var maintOffCmd = &cobra.Command{
	Use:     "off",
	Aliases: []string{"disable"},
	Run: func(cmd *cobra.Command, args []string) {
		app, err := app.NewApp(configFile, logLevel)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		os.Exit(app.CliDisableMaintenance(maintWait))
	},
}

func init() {
	maintCmd.AddCommand(maintOnCmd)
	maintCmd.AddCommand(maintOffCmd)
	maintCmd.PersistentFlags().DurationVarP(&maintWait, "wait", "w", 30*time.Second,
		"how long to wait for maintenance activation, 0s to return immediately")
	rootCmd.AddCommand(maintCmd)
}
