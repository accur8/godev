package launchy

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"time"

	"accur8.io/godev/a8"
	base "accur8.io/godev/hermes/api"
	"accur8.io/godev/hermes/model"
	launchy_proto "accur8.io/godev/launchy/proto"
	"accur8.io/godev/log"
	"accur8.io/godev/nefario/rpc"
	"github.com/palantir/stacktrace"
	"google.golang.org/protobuf/proto"
)

type ExitCode int

type State struct {
	ProcessState                 launchy_proto.ProcessState
	MostRecentExitCode           int
	RpcRequestCh                 chan proto.Message
	ChildProcess                 *ChildProcess
	CurrentUser                  *user.User
	LaunchyProcessStartedRequest *rpc.ProcessStartedRequest
}

type ChildProcess struct {
	RunningCommand        *exec.Cmd
	ProcessStartedRequest *rpc.ProcessStartedRequest
	ChildCtx              context.Context
	ChildCtxCancelFn      context.CancelFunc
	CmdStdout             *ProcessStreamWriter
	CmdStderr             *ProcessStreamWriter
}

type LaunchyControllerS struct {
	launchyContext         context.Context
	launchyContextCancelFn context.CancelFunc
	launchArgs             *LaunchArgs
	nefarioClient          NefarioServiceClient
	mailbox                base.MailboxI
	processUid             base.ProcessUid
	minionUid              a8.Uid
	state                  *State
}

func NewLaunchyController(
	launchyContext context.Context,
	launchyContextCancelFn context.CancelFunc,
	launchArgs *LaunchArgs,
	nefarioClient NefarioServiceClient,
	mailbox base.MailboxI,
	processUid base.ProcessUid,
) LaunchyController {
	user, err := user.Current()
	if err != nil {
		panic(stacktrace.Propagate(err, "error getting current user"))
	}
	return &LaunchyControllerS{
		launchyContext:         launchyContext,
		launchyContextCancelFn: launchyContextCancelFn,
		launchArgs:             launchArgs,
		nefarioClient:          nefarioClient,
		mailbox:                mailbox,
		processUid:             processUid,
		state: &State{
			ProcessState:       launchy_proto.ProcessState_Stopped,
			MostRecentExitCode: -1,
			RpcRequestCh:       make(chan proto.Message),
			CurrentUser:        user,
		},
	}
}

type LaunchyController interface {
	ProcessUid() model.ProcessUid
	NefarioClient() NefarioServiceClient
	LaunchyContext() context.Context
	CancelLaunchyContext()
	LaunchArgs() *LaunchArgs
	Command() []string
	SubmitLaunchyProcessStart() error
	SubmitChildProcessStart(processUid a8.Uid, childProcess *ChildProcess, launchError error) error
	SubmitChildProcessPing(*ChildProcess) error
	SubmitChildProcessComplete(*RunResult) error
	IsProcessRunning() bool
	State() *State
	StopRunningProcess(signal launchy_proto.StopSignal)

	RpcPing(*launchy_proto.PingRequest) (*launchy_proto.PingResponse, error)
	RpcStartProcess(*launchy_proto.StartProcessRequest) (*launchy_proto.StartProcessResponse, error)
	RpcStopProcess(*launchy_proto.StopProcessRequest) (*launchy_proto.StopProcessResponse, error)
	RpcProcessStatus(*launchy_proto.ProcessStatusRequest) (*launchy_proto.ProcessStatusResponse, error)
	RpcStopLaunchy(*launchy_proto.StopLaunchyRequest) (*launchy_proto.StopLaunchyResponse, error)
}

func (lcs *LaunchyControllerS) Command() []string {
	return lcs.launchArgs.Command
}

func (lcs *LaunchyControllerS) NefarioClient() NefarioServiceClient {
	return lcs.nefarioClient
}

func (lcs *LaunchyControllerS) LaunchyContext() context.Context {
	return lcs.launchyContext
}

func (lcs *LaunchyControllerS) CancelLaunchyContext() {
	lcs.launchyContextCancelFn()
}

func (lcs *LaunchyControllerS) ProcessUid() model.ProcessUid {
	return lcs.processUid
}

func (lcs *LaunchyControllerS) LaunchArgs() *LaunchArgs {
	return lcs.launchArgs
}

func (lcs *LaunchyControllerS) SubmitLaunchyProcessStart() error {

	cwd, err := os.Getwd()
	if err != nil {
		return stacktrace.Propagate(err, "error getting cwd")
	}

	req := &rpc.ProcessStartedRequest{
		ProcessUid:       lcs.ProcessUid().String(),
		ProcessPid:       int32(os.Getpid()),
		MinionUid:        lcs.minionUid,
		StartedAt:        NowTimeStamp(),
		Command:          os.Args,
		Cwd:              cwd,
		Category:         lcs.launchArgs.Category,
		Channels:         []string{},
		Controllable:     true,
		Kind:             "launchy-parent",
		Env:              []string{},
		ExtraDataJsonStr: lcs.launchArgs.ExtraData, // empty Env for now since it makes everything noisey and provides no clear value
	}
	lcs.state.LaunchyProcessStartedRequest = req
	err = lcs.NefarioClient().ProcessStart(lcs.LaunchyContext(), req)
	if err != nil {
		return stacktrace.Propagate(err, "ProcessStart error")
	}

	// submit pings
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				lcs.nefarioClient.ProcessPing(lcs.launchyContext, rpc.ProcessPingReq(&rpc.ProcessPing{ProcessUid: lcs.processUid.String()}))
			case <-lcs.launchyContext.Done():
				return
			}
		}
	}()

	return nil
}

func (lcs *LaunchyControllerS) State() *State {
	return lcs.state
}

