package launchy

import (
	"os"

	"accur8.io/godev/log"
	"github.com/spf13/cobra"
)

var CmdRoot = &cobra.Command{
	Use:   "minion",
	Short: "Minion your friendly agent to help you take over the world one process at a time",
	Long:  `Minion your friendly agent to help you take over the world one process at a time`,
}

var CmdRunChildProcess = &cobra.Command{
	Use:   "run",
	Short: "run a child process",
	Long:  `run a child process`,
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		RunChildProcessMain(cmd, args, CmdRunChildProcessFlags, CmdGlobalFlags, CmdJavaProcessFlags, RunChild)
	},
}

var CmdRunJavaProcess = &cobra.Command{
	Use:   "run-java",
	Short: "run a java process",
	Long:  `run a java process`,
	Args:  cobra.MinimumNArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		RunChildProcessMain(cmd, args, CmdRunChildProcessFlags, CmdGlobalFlags, CmdJavaProcessFlags, RunJava)
	},
}

var CmdUpgradeJavaProcess = &cobra.Command{
	Use:   "upgrade-java",
	Short: "upgrade a java process",
	Long:  `upgrade a java process`,
	Args:  cobra.MinimumNArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		RunChildProcessMain(cmd, args, CmdRunChildProcessFlags, CmdGlobalFlags, CmdJavaProcessFlags, UpgradeJava)
	},
}

// serverCmd represents the server command
var CmdAgent = &cobra.Command{
	Use:   "agent",
	Short: "Run the systemd monitor agent",
	Long:  `Run the systemd monitor agent`,
	Run: func(cmd *cobra.Command, args []string) {
		AgentMain(cmd, args, &CmdGlobalFlags)
	},
}

// serverCmd represents the server command
var CmdInstallAgent = &cobra.Command{
	Use:   "install-agent",
	Short: "Install the agent as a systemd service as a user level unit",
	Long:  `Install the agent as a systemd service as a user level unit`,
	Run: func(cmd *cobra.Command, args []string) {
		InstallAgent(cmd, args, &CmdGlobalFlags)
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func CmdExecute() {
	err := CmdRoot.Execute()
	if err != nil {
		log.Warn(err.Error())
		os.Exit(1)
	}
}

type CmdGlobalFlagsS struct {
	TraceLogging *bool
	Logging      *bool
	Category     *string
	ExtraData    *string
}

type CmdRunChildProcessFlagsS struct {
	NoStart         *bool
	NoExit          *bool
	ConsolePassThru *bool
}

type CmdJavaProcessFlagsS struct {
	LauncherJsonFile *string
	Version          *string
}

var CmdGlobalFlags CmdGlobalFlagsS
var CmdRunChildProcessFlags CmdRunChildProcessFlagsS
var CmdJavaProcessFlags CmdJavaProcessFlagsS

func init() {

	CmdRoot.AddCommand(CmdRunChildProcess)
	CmdRoot.AddCommand(CmdAgent)
	CmdRoot.AddCommand(CmdInstallAgent)
	CmdRoot.AddCommand(CmdRunJavaProcess)

	CmdGlobalFlags.TraceLogging = CmdRoot.PersistentFlags().Bool("trace", false, "Show trace logging")
	CmdGlobalFlags.Logging = CmdRoot.PersistentFlags().Bool("logging", false, "Show logging")
	CmdGlobalFlags.Category = CmdRoot.PersistentFlags().String("category", "", "category inside the namespace")
	CmdGlobalFlags.ExtraData = CmdRoot.PersistentFlags().String("extraData", "{}", "extra json data passed through to the nefario database")

	CmdRunChildProcessFlags.NoStart = CmdRunChildProcess.PersistentFlags().Bool("nostart", false, "Do not start the app, will wait for remote control to start it")
	CmdRunChildProcessFlags.NoExit = CmdRunChildProcess.PersistentFlags().Bool("wait", false, "Do not exit, used when remotely controlling start/stop")
	CmdRunChildProcessFlags.ConsolePassThru = CmdRunChildProcess.PersistentFlags().Bool("consolepassthru", false, "pass through console logging")

	CmdJavaProcessFlags.LauncherJsonFile = CmdRunChildProcess.PersistentFlags().String("launcherjson", "", "launcher json to use")
	CmdJavaProcessFlags.Version = CmdRunChildProcess.PersistentFlags().String("version", "", "version to use default is current | latest | number")

}

// var (
// 	traceLoggingF = flag.Bool("trace", false, "Show trace logging")
// 	loggingF      = flag.Bool("logging", false, "Show logging")
// 	// versionF         = flag.Bool("version", false, "Show version")
// 	// helpF            = flag.Bool("help", false, "Show usage")
// 	extraDataF       = flag.String("extraData", "{}", "extra json data passed through to the nefario database")
// 	noStartF         = flag.Bool("nostart", false, "Do not start the app, will wait for remote control to start it")
// 	noExitF          = flag.Bool("wait", false, "Do not exit, used when remotely controlling start/stop")
// 	categoryF        = flag.String("category", "", "category inside the namespace")
// 	consolePassThruF = flag.Bool("consolepassthru", false, "pass through console logging")
// )
