package launchy

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"reflect"
	"strings"
	"time"

	"accur8.io/godev/a8"
	"accur8.io/godev/a8nats"
	"accur8.io/godev/cdc"
	"accur8.io/godev/hermes/api"
	"accur8.io/godev/hermes/directclient"
	"accur8.io/godev/hermes/hproto"
	"accur8.io/godev/hermes/model"
	"accur8.io/godev/hermes/rpcserver"
	"accur8.io/godev/iter"
	launchy_proto "accur8.io/godev/launchy/proto"
	"accur8.io/godev/log"
	"accur8.io/godev/nefario/rpc"
	parse_service_list "github.com/lechuhuuha/parse-service-list"
	"github.com/palantir/stacktrace"
	"github.com/spf13/cobra"
	"golang.org/x/exp/maps"
)

type AgentServices struct {
	NefarioClient        NefarioServiceClient
	ProcessUid           string
	MinionUid            string
	GlobalFlags          *CmdGlobalFlagsS
	Mailbox              api.MailboxI
	User                 *user.User
	Config               AgentConfig
	DefaultCaptureWindow CaptureWindow
	ServicesByName       a8.SyncMap[string, *SystemdService]
	InitialLoadComplete  chan struct{}
}

type SystemdService struct {
	Record        *ServiceRecordDto
	CurrentSlurp  *ProcessRunSlurp
	CaptureWindow *CaptureWindow
	Status        rpc.SystemdServiceStatus
}

type ServiceRecordDto struct {
	Uid           string `json:"uid"`
	Name          string `json:"name"`
	MinionUid     string `json:"minionUid"`
	MinionEnabled bool   `json:"minionEnabled"`
	Exists        bool   `json:"exists"`
}

type AgentConfig struct {
	ServiceSlurpTimeout      time.Duration
	RecordWatchTimeout       time.Duration
	CentralNatsUrl           string
	RemoteLinuxCommandPrefix []string
}

type System struct {
	Args []string
}

func (services *AgentServices) RpcSystemctlListSystemdServices(*launchy_proto.ListSystemdServicesRequest) (*launchy_proto.ListSystemdServicesResponse, error) {
	var err error
	outputBytes, err := services.RunLinuxCommand("systemctl", "--user", "--no-pager", "--type=service", "--all").CombinedOutput()

	outputStr := scrubSystemCtlOutput(outputBytes)
	var response launchy_proto.ListSystemdServicesResponse
	if err != nil {
		response.ErrorMessage = err.Error()
		response.ErrorOutput = outputStr
	} else {
		log.Trace("parsing systemctl output -- \n%s", outputStr)
		items, err := parse_service_list.ParseSystemdOutput([]byte(outputStr))
		if err != nil {
			err := stacktrace.Propagate(err, "error parsing systemctl output -- \n%s", outputStr)
			response.ErrorMessage = err.Error()
			response.ErrorOutput = outputStr
		} else {
			log.Trace("parsing results\n%s", a8.ToJson(items))
			for _, item := range items {
				response.Services = append(response.Services, &launchy_proto.SystemdUnitInfo{
					Unit:        item.Name,
					Load:        item.Loaded,
					Active:      item.State,
					Sub:         item.Status,
					Description: item.Description,
				})
			}
		}
	}
	return &response, nil
}

func (services *AgentServices) RpcSystemdServiceAction(req *launchy_proto.SystemdServiceActionRequest) (*launchy_proto.SystemdServiceActionResponse, error) {
	var err error
	var output []byte
	actionName := strings.ToLower(launchy_proto.SystemdAction_name[int32(req.Action)])
	if a8.IsLinux() {
		if req.Action == launchy_proto.SystemdAction_Status {
			servicesList, err := services.RpcSystemctlListSystemdServices(&launchy_proto.ListSystemdServicesRequest{})
			if err != nil {
				return nil, stacktrace.Propagate(err, "error listing systemd services")
			}
			output, err = parseServicesStatus(servicesList, req.ServiceName)
			if err != nil {
				return nil, stacktrace.Propagate(err, "error parseServicesStatus")
			}
		} else {
			output, err = services.RunLinuxCommand("systemctl", "--user", actionName, req.ServiceName).CombinedOutput()
		}
	} else {
		output = []byte("fake output")
	}
	var response launchy_proto.SystemdServiceActionResponse
	response.CommandOutput = string(output)
	if err != nil {
		response.ErrorMessage = err.Error()
	}
	return &response, nil
}

