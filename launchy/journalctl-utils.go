package launchy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"accur8.io/godev/a8"
	"accur8.io/godev/hermes/api"
	"accur8.io/godev/hermes/model"
	"accur8.io/godev/log"
	"accur8.io/godev/nefario/rpc"
	"github.com/palantir/stacktrace"
)

type CaptureWindow struct {
	MaxBytes int
	MaxTime  time.Duration
}

type ProcessRunSlurp struct {
	Service      *SystemdService
	ShutdownOnce sync.Once
	Services     *AgentServices
	SlurpChan    *SlurpChan
	Ctx          a8.ContextI
	ProcessUid   api.ProcessUid
	Sequence     uint64
	Running      bool
}

type SlurpChan struct {
	Chan chan *rpc.Buffer
	Size uint64
	Name api.ChannelName
}

type JournalCtlMessage struct {
	CodeFunc        string          `json:"CODE_FUNC"`
	SyslogId        string          `json:"SYSLOG_IDENTIFIER"`
	Message         json.RawMessage `json:"MESSAGE"`
	SystemdUserUnit string          `json:"_SYSTEMD_USER_UNIT"`
	UserUnit        string          `json:"USER_UNIT"`
	JobType         string          `json:"JOB_TYPE"`
	JobResult       string          `json:"JOB_RESULT"`
	ExitCode        string          `json:"EXIT_CODE"`
	ExitStatus      string          `json:"EXIT_STATUS"`
	Pid             string          `json:"_PID"`
	resolvedMessage string
	resolvedKind    int
}

const (
	MessageKind_Uninitialized = iota
	MessageKind_AppMessage
	MessageKind_Start
	MessageKind_Stopping
	MessageKind_Failed
	MessageKind_Stopped
	MessageKind_JournalUnknown
)

func (jcm *JournalCtlMessage) MessageKind() int {
	if jcm.resolvedKind == MessageKind_Uninitialized {
		init := func() int {
			if jcm.SyslogId == "systemd" {
				if jcm.JobType == "start" {
					return MessageKind_Start
				} else if jcm.JobType == "stop" {
					return MessageKind_Stopped
				} else if jcm.CodeFunc == "unit_status_log_starting_stopping_reloading" {
					messageP, err := jcm.MessageLine()
					message := *messageP
					if err == nil && strings.HasPrefix(message, "Stopping ") {
						return MessageKind_Stopping
					}
				} else if jcm.CodeFunc == "unit_log_resources" {
					messageP, err := jcm.MessageLine()
					message := *messageP
					if err == nil && strings.Contains(message, ": Consumed") && strings.HasSuffix(message, " CPU time.") {
						return MessageKind_Failed
					}
				}
				return MessageKind_JournalUnknown
			} else {
				return MessageKind_AppMessage
			}
		}
		jcm.resolvedKind = init()
	}
	return jcm.resolvedKind
}

func (jcm *JournalCtlMessage) MessageLine() (*string, error) {
	if jcm.resolvedMessage != "" {
		return &jcm.resolvedMessage, nil
	}
	if len(jcm.Message) > 0 {
		var err error
		var line string
		firstCh := string(jcm.Message[0])
		if firstCh == "[" {
			var bytes []byte
			err = json.Unmarshal(jcm.Message, &bytes)
			if err != nil {
				msg := ""
				return &msg, err
			} else {
				jcm.resolvedMessage = string(bytes)
				return &jcm.resolvedMessage, err
			}
		} else if firstCh == `"` {
			err = json.Unmarshal(jcm.Message, &line)
			if err != nil {
				msg := ""
				return &msg, err
			} else {
				jcm.resolvedMessage = line
				return &jcm.resolvedMessage, err
			}
		} else {
			msg := string(jcm.Message)
			if msg == "null" {
				return nil, nil
			} else {
				return nil, stacktrace.NewError(fmt.Sprintf("unknown message format - %s", string(jcm.Message)))
			}
		}
	} else {
		return nil, nil
	}
}

