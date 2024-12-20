package launchy

import (
	"context"
	"errors"
	"fmt"
	"syscall"

	"os"
	"os/exec"
	"os/signal"

	"accur8.io/godev/a8"
	"accur8.io/godev/a8nats"
	"accur8.io/godev/hermes/directclient"
	"accur8.io/godev/hermes/model"
	launchy_proto "accur8.io/godev/launchy/proto"
	"accur8.io/godev/log"
	"accur8.io/godev/nefario/rpc"

	"github.com/palantir/stacktrace"
	"github.com/spf13/cobra"
)

var (
	sentMsgCounter = int64(0)
)

// Name is the name of this command.
const AppName string = "minion"

// // Homepage is the URL to the canonical website describing this command.
// const Homepage string = "https://github.com/fizzy33/launchy-go" // TODO fixme

// // Version is the version string that gets overridden at link time for releases.
const Version string = "HEAD"

type Config struct {
	LocalNatsUrl             string         `json:"localNatsUrl"`
	CentralNatsUrl           string         `json:"centralNatsUrl"`
	MinionUid                string         `json:"minionUid"`
	NefarioSubject           a8nats.Subject `json:"nefarioSubject"`
	RemoteLinuxCommandPrefix []string       `json:"remoteLinuxCommandPrefix"`
}

func init() {
	// remove warning so we can keep it in the code and use it when needed
	if false {
		log.Debug("%v", sentMsgCounter)
	}
}

const (
	RunChild = iota
	RunJava
	UpgradeJava
)

func RunChildProcessMain(cmd *cobra.Command, args []string, flags CmdRunChildProcessFlagsS, globalFlags CmdGlobalFlagsS, javaFlags CmdJavaProcessFlagsS, action int) {
	rc, err := RunChildProcessMain0(cmd, args, flags, globalFlags, javaFlags, action)
	if err != nil {
		log.Error("RunChildProcessMain0 error %v", err)
		os.Exit(1)
	}
	os.Exit(rc)
}

func RunChildProcessMain0(cmd *cobra.Command, args []string, flags CmdRunChildProcessFlagsS, globalFlags CmdGlobalFlagsS, javaFlags CmdJavaProcessFlagsS, action int) (int, error) {

	ctx, cancelCtxFn0 := context.WithCancel(context.Background())

	cancelCtxFn := func() {
		log.Debug("canceling main context")
		// debug.PrintStack()
		cancelCtxFn0()
	}
	defer cancelCtxFn()

	launchArgs := &LaunchArgs{
		TraceLogging:    *globalFlags.TraceLogging,
		Logging:         *globalFlags.Logging,
		NoExit:          *flags.NoExit,
		Category:        *globalFlags.Category,
		ConsolePassThru: *flags.ConsolePassThru,
		NoStart:         *flags.NoStart,
		ExtraData:       *globalFlags.ExtraData,
	}

	if !a8.IsValidJson(launchArgs.ExtraData) {
		log.Error("extraData is not valid json -- %s", launchArgs.ExtraData)
		os.Exit(1)
	}

	log.IsTraceEnabled = launchArgs.TraceLogging
	log.IsLoggingEnabled = launchArgs.Logging
	log.InitFileLogging("./logs", "launchy")

	config := a8.ConfigLookup[Config](AppName)
	if config == nil {
		log.Error("unable to load config tried %v", a8.PossibleConfigFiles(AppName))
		os.Exit(1)
	}

	launchArgs.MinionUid = config.MinionUid

	// if flag.NArg() < 1 {
	// 	log.Error("missing command")
	// 	os.Exit(1)
	// }

	log.Debug("LaunchArgs %+v", args)

	if action == RunChild {
		launchArgs.Command = args
	} else if action == RunJava {
		launchArgs.Command = args
		launchArgs0, err := PrepareJavaProcess(action, launchArgs, &CmdJavaProcessFlags)
		launchArgs = launchArgs0
		if err != nil {
			return -1, stacktrace.Propagate(err, "PrepareJavaProcess error")
		}
	} else if action == UpgradeJava {
		_, err := PrepareJavaProcess(action, launchArgs, &CmdJavaProcessFlags)
		if err != nil {
			return -1, stacktrace.Propagate(err, "PrepareJavaProcess error")
		} else {
			return 0, nil
		}
	} else {
		return -1, stacktrace.NewError("unknown action %v", action)
	}

	mailbox, err := directclient.CreateRpcMailbox(
		ctx,
		&directclient.RpcMailboxConfig{
			Url:                  config.CentralNatsUrl,
			UseDirectSendMessage: false,
		},
	)
	if err != nil {
		panic(err)
	}

	natsConfig := a8nats.ConnArgs{
		NatsUrl: config.LocalNatsUrl,
		Name:    launchArgs.Command[0],
	}

	nefarioClient, err := NewNefarioServiceClient(natsConfig, config.NefarioSubject)
	if err != nil {
		panic(err)
	}
	defer nefarioClient.Shutdown()

	launchyController := NewLaunchyController(
		ctx,
		cancelCtxFn,
		launchArgs,
		nefarioClient,
		mailbox,
		model.RandomProcessUid(),
	)

	interuptCh := make(chan os.Signal, 1)
	signal.Notify(interuptCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		defer close(interuptCh)
	outer:
		for {
			select {
			case s := <-interuptCh:
				log.Debug("received signal %+v", s)
				if launchyController.IsProcessRunning() {
					launchyController.StopRunningProcess(1) //StopSignal_Interupt)
				} else {
					cancelCtxFn()
					break outer
				}
			case <-ctx.Done():
				break outer
			}
		}
	}()

	launchyController.SubmitLaunchyProcessStart()

	go func() {
		err := SubmitRpcListenAndServe(launchyController)
		if err != nil {
			err = stacktrace.Propagate(err, "StartAndListen error")
			log.Error("%v", err)
		}
	}()

	err = SubmitInitialRun(launchyController)
	if err != nil {
		err = stacktrace.Propagate(err, "SubmitInitialRun error")
		// log.Error(err.Error())
		return -1, err
	}

	<-ctx.Done()

	req := &rpc.ProcessCompletedRequest{
		ProcessUid:  launchyController.ProcessUid().String(),
		ExitCode:    0,
		ExitMessage: "",
		CompletedAt: NowTimeStamp(),
	}
	err = nefarioClient.ProcessCompleted(context.Background(), req)
	if err != nil {
		log.Error("ProcessCompleted error %v", err)
	}

	return 0, nil

}