func (as *AgentServices) RunLinuxCommand(name string, args ...string) *exec.Cmd {
	var ca []string
	ca = append(ca, as.Config.RemoteLinuxCommandPrefix...)
	ca = append(ca, name)
	ca = append(ca, args...)
	return Command(ca[0], ca[1:]...)
}

func (as *AgentServices) NefarioListSystemdServiceRecords() (*rpc.ListServicesResponse, error) {
	resp, err := api.RpcCall[*rpc.ListServicesRequest, *rpc.ListServicesResponse](
		&rpc.ListServicesRequest{
			MinionUid: as.MinionUid,
		},
		&api.RpcRequest{
			From:     as.Mailbox,
			To:       model.NefarioRpcMailbox,
			EndPoint: rpc.EndPointsByPath.ListSystemdServiceRecords.Path,
		},
	)
	if err != nil {
		return nil, stacktrace.Propagate(err, "NefarioListSystemdServiceRecords error")
	} else {
		return *resp, nil
	}
}

func (as *AgentServices) NefarioSystemdServiceRecordCrud(actions []*rpc.SystemdServiceRecordCrud) error {
	req := &rpc.SystemdServiceRecordCrudRequest{CrudActions: actions}
	_, err := api.RpcCall[*rpc.SystemdServiceRecordCrudRequest, *rpc.SystemdServiceRecordCrudResponse](
		req,
		&api.RpcRequest{
			From:     as.Mailbox,
			To:       model.NefarioRpcMailbox,
			EndPoint: rpc.EndPointsByPath.SystemdServiceRecordCrud.Path,
		},
	)
	return stacktrace.Propagate(err, "NefarioSystemdServiceRecordCrud error - %v", a8.ToProtoJson(req))
}

func AgentMain(cmd *cobra.Command, args []string, globalFlags *CmdGlobalFlagsS) {

	log.IsTraceEnabled = *globalFlags.TraceLogging
	log.IsLoggingEnabled = *globalFlags.Logging
	log.InitFileLogging("./logs", "minion")

	config := a8.ConfigLookup[Config](AppName)
	if config == nil {
		log.Error("unable to load config tried %v", a8.PossibleConfigFiles(AppName))
		os.Exit(1)
	}

	ctx := context.Background()

	mailbox, err := directclient.CreateRpcMailbox(
		ctx,
		&directclient.RpcMailboxConfig{
			Url:         config.CentralNatsUrl,
			Description: "minion agent rpc",
		},
	)
	if err != nil {
		panic(stacktrace.Propagate(err, "error creating rpc mailbox"))
	}

	currentUser, err := user.Current()
	if err != nil {
		panic(stacktrace.Propagate(err, "error getting current user"))
	}

	localNatsConfig := a8nats.ConnArgs{
		NatsUrl: config.LocalNatsUrl,
		Name:    "minion-agent-" + currentUser.Username,
	}

	nefarioClient, err := NewNefarioServiceClient(localNatsConfig, config.NefarioSubject)
	if err != nil {
		panic(err)
	}

	app := a8.GlobalApp()

	agentServices := &AgentServices{
		ProcessUid:    a8.RandomUid(),
		MinionUid:     config.MinionUid,
		Mailbox:       mailbox,
		NefarioClient: nefarioClient,
		GlobalFlags:   globalFlags,
		User:          currentUser,
		Config: AgentConfig{
			ServiceSlurpTimeout:      1 * time.Hour,
			RecordWatchTimeout:       1 * time.Hour,
			CentralNatsUrl:           config.CentralNatsUrl,
			RemoteLinuxCommandPrefix: config.RemoteLinuxCommandPrefix,
		},
		ServicesByName:      a8.NewSyncMap[string, *SystemdService](),
		InitialLoadComplete: make(chan struct{}, 1),
	}

	app.SubmitSubProcess(
		"agent-rpc",
		func(ctx a8.ContextI) error {
			return SubmitAgentRpcServer(ctx, agentServices)
		},
	)

	app.SubmitSubProcess(
		"service-slurper",
		func(ctx a8.ContextI) error {
			return RunServiceSlurper(ctx, agentServices)
		},
	)

	app.SubmitSubProcess(
		"nefario-lifecycle",
		func(ctx a8.ContextI) error {
			return RunMinionLifecycle(ctx, agentServices)
		},
	)

	app.SubmitSubProcess(
		"journalctl-manager",
		func(ctx a8.ContextI) error {
			return RunJournalCtlManager(app, ctx, agentServices)
		},
	)

	app.WaitForCompletion()

}

