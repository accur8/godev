package main

import (
	"accur8.io/godev/hermes"
	"accur8.io/godev/hermes/model"
	"github.com/spf13/cobra"
)

func init() {
	hermes.ServerCmd.Run = func(cmd *cobra.Command, args []string) {
		hermes.RunServer()
	}
	hermes.CreateNamedMailboxCmd.Run = func(cmd *cobra.Command, args []string) {
		hermes.RunCreateNamedMailbox(model.NewMailboxAddress(args[0]))
	}
	hermes.MailboxInfoCmd.Run = func(cmd *cobra.Command, args []string) {
		hermes.RunMailboxInfoCmd(args)
	}
}

func main() {
	hermes.Execute()
}