func (lcs *LaunchyControllerS) IsProcessRunning() bool {
	return lcs.state.ChildProcess != nil
}

func (lcs *LaunchyControllerS) StopRunningProcess(signal launchy_proto.StopSignal) {
	ChildProcess := lcs.state.ChildProcess
	if ChildProcess == nil {
		log.Debug("no child process ignoring StopRunningProcess")
		return
	}
	cmd := ChildProcess.RunningCommand
	if cmd == nil {
		log.Debug("no RunningCommand on child process ignoring StopRunningProcess")
		return
	}
	switch signal {
	case launchy_proto.StopSignal_Interupt:
		cmd.Process.Signal(os.Interrupt)
	case launchy_proto.StopSignal_Kill:
		cmd.Process.Signal(os.Kill)
	default:
		log.Error("don't know how to handle signal %v", signal)
	}

}

func (lcs *LaunchyControllerS) RpcPing(req *launchy_proto.PingRequest) (*launchy_proto.PingResponse, error) {
	return &launchy_proto.PingResponse{}, nil
}

func (lcs *LaunchyControllerS) RpcStartProcess(req *launchy_proto.StartProcessRequest) (*launchy_proto.StartProcessResponse, error) {
	lcs.state.RpcRequestCh <- req
	return &launchy_proto.StartProcessResponse{}, nil
}

func (lcs *LaunchyControllerS) RpcStopProcess(req *launchy_proto.StopProcessRequest) (*launchy_proto.StopProcessResponse, error) {
	lcs.state.RpcRequestCh <- req
	return &launchy_proto.StopProcessResponse{}, nil
}

func (lcs *LaunchyControllerS) RpcProcessStatus(req *launchy_proto.ProcessStatusRequest) (*launchy_proto.ProcessStatusResponse, error) {
	return &launchy_proto.ProcessStatusResponse{
		State: launchy_proto.ProcessState_name[int32(lcs.state.ProcessState)],
	}, nil
}

func (lcs *LaunchyControllerS) RpcStopLaunchy(req *launchy_proto.StopLaunchyRequest) (*launchy_proto.StopLaunchyResponse, error) {
	lcs.launchyContextCancelFn()
	return &launchy_proto.StopLaunchyResponse{}, nil
}

func (lcs *LaunchyControllerS) SubmitChildProcessStart(processUid a8.Uid, childProcess *ChildProcess, cmdErr error) error {

	launchError := ""
	if cmdErr != nil {
		launchError = cmdErr.Error()
	}

	req := &rpc.ProcessStartedRequest{
		ProcessUid:          processUid,
		ParentProcessRunUid: lcs.ProcessUid().String(),
		MinionUid:           lcs.minionUid,
		StartedAt:           NowTimeStamp(),
		Command:             childProcess.RunningCommand.Args,
		Cwd:                 lcs.State().LaunchyProcessStartedRequest.Cwd,
		Category:            lcs.launchArgs.Category,
		Channels:            []string{"stderr", "stdout"},
		Controllable:        false,
		LaunchError:         launchError,
		Kind:                "launchy-child",
		Env:                 []string{}, // empty Env for now since it makes everything noisey and provides no clear value
		ExtraDataJsonStr:    lcs.launchArgs.ExtraData,
	}
	childProcess.ProcessStartedRequest = req
	lcs.state.ChildProcess = childProcess

	cmd := childProcess.RunningCommand
	if cmd.Process != nil {
		req.ProcessPid = int32(cmd.Process.Pid)
		state := lcs.State()
		state.ProcessState = launchy_proto.ProcessState_Running
		state.ChildProcess = childProcess
	}

	// submit pings
	a8.SubmitGoRoutine(fmt.Sprintf("child process pinger for %s", processUid), func() error {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-childProcess.ChildCtx.Done():
				return nil
			case <-ticker.C:
				lcs.SubmitChildProcessPing(childProcess)
			}
		}
	})

	err := lcs.NefarioClient().ProcessStart(lcs.LaunchyContext(), req)
	if err != nil {
		return stacktrace.Propagate(err, "SubmitChildProcessStart error")
	}
	return nil

}

func (lcs *LaunchyControllerS) SubmitChildProcessPing(childProcess *ChildProcess) error {

	ping := &rpc.ProcessPing{
		ProcessUid:   childProcess.ProcessStartedRequest.ProcessUid,
		ChannelSizes: map[string]uint64{},
	}

	if childProcess.CmdStdout != nil {
		ping.ChannelSizes["stdout"] = childProcess.CmdStdout.ByteCount
	}

	if childProcess.CmdStderr != nil {
		ping.ChannelSizes["stderr"] = childProcess.CmdStderr.ByteCount
	}

	req := rpc.ProcessPingReq(ping)
	err := lcs.NefarioClient().ProcessPing(lcs.LaunchyContext(), req)
	if err != nil {
		return stacktrace.Propagate(err, "SubmitChildProcessPing error")
	}
	return nil
}

func (lcs *LaunchyControllerS) SubmitChildProcessComplete(rr *RunResult) error {

	req := &rpc.ProcessCompletedRequest{
		ProcessUid:  lcs.state.ChildProcess.ProcessStartedRequest.ProcessUid,
		ExitCode:    int32(rr.ExitCode),
		ExitMessage: rr.ExitMessage,
		CompletedAt: NowTimeStamp(),
	}

	lcs.state.ChildProcess = nil
	lcs.state.ProcessState = launchy_proto.ProcessState_Stopped

	err := lcs.NefarioClient().ProcessCompleted(lcs.LaunchyContext(), req)
	if err != nil {
		return stacktrace.Propagate(err, "SubmitChildProcessComplete error")
	}
	return nil

}