func RunMinionLifecycle(context context.Context, services *AgentServices) error {

	cwd, err := os.Getwd()
	if err != nil {
		return stacktrace.Propagate(err, "error getting cwd")
	}

	parentProcess :=
		&rpc.ProcessStartedRequest{
			ProcessUid:   services.ProcessUid,
			ProcessPid:   int32(os.Getpid()),
			MinionUid:    services.MinionUid,
			StartedAt:    NowTimeStamp(),
			Command:      os.Args,
			Cwd:          cwd,
			Category:     *services.GlobalFlags.Category,
			Channels:     []string{},
			Controllable: true,
			Kind:         "minion",
			Env:          []string{},
			ServiceUid:   "",
		}

	err = services.NefarioClient.ProcessStart(context, parentProcess)
	if err != nil {
		return stacktrace.Propagate(err, "NefarioClient.ProcessStart error")
	}

	err = services.NefarioClient.UpdateMailbox(context, &rpc.UpdateMailboxRequest{ProcessUid: services.ProcessUid, Mailbox: services.Mailbox.Address().String()})
	if err != nil {
		return stacktrace.Propagate(err, "NefarioClient.UpdateMailbox error")
	}

outer:
	// pings while context still alive
	for {
		select {
		case <-context.Done():
			break outer
		case <-time.After(30 * time.Second):
			err = services.NefarioClient.ProcessPing(context, rpc.ProcessPingReq(&rpc.ProcessPing{ProcessUid: services.ProcessUid}))
			if err != nil {
				return stacktrace.Propagate(err, "NefarioClient.ProcessPing error")
			}
		}
	}

	// process complete
	err = services.NefarioClient.ProcessCompleted(
		context,
		&rpc.ProcessCompletedRequest{
			ProcessUid:  services.ProcessUid,
			ExitCode:    0,
			ExitMessage: "complete",
			CompletedAt: NowTimeStamp(),
		},
	)
	if err != nil {
		return stacktrace.Propagate(err, "NefarioClient.ProcessCompleted error")
	}
	return nil

}

func SubmitAgentRpcServer(context context.Context, services *AgentServices) error {

	mailbox := services.Mailbox

	server := rpcserver.NewRpcServer(mailbox.Address())

	server.RegisterHandlers(
		rpcserver.Handler("ListSystemdServices", services.RpcSystemctlListSystemdServices),
		rpcserver.Handler("SystemdServiceAction", services.RpcSystemdServiceAction),
		// rpcserver.Handler("stopProcess", lc.RpcStopProcess),
		// rpcserver.Handler("processStatus", lc.RpcProcessStatus),
		// rpcserver.Handler("stopLaunchy", lc.RpcStopLaunchy),
	)

	if log.IsTraceEnabled {
		log.Trace("mailbox keys %s", a8.ToJson(mailbox.Keys()))
	}

	msgHander := func(msg *hproto.Message) error {
		smr := server.ProcessRpcCall(msg)
		log.Debug("sending rpc response %+v", smr)
		_, err := services.Mailbox.PublishSendMessageRequest(smr)
		return err
	}

	mailbox.RunRpcInboxReader(context, msgHander)

	return nil

}

