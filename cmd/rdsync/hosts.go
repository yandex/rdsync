package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/yandex/rdsync/internal/app"
)

var priority int
var dryRun bool
var skipRedisCheck bool

var hostListCmd = &cobra.Command{
	Use:     "host",
	Aliases: []string{"hosts"},
	Short:   "list hosts in cluster",
	Run: func(cmd *cobra.Command, args []string) {
		app, err := app.NewApp(configFile, logLevel)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		os.Exit(app.CliHostList())
	},
}

var hostAddCmd = &cobra.Command{
	Use:   "add",
	Short: "add host to cluster",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		app, err := app.NewApp(configFile, logLevel)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		var priorityVal *int
		cmd.Flags().Visit(func(f *pflag.Flag) {
			switch f.Name {
			case "priority":
				priorityVal = &priority
			}
		})

		os.Exit(app.CliHostAdd(args[0], priorityVal, dryRun, skipRedisCheck))
	},
}

var hostRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "remove host from cluster",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		app, err := app.NewApp(configFile, logLevel)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		os.Exit(app.CliHostRemove(args[0]))
	},
}

func init() {
	hostAddCmd.Flags().IntVar(&priority, "priority", 100, "host priority")
	hostAddCmd.Flags().BoolVar(&skipRedisCheck, "skip-redis-check", false, "do not check redis availability")
	hostAddCmd.Flags().BoolVar(&dryRun, "dry-run", false, "tests suggested changes."+
		" Exits codes:"+
		" 0 - when no changes detected,"+
		" 1 - when some error happened or changes prohibited,"+
		" 2 - when changes detected and some changes will be performed during usual run")
	hostListCmd.AddCommand(hostAddCmd)
	hostListCmd.AddCommand(hostRemoveCmd)
	rootCmd.AddCommand(hostListCmd)
}