type JournalCtlExtraData struct {
}

func StartProcessRunSlurp(parentCtx a8.ContextI, services *AgentServices, info *SystemdService) *ProcessRunSlurp {

	pid := currentPid(info.Record.Name, services)

	ctx := parentCtx.ChildContext()

	sps := ProcessRunSlurp{
		Service:    info,
		Services:   services,
		ProcessUid: model.RandomProcessUid(),
		SlurpChan: &SlurpChan{
			Chan: make(chan *rpc.Buffer, 100),
			Name: ConsoleChannel,
		},
		Ctx:     ctx,
		Running: true,
	}

	extraDataJson, err := json.Marshal(JournalCtlExtraData{})
	if err != nil {
		log.Error("error marshalling journalctl extra data %v", err)
		extraDataJson = []byte("{}")
	}

	processPid, err := strconv.Atoi(pid)
	if err != nil {
		log.Error("error parsing pid %v", err)
	}

	err = services.NefarioClient.ProcessStart(
		ctx,
		&rpc.ProcessStartedRequest{
			ProcessUid:          sps.ProcessUid.String(),
			ParentProcessRunUid: services.ProcessUid,
			MinionUid:           services.MinionUid,
			ServiceUid:          info.Record.Uid,
			StartedAt:           rpc.NowTimeStamp(),
			Category:            "journalctl",
			Channels:            []string{ConsoleChannel.String()},
			Cwd:                 "",
			ProcessPid:          int32(processPid),
			Kind:                "journalctl",
			Controllable:        false,
			Env:                 []string{},
			LaunchError:         "",
			ExtraDataJsonStr:    string(extraDataJson),
		},
	)
	if err != nil {
		log.Error("NefarioClient.ProcessStart error %v", err)
		return nil
	}

	sps.SubmitChanPublisher(sps.SlurpChan)

	a8.SubmitGoRoutine(
		"watch-pid"+pid,
		func() error {
			cmd := services.RunLinuxCommand("tail", "-f", "--pid="+pid, "/dev/null")
			err := cmd.Start()
			if err != nil {
				return stacktrace.Propagate(err, "error starting tail --pid=%s /dev/null", pid)
			}
			err = cmd.Wait()
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					log.Trace("tail --pid=%s /dev/null exited with code %d", pid, exitErr.ExitCode())
				} else {
					return stacktrace.Propagate(err, "error waiting on tail --pid=%s /dev/null", pid)
				}
			}
			sps.Ctx.Cancel()
			return nil
		},
	)

	return &sps

}

func RunJournalCtlSlurp(ctx a8.ContextI, services *AgentServices) error {

	defer ctx.Cancel()

	cmd := services.RunLinuxCommand("journalctl", "-f", "--user", "--output=json", "--output-fields=MESSAGE,_PID,SYSLOG_IDENTIFIER,JOB_TYPE,JOB_RESULT,EXIT_CODE,EXIT_STATUS,CODE_FUNC,_SYSTEMD_USER_UNIT,USER_UNIT")

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pings := make([]*rpc.ProcessPing, 0)
				services.ServicesByName.Range(func(_ string, service *SystemdService) bool {
					if service.CurrentSlurp != nil {
						pings = append(pings, &rpc.ProcessPing{
							ProcessUid: service.CurrentSlurp.ProcessUid.String(),
							ChannelSizes: map[string]uint64{
								"journalctl": service.CurrentSlurp.SlurpChan.Size,
							},
						})
					}
					return true
				})
				if len(pings) > 0 {
					err := services.NefarioClient.ProcessPing(
						ctx,
						&rpc.ProcessPingRequest{
							Pings:     pings,
							Timestamp: rpc.NowTimeStamp(),
						},
					)
					if err != nil {
						log.Error("error pinging journalctl process %v", err)
					}
				}
			}
		}

	}()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return stacktrace.Propagate(err, "error getting stdout pipe")
	}
	defer stdout.Close()

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return stacktrace.Propagate(err, "error getting stderr pipe")
	}
	defer stderr.Close()

	err = cmd.Start()
	if err != nil {
		return stacktrace.Propagate(err, "error starting cmd")
	}

	// ??? TODO do something with stderr
	// submitJournalCtlReader("stderr", stderr, true)

	SubmitJournalCtlStdoutReader(ctx, stdout, services)

	a8.SubmitGoRoutine(
		"journalctl-waiter",
		func() error {
			err := cmd.Wait()
			ctx.Cancel()
			return err
		},
	)

	<-ctx.Done()

	cmd.Process.Signal(os.Interrupt)

	return nil

}