func RunServiceSlurper(ctx a8.ContextI, services *AgentServices) error {

	runSingle := func() {
		err := RunServiceSlurpLoopOnce(services)
		if err != nil {
			log.Error(err.Error())
		}
	}

	ticker := time.NewTicker(services.Config.ServiceSlurpTimeout)
	defer ticker.Stop()
	// slurp services

	runSingle()
	close(services.InitialLoadComplete)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			runSingle()
		}
	}

}

func RunServiceSlurpLoopOnce(services *AgentServices) error {

	responseFromSystemCtl, err := services.RpcSystemctlListSystemdServices(&launchy_proto.ListSystemdServicesRequest{})
	if err != nil {
		return stacktrace.Propagate(err, "error listing systemd services")
	}

	responseFromDb, err := services.NefarioListSystemdServiceRecords()
	if err != nil {
		return stacktrace.Propagate(err, "error listing systemd services in db")
	}

	actualServices := responseFromSystemCtl.Services
	servicesInDb := responseFromDb.Records

	actualServicesByName := iter.FromSlice(actualServices).ToMap(func(service *launchy_proto.SystemdUnitInfo) string { return service.Unit })
	servicesInDbByName := iter.FromSlice(servicesInDb).ToMap(func(service *rpc.ServiceRecord) string { return service.Name })

	allKeys := maps.Keys(actualServicesByName)
	allKeys = append(allKeys, maps.Keys(servicesInDbByName)...)

	allKeys = a8.RemoveDupes(allKeys)

	crudActions := []*rpc.SystemdServiceRecordCrud{}

	getStatus := func(unit *launchy_proto.SystemdUnitInfo) rpc.SystemdServiceStatus {
		if unit.Sub == "running" {
			return rpc.SystemdServiceStatus_SystemdServiceStatus_Running
		} else if unit.Sub == "dead" {
			return rpc.SystemdServiceStatus_SystemdServiceStatus_Stopped
		} else {
			return rpc.SystemdServiceStatus_SystemdServiceStatus_Unknown
		}
	}

	toServiceUnitJson := func(unit *launchy_proto.SystemdUnitInfo) *rpc.ServiceUnitJson {
		return &rpc.ServiceUnitJson{
			Status:          getStatus(unit),
			UnitDescription: unit.Description,
			StatusSource:    "systemctl",
			UnitExists:      true,
			UnitName:        unit.Unit,
		}
	}

	for _, serviceName := range allKeys {
		actualService, actualServiceExists := actualServicesByName[serviceName]
		serviceInDb, serviceInDbExists := servicesInDbByName[serviceName]

		if actualServiceExists && serviceInDbExists {
			// update
			serviceUnitJson := toServiceUnitJson(actualService)
			if !reflect.DeepEqual(serviceUnitJson, serviceInDb.UnitJson) {
				serviceInDb.UnitJson = serviceUnitJson
				update := &rpc.SystemdServiceRecordCrud{Action: rpc.CrudAction_update, Record: serviceInDb}
				crudActions = append(crudActions, update)
			}
		} else if actualServiceExists {
			// insert
			serviceInDb = &rpc.ServiceRecord{}
			serviceInDb.Uid = a8.RandomUid()
			serviceInDb.MinionUid = services.MinionUid
			serviceInDb.Name = serviceName
			serviceInDb.UnitJson = toServiceUnitJson(actualService)
			update := &rpc.SystemdServiceRecordCrud{Action: rpc.CrudAction_insert, Record: serviceInDb}
			crudActions = append(crudActions, update)
		} else if serviceInDbExists {
			// delete
			if serviceInDb.UnitJson.UnitExists {
				serviceInDb.UnitJson.UnitExists = false
				serviceInDb.UnitJson.Status = rpc.SystemdServiceStatus_SystemdServiceStatus_Unspecified
				serviceInDb.UnitJson.StatusSource = "service not showing in systemctl"
				update := &rpc.SystemdServiceRecordCrud{Action: rpc.CrudAction_update, Record: serviceInDb}
				crudActions = append(crudActions, update)
			}
		}

		_, exists := services.ServicesByName.Load(serviceName)
		if !exists {
			record := &ServiceRecordDto{
				Uid:           serviceInDb.Uid,
				Name:          serviceInDb.Name,
				MinionUid:     serviceInDb.MinionUid,
				MinionEnabled: serviceInDb.MinionEnabled,
			}
			services.ServicesByName.Store(serviceName, &SystemdService{Record: record})
		}

	}

	if len(crudActions) > 0 {
		err := services.NefarioSystemdServiceRecordCrud(crudActions)
		if err != nil {
			return stacktrace.Propagate(err, "error sending systemd service crud actions")
		}
	}

	return nil

}