func writer(processUid a8.Uid, launchyController LaunchyController, name string, transport rpc.Transport, passThru *os.File) *ProcessStreamWriter {
	if !launchyController.LaunchArgs().ConsolePassThru {
		passThru = nil
	}
	psw := &ProcessStreamWriter{
		ProcessUid:        processUid,
		Name:              name,
		LaunchyController: launchyController,
		PassThru:          passThru,
		ChannelName:       ConsoleChannel,
		Transport:         transport,
	}
	return psw
}

func SubmitInitialRun(
	launchyController LaunchyController,
) error {

	log.Debug("process uid %s", launchyController.ProcessUid())

	state := launchyController.State()

	submitRunCommand := func() {
		go func() {
			RunChildProcess(launchyController)
			if !launchyController.LaunchArgs().NoExit {
				launchyController.CancelLaunchyContext()
			}
		}()
	}

	go func() {
		ctx := launchyController.LaunchyContext()
		ch := state.RpcRequestCh
		autoStart := !launchyController.LaunchArgs().NoStart
		if autoStart {
			submitRunCommand()
		}
		for {
			select {
			case cmd := <-ch:
				switch req := cmd.(type) {
				case *launchy_proto.StartProcessRequest:
					if !launchyController.IsProcessRunning() {
						submitRunCommand()
					} else {
						log.Debug("process already running")
					}
				case *launchy_proto.StopProcessRequest:
					if launchyController.IsProcessRunning() {
						launchyController.StopRunningProcess(req.Signal)
					} else {
						log.Debug("process not running")
					}
				default:
					log.Error("unknown command %+v", req)
				}
			case <-ctx.Done():
				return
				// case <-time.After(100 * time.Millisecond):
				// 	// noop
			}
		}
	}()

	return nil
}

// Run function executes cmd[0] with parameters cmd[1:] and redirects its stdout & stderr to passed
// writers of corresponding parameter names.
func RunChildProcess(launchyController LaunchyController) *RunResult {
	state := launchyController.State()
	commandArgs := launchyController.Command()
	state.ProcessState = launchy_proto.ProcessState_Starting
	childProcessUid := a8.RandomUid()

	cmdStdout := writer(childProcessUid, launchyController, "stdout", rpc.Transport_Transport_stdout, os.Stdout)
	cmdStderr := writer(childProcessUid, launchyController, "stderr", rpc.Transport_Transport_stderr, os.Stderr)

	cmd := exec.Command(commandArgs[0], commandArgs[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, cmdStdout, cmdStderr

	envCopy := os.Environ()
	cmd.Env = append(envCopy, fmt.Sprintf("PROCESS_UID=%s", childProcessUid))

	exitMessage := ""
	exitCode := -1
	err := cmd.Start()

	childCtx, childCtxCancelFn := context.WithCancel(launchyController.LaunchyContext())
	defer childCtxCancelFn()

	childProcess := &ChildProcess{
		RunningCommand:   cmd,
		ChildCtx:         childCtx,
		ChildCtxCancelFn: childCtxCancelFn,
		CmdStdout:        cmdStdout,
		CmdStderr:        cmdStderr,
	}

	launchyController.SubmitChildProcessStart(childProcessUid, childProcess, err)

	err = cmd.Wait()
	if err != nil {
		// Convert *exec.ExitError to just exit code and no error.
		// From our point of view, it's not really an error but a value.
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ProcessState.ExitCode()
			if log.IsTraceEnabled {
				log.Debug("ee = %#v", ee.ProcessState)
			}
		}
		exitMessage = err.Error()
	} else {
		exitCode = 0
	}
	if log.IsTraceEnabled {
		log.Trace("exitCode = %d  exitMessage = %v  err = %+v  err# = %#v", exitCode, exitMessage, err, err)
	}

	runResult := &RunResult{
		ExitCode:    exitCode,
		ExitMessage: exitMessage,
	}

	err = launchyController.SubmitChildProcessComplete(runResult)
	if err != nil {
		err = stacktrace.Propagate(err, "PostCompletion error")
		log.Error(err.Error())
	}

	state.MostRecentExitCode = runResult.ExitCode
	state.ChildProcess = nil
	state.ProcessState = launchy_proto.ProcessState_Stopped

	return runResult

}