func currentPid(serviceName string, services *AgentServices) string {
	showPidCmd := services.RunLinuxCommand("systemctl", "--user", "show", "--value", "--property", "MainPID", serviceName)
	result, err := showPidCmd.CombinedOutput()
	if err != nil {
		err = stacktrace.Propagate(err, "error getting pid for %s", serviceName)
		log.Error(err.Error())
		return ""
	}
	resultStr := strings.TrimSpace(string(result))
	pid, err := strconv.Atoi(resultStr)
	if err != nil {
		err = stacktrace.Propagate(err, "error getting pid for %s -- %s", serviceName, resultStr)
		log.Error(err.Error())
		return ""
	}
	return strconv.Itoa(pid)
}

func (sps *ProcessRunSlurp) Shutdown(exitMessage string) {
	sps.ShutdownOnce.Do(func() {
		mockCtx := context.Background()
		err := sps.Services.NefarioClient.ProcessCompleted(
			mockCtx,
			&rpc.ProcessCompletedRequest{
				ProcessUid:  sps.ProcessUid.String(),
				ExitCode:    0,
				ExitMessage: exitMessage,
				CompletedAt: rpc.NowTimeStamp(),
			},
		)
		if err != nil {
			log.Error("ProcessCompleted error %v - %v", err, sps.ProcessUid)
		}
		sps.Running = false
		sps.Ctx.Cancel()
	})
}

func SubmitJournalCtlStdoutReader(ctx a8.ContextI, rawStdoutReader io.ReadCloser, services *AgentServices) {
	notFoundServiceNames := make(map[string]struct{})
	a8.SubmitGoRoutine(
		"journalctl-reader-stdout",
		func() error {
			stdoutReader := bufio.NewReader(rawStdoutReader)
			for {
				line, err := stdoutReader.ReadBytes('\n')
				// log.Trace("received journalctl line -- %s", line)
				if err != nil {
					// Read returns io.EOF at the end of file, which is not an error for us
					happyPathError := errors.Is(err, os.ErrClosed) || errors.Is(err, io.EOF)
					if happyPathError {
						err = nil
					}
					return err
				}
				// fmt.Printf("read %s %d bytes", label, len(line))
				// fmt.Println(string(line))
				var message JournalCtlMessage
				err = json.Unmarshal(line, &message)
				if err != nil {
					log.Error("error unmarshalling journalctl message %v -- %s", err, line)
					continue
				}

				if message.SystemdUserUnit == "minion.service" {
					// skip ourselves otherwise it could get into an ugly forever loop
					continue
				}

				var service *SystemdService
				var serviceNames []string = nil
				initService := func(serviceName string) {
					if service == nil && serviceName != "" {
						serviceNames = append(serviceNames, serviceName)
						service, _ = services.ServicesByName.Load(serviceName)
					}
				}

				initService(message.SystemdUserUnit)
				initService(message.UserUnit)

				if service == nil {
					if len(serviceNames) > 0 {
						for _, serviceName := range serviceNames {
							if _, ok := notFoundServiceNames[serviceName]; !ok {
								log.Warn("service not found -- %s -- %s", serviceName, line)
								notFoundServiceNames[serviceName] = struct{}{}
							}
						}
					} else {
						log.Debug("no service name found in line -- %s", line)
					}
					continue
				}

				if !service.Record.MinionEnabled && service.CurrentSlurp == nil {
					continue
				}

				stopCurrentSlurp := func(exitMessage string) {
					if service.CurrentSlurp != nil {
						service.CurrentSlurp.Shutdown(exitMessage)
						service.CurrentSlurp = nil
					}
				}

				newSlurp := func() {
					stopCurrentSlurp("disappeared")
					service.CurrentSlurp = StartProcessRunSlurp(ctx, services, service)
				}

				messageKind := message.MessageKind()
				switch messageKind {
				case MessageKind_AppMessage:
					messageLineP, err := message.MessageLine()
					if err != nil {
						log.Warn(err.Error())
					} else if messageLineP == nil {
						log.Trace("dropping null message line %s", line)
					} else {
						messageLine := *messageLineP
						if service.CurrentSlurp == nil {
							newSlurp()
						}
						buffer := &rpc.Buffer{Timestamp: rpc.NowTimeStamp(), Data: []byte(messageLine + "\n")}
						service.CurrentSlurp.SlurpChan.Chan <- buffer
					}
				case MessageKind_Start:
					newSlurp()
				case MessageKind_Stopping:
					log.Debug("received stopping %s", service.Record.Name)
				case MessageKind_Failed:
					stopCurrentSlurp("failed")
				case MessageKind_Stopped:
					stopCurrentSlurp("stopped")
				default:
					log.Error("don't know how to handle message kind %v -- %v", messageKind, line)
				}
			}
		},
	)
}

