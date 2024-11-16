package main

import (
	"fmt"
	"os"

	"accur8.io/godev/a8"
	"accur8.io/godev/devops"
	"accur8.io/godev/log"
	"github.com/spf13/cobra"
)

// localInstallCmd represents the local-install command
var localInstallCmd = &cobra.Command{
	Use:   "local-install",
	Short: "Installs the application locally",
	Run: func(cmd *cobra.Command, args []string) {
		runSubCommand(args, devops.Install)
	},
}

func runSubCommand(args []string, subCommandFn func([]string) error) {
	Bootstrap()
	runSubCommand := func(ctx a8.ContextI) error {
		return subCommandFn(args)
	}
	a8.GlobalApp().SubmitSubProcess("main", runSubCommand)
	a8.GlobalApp().WaitForCompletion()
}

// deployCmd represents the deploy command
var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploys the passed application(s) to their remote user@server",
	Run: func(cmd *cobra.Command, args []string) {
		runSubCommand(args, devops.Deploy)
	},
}

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "a8-main",
	Short: "A brief description of your application",
	Run: func(cmd *cobra.Command, args []string) {
		// This block runs if no subcommands are provided
		fmt.Println("Welcome to the a8 command!  Use --help to see available commands.")
	},
}

var trace bool

func Bootstrap() {
	if trace {
		log.EnableTraceLogging()
	} else {
		log.ErrorLoggingOnly()
	}
}

func main() {

	rootCmd.AddCommand(localInstallCmd)
	rootCmd.AddCommand(deployCmd)

	// Add the --trace flag to the root command
	rootCmd.PersistentFlags().BoolVar(&trace, "trace", false, "Enable trace logging")

	// Execute the root command
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	os.Exit(0)

}
