/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package hermes

import (
	"os"

	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "hermes",
	Short: "Hermes devops service mesh",
	Long:  `Hermes is a devops service mesh server.  Providing deomcratized RPC across any process in your infrastructure.A longer description that spans multiple lines and likely contains`,
	// Uncomment the following line if your bare application
	// has an action associated with it:
	// Run: func(cmd *cobra.Command, args []string) { },

}

// serverCmd represents the server command
var ServerCmd = &cobra.Command{
	Use:   "server",
	Short: "run the hermes server",
	Long:  `run the hermes server`,
}

// serverCmd represents the server command
var CreateNamedMailboxCmd = &cobra.Command{
	Use:   "create_named_mailbox",
	Short: "create a named mailbox",
	Long:  `create a named mailbox`,
	Args:  cobra.ExactArgs(1),
}

// serverCmd represents the server command
var MailboxInfoCmd = &cobra.Command{
	Use:   "mailbox_info",
	Short: "pass in a mailbox address or a ready key or an admin key and get all the info for that mailbox",
	Long:  `pass in a mailbox address or a ready key or an admin key and get all the info for that mailbox`,
	Args:  cobra.MinimumNArgs(1),
}

var TraceLogging *bool

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := RootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {

	RootCmd.AddCommand(CreateNamedMailboxCmd)
	RootCmd.AddCommand(ServerCmd)
	RootCmd.AddCommand(MailboxInfoCmd)

	TraceLogging = RootCmd.PersistentFlags().Bool("trace", false, "Trace logging")

}