func (sps *ProcessRunSlurp) SubmitChanPublisher(slurpChan *SlurpChan) {

	streamName := slurpChan.Name

	captureWindow := sps.Service.CaptureWindow
	if captureWindow == nil {
		captureWindow = &CaptureWindow{
			MaxBytes: 1000000,
			MaxTime:  time.Second * 3,
		}
	}
	captureWindowInMillis := int64(captureWindow.MaxTime / time.Millisecond)
	start := a8.NowUtcTimestamp()

	a8.SubmitGoRoutine(
		"journalctl-streamwrite-"+sps.Service.Record.Name,
		func() error {

			defer func() {
				log.Debug("%v", sps)
				sps.Shutdown("failed")
			}()

			byteCount := 0
			currentBuffers := make([]*rpc.Buffer, 4)
			sendCurrentBuffers := func() {
				if len(currentBuffers) > 0 {
					sps.Services.NefarioClient.StreamWrite(
						sps.Ctx,
						&rpc.StreamWrite{
							ProcessUid:  sps.ProcessUid.String(),
							ChannelName: ConsoleChannel.String(),
							Record: &rpc.StreamRecord{
								Sequence:  sps.Sequence,
								Timestamp: rpc.NowTimeStamp(),
								Buffers:   currentBuffers,
							},
						},
					)
					if log.IsTraceEnabled {
						log.Trace("sending %s %d bytes", streamName, byteCount)
					}
					slurpChan.Size += uint64(byteCount)
					byteCount = 0
					clear(currentBuffers)
					currentBuffers = currentBuffers[:0]
				}
				start = a8.NowUtcTimestamp()
			}
			for {
				now := a8.NowUtcTimestamp()
				timeLeft := time.Millisecond * time.Duration(start.InEpochhMillis()+captureWindowInMillis-now.InEpochhMillis())
				// log.Trace("time left %v", timeLeft)
				if timeLeft <= 0 {
					sendCurrentBuffers()
				} else {
					select {
					case <-sps.Ctx.Done():
						sendCurrentBuffers()
						return nil
					case buffer := <-slurpChan.Chan:
						byteCount += len(buffer.Data)
						currentBuffers = append(currentBuffers, buffer)
						if byteCount >= captureWindow.MaxBytes {
							sendCurrentBuffers()
						}
					case <-time.After(timeLeft):
						sendCurrentBuffers()
					}
				}
			}
		},
	)
}