func Command(name string, args ...string) *exec.Cmd {
	// args = append([]string{"dev@tulip", "--", name}, args...)
	// args = append([]string{"glen@ahs-hermes", "--", name}, args...)
	// name = "ssh"
	log.Debug("running command -- %v %v", name, strings.Join(args, " "))
	return exec.Command(name, args...)
}

func RunJournalCtlManager(app a8.App, ctx a8.ContextI, services *AgentServices) error {

	processRecord := func(label string, record *ServiceRecordDto) error {
		if record.MinionUid == services.MinionUid {
			log.Debug("cdc event %s - %s", label, a8.ToJson(record))
			service, found := services.ServicesByName.Load(record.Name)
			if found {
				if service.Record.MinionEnabled != record.MinionEnabled {
					service.Record.MinionEnabled = record.MinionEnabled
				}
			} else {
				services.ServicesByName.Store(
					record.Name,
					&SystemdService{
						Record: record,
					},
				)
			}
		}
		return nil
	}

	processChangeEvent := func(event *cdc.ChangeDataCaptureEvent, record *ServiceRecordDto) error {
		return processRecord(event.Action, record)
	}

	natsConn, err := a8nats.ResolveConn(a8nats.ConnArgs{NatsUrl: services.Config.CentralNatsUrl})
	if err != nil {
		return stacktrace.Propagate(err, "error resolving nats conn")
	}

	a8.SubmitGoRoutine(
		"run-change-data-capture",
		func() error {
			return cdc.RunChangeDataCapture(
				cdc.ChangeDataCaptureConfig{
					Ctx:      ctx,
					NatsConn: natsConn,
					Root:     "wal_listener",
					Database: "nefario",
					Table:    "service",
					StartSeq: "new",
				},
				processChangeEvent,
			)
		},
	)

	initialServicesResponse, err := services.NefarioListSystemdServiceRecords()
	if err != nil {
		log.Error("error listing initial systemd services %v", err)
	} else {
		for _, serviceRecord := range initialServicesResponse.Records {
			processRecord(
				"initial",
				&ServiceRecordDto{
					Uid:           serviceRecord.Uid,
					Name:          serviceRecord.Name,
					MinionUid:     serviceRecord.MinionUid,
					MinionEnabled: serviceRecord.MinionEnabled,
				},
			)
		}
	}

	a8.SubmitGoRoutine(
		"journalctl-slurp",
		func() error {
			return RunJournalCtlSlurp(ctx, services)
		},
	)

	<-ctx.Done()

	return nil

}
