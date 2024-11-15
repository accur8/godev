package hermes

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"accur8.io/godev/a8"
	"accur8.io/godev/a8nats"
	"accur8.io/godev/hermes/api"
	"accur8.io/godev/hermes/hproto"
	"accur8.io/godev/hermes/model"
	"accur8.io/godev/httpserver"
	launchy_proto "accur8.io/godev/launchy/proto"
	"accur8.io/godev/log"
	"accur8.io/godev/nefario"
	"accur8.io/godev/nefario/checks"
	"accur8.io/godev/nefario/rpc"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/NYTimes/gziphandler"
	"github.com/go-co-op/gocron/v2"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/nats-io/nats.go"
	"github.com/palantir/stacktrace"
)

var (
	HermesSendMessageRequestWorkQueue = WorkQueue("hermes-control.SendMessageRequest")
)

func WorkQueue(name string) WorkQueueI {
	return &_WorkQueue{Name: name}
}

type _WorkQueue struct {
	Name string
}
type WorkQueueI interface {
	Subject() a8nats.Subject
	Stream() a8nats.StreamName
	Consumer() a8nats.ConsumerName
}

func (wq *_WorkQueue) Subject() a8nats.Subject {
	return a8nats.Subject(wq.Name)
}

func (wq *_WorkQueue) Stream() a8nats.StreamName {
	return a8nats.StreamName(strings.ReplaceAll(wq.Name, ".", "-"))
}

func (wq *_WorkQueue) Consumer() a8nats.ConsumerName {
	return a8nats.ConsumerName(wq.Stream())
}
func RunLifeCycleChecks(ctx context.Context, services *Services) error {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			return nil
		case <-ticker.C:
			RunSingleLifeCycleCheck(services)
		}
	}
}

func RunSingleLifeCycleCheck(services *Services) {
	log.Debug("RunSingleLifeCycleCheck() %d active web sockets", len(services.ActiveClients))
	timeoutCutoff := a8.NowUtcTimestamp().InEpochhMillis() - services.Config.WebSocketTimeoutInMs
	for uuid, acw := range services.ActiveClients {
		if acw.Client.GetClientInfoX().LastActivity.InEpochhMillis() <= timeoutCutoff {
			delete(services.ActiveClients, uuid)
			go acw.Shutdown()
		}
	}
}

type MainConfig struct {
	NatsUrl           string   `json:"natsUrl"`
	HttpListenAddress string   `json:"httpListenAddress"`
	GrpcListenAddress string   `json:"grpcListenAddress"`
	DatabaseUrl       string   `json:"databaseUrl"`
	Engines           []string `json:"engines"` // http purge nefario
}

func (mc *MainConfig) HasEngine(name string) bool {
	for _, engine := range mc.Engines {
		if strings.EqualFold(engine, name) {
			return true
		}
	}
	return false
}

func init() {

	log.Info("initializing")
	// Log as JSON instead of the default ASCII formatter.
	// log.SetFormatter(&log.JSONFormatter{})

	// Output to stdout instead of the default stderr
	// Can be any io.Writer, see below for File example
	// log.SetOutput(os.Stdout)

	// Only log the warning severity or above.
	// log.SetLevel(log.TraceLevel)

}

func initializeServices() (*Services, *MainConfig, error) {

	log.IsTraceEnabled = *TraceLogging
	// log.IsLoggingEnabled = *loggingF
	log.IsLoggingEnabled = true
	log.InitFileLogging("./logs", "hermes")

	mainConfig := a8.ConfigLookup[MainConfig]("hermes")
	if mainConfig == nil {
		return nil, nil, stacktrace.NewError("unable to find hermes config - %+v", a8.PossibleConfigFiles("hermes"))
	}

	// Connect to a server
	natsConn, err := a8nats.ResolveConn(a8nats.ConnArgs{NatsUrl: mainConfig.NatsUrl, Name: "hermes"})
	if err != nil {
		return nil, nil, stacktrace.Propagate(err, "nats connect failure")
	}

	mailboxStore := NewMailboxStoreStruct(natsConn, true)

	app := a8.GlobalApp()

	scheduler, err := gocron.NewScheduler()
	if err != nil {
		return nil, nil, stacktrace.Propagate(err, "error creating scheduler")
	}

	scheduler.Start()

	// not the prettiest but it avoids a global var
	services := &Services{
		MailboxStore:  mailboxStore,
		Config:        &Config{WebSocketTimeoutInMs: 90_000},
		ActiveClients: make(map[uuid.UUID]*ActiveClientW),
		App:           app,
		Scheduler:     scheduler,
	}

	log.Info("connected to nats %s", mainConfig.NatsUrl)

	return services, mainConfig, nil

}

func RunMailboxInfoCmd(args []string) {
	services, _, err := initializeServices()
	if err != nil {
		err = stacktrace.Propagate(err, "error initializing services")
		log.Error("%v", err)
		os.Exit(1)
	}

	for _, key := range args {
		mbox := services.MailboxStore.FetchMailboxByAddress(model.NewMailboxAddress(key))
		if mbox == nil {
			mbox = services.MailboxStore.FetchMailboxByAdminKey(model.NewAdminKey(key))
		}
		if mbox == nil {
			mbox = services.MailboxStore.FetchMailboxByReaderKey(model.ReaderKey(model.NewReaderKey(key)))
		}
		if mbox == nil {
			log.Info("mailbox not found %s", key)
		} else {
			log.Info("mailbox %s found %s", key, a8.ToJson(mbox))
		}
	}

}

func RunCreateNamedMailbox(address api.MailboxAddress) {

	services, _, err := initializeServices()
	if err != nil {
		err = stacktrace.Propagate(err, "error initializing services")
		log.Error("%v", err)
		os.Exit(1)
	}

	{
		mbox0 := services.MailboxStore.FetchMailboxByAddress(address)
		if mbox0 != nil {
			log.Info("mailbox already exists \n%s", a8.ToJson(mbox0.Keys()))
			return
		}
	}

	mbox, err := services.MailboxStore.FetchOrCreateNamedMailbox(address)
	if err != nil {
		err = stacktrace.Propagate(err, "error creating named mailbox")
		log.Error(err.Error())
	}

	log.Info("created named mailbox \n%s", a8.ToJson(mbox.Keys()))

}

func RunServer() {

	log.Info("running")

	services, mainConfig, err := initializeServices()
	if err != nil {
		err = stacktrace.Propagate(err, "error initializing services")
		log.Error("%v", err)
		os.Exit(1)
	}

	mainApp := services.App

	if mainConfig.HasEngine("nefario") {

		mainApp.SubmitSubProcess(
			"nefario",
			func(ctx a8.ContextI) error {

				nefarioAddress := model.NefarioRpcMailbox

				mailbox, err := services.MailboxStore.FetchOrCreateNamedMailbox(nefarioAddress)
				if err != nil {
					return stacktrace.Propagate(err, "error creating named mailbox - %s", nefarioAddress)
				}

				return nefario.Run(
					ctx,
					&nefario.Config{
						DatabaseUrl: mainConfig.DatabaseUrl,
						NatsConn:    services.Nats(),
						Mailbox:     mailbox,
					},
				)
			},
		)

		mainApp.SubmitSubProcess(
			"sendMessageRequestConsumer",
			func(ctx a8.ContextI) error {
				return RunSendMessageRequestConsumer(ctx, services)
			},
		)

	}

	if mainConfig.HasEngine("http") {
		mainApp.SubmitSubProcess(
			"lifeCycleChecks",
			func(ctx a8.ContextI) error {
				return RunLifeCycleChecks(ctx, services)
			},
		)

		mainApp.SubmitSubProcess(
			"grpcServer",
			func(ctx a8.ContextI) error {
				return RunGrpcServer(ctx, services, mainConfig)
			},
		)

		mainApp.SubmitSubProcess(
			"httpServer",
			func(ctx a8.ContextI) error {
				return RunHttpServer(ctx, services, mainConfig)
			},
		)

		mainApp.SubmitSubProcess(
			"mailbox-sync-to-disk",
			func(ctx a8.ContextI) error {
				return RunMailboxSyncToDisk(ctx, services)
			},
		)
	}

	if mainConfig.HasEngine("purge") {
		mainApp.SubmitSubProcess(
			"mailbox-purge",
			func(ctx a8.ContextI) error {
				return RunMailboxPurge(ctx, services)
			},
		)
	}

	mainApp.WaitForCompletion()

}

func RunSendMessageRequestConsumer(ctx context.Context, services *Services) error {

	messageHandler := func(smr *hproto.SendMessageRequest) error {
		fromAddress := smr.Message.Header.SenderO()
		mbox := services.MailboxStore.FetchMailboxByAddress(fromAddress)
		if mbox == nil {
			return stacktrace.NewError("Message.Header.Sender address %s not found", smr.Message.Header.Sender)
		}
		response, err := mbox.PublishSendMessageRequest(smr)
		if err != nil {
			err = stacktrace.Propagate(err, "error publishing SendMessageRequest")
			log.Error(err.Error())
		}
		if len(response.Errors) > 0 {
			log.Debug("unable to publish request to underlying messaging fabric -- %v", a8.ToJson(response))
		}
		return nil
	}

	config := &a8nats.WorkQueueConfig{
		Nats:               a8nats.ConnArgs{NatsConn: services.Nats()},
		Subject:            HermesSendMessageRequestWorkQueue.Subject(),
		StreamName:         HermesSendMessageRequestWorkQueue.Stream(),
		ConsumerName:       HermesSendMessageRequestWorkQueue.Consumer(),
		RetentionPolicy:    nats.LimitsPolicy,
		MaxDeliver:         5,
		AssumeProto:        true,
		FetchTimeoutMillis: 1_000,
		MaxMsgs:            10_000,
		MaxBytes:           60 * int64(a8.Units.Megabyte),
		CreateStream:       true,
	}

	return a8nats.RunProtoWorkQueue(ctx, config, messageHandler)
}

func RunHttpServer(ctx context.Context, services *Services, mainConfig *MainConfig) error {

	router := mux.NewRouter()

	// adds a handler wrapping in any needed middleware
	AddHandler := func(path string, handler http.HandlerFunc) *mux.Route {
		wrappedHandler := gziphandler.GzipHandler(handler)
		hackFn := func(resp http.ResponseWriter, req *http.Request) {
			httpserver.SetCorsHeaders(resp)
			if req.Method == "OPTIONS" {
				resp.WriteHeader(http.StatusOK)
			} else {
				wrappedHandler.ServeHTTP(resp, req)
			}
		}
		route := router.HandleFunc(path, hackFn)
		return route
	}

	router.HandleFunc("/api/ws/send_receive_proto", BuildSendReceiveWsHandler(services, false)).Methods("GET", "OPTIONS")
	router.HandleFunc("/api/ws/send_receive_json", BuildSendReceiveWsHandler(services, true)).Methods("GET", "OPTIONS")
	// router.HandleFunc("/api/ws/single_channel_writer/{writerKey}/{channel}", SingleChannelWriterWsHandler).Methods("GET")
	AddHandler("/api/create_mailbox", httpserver.JsonRpc(services, CreateMailboxRpc)).Methods("POST", "OPTIONS")
	AddHandler("/api/add_channel", httpserver.JsonRpc(services, AddChannelRpc)).Methods("POST", "OPTIONS")
	AddHandler("/api/rpc_call", httpserver.JsonRpcE("", BuildRpcCallHandler(services))).Methods("POST", "OPTIONS")
	AddHandler("/api/mailbox_info/{adminKey}", httpserver.JsonRpcE(services, MailboxInfoHandler)).Methods("GET", "OPTIONS")
	AddHandler("/api/stream_download/{processUid}/{streamName}", BuildDownloadStreamService(services, false)).Methods("GET", "OPTIONS")
	AddHandler("/api/stream_download/{processUid}/{streamName}/{startPos}", BuildDownloadStreamService(services, false)).Methods("GET", "OPTIONS")
	AddHandler("/api/stream_tail/{processUid}/{streamName}", BuildDownloadStreamService(services, true)).Methods("GET", "OPTIONS")
	AddHandler("/api/stream_tail/{processUid}/{streamName}/{startPos}", BuildDownloadStreamService(services, true)).Methods("GET", "OPTIONS")
	AddHandler("/api/channel_download/{adminKey}/{channelName}", BuildDownloadChannelService(services)).Methods("GET", "OPTIONS")
	AddHandler("/api/channel_download/{adminKey}/{channelName}/{startPos}", BuildDownloadChannelService(services)).Methods("GET", "OPTIONS")
	AddHandler("/api/proto_to_json", BuildProtoToJsonHandler()).Methods("POST", "OPTIONS")
	AddHandler("/api/status_html/31BA7EAA-2EDF-42D4-A207-AE4F0EF07A38", checks.BuildHttpHandler(ctx)).Methods("GET")
	AddHandler("/", notFoundHandler)

	log.Info("routes %s", httpserver.RouteDescription(router))

	log.Info("starting web server on %s", mainConfig.HttpListenAddress)

	srv := &http.Server{
		Addr:    mainConfig.HttpListenAddress,
		Handler: router,
	}

	go func() {
		err := srv.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			err = stacktrace.Propagate(err, "error listening on %s", mainConfig.HttpListenAddress)
			log.Error("%v", err)
		}
	}()

	<-ctx.Done()
	log.Debug("http server on %s stopping", mainConfig.HttpListenAddress)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer func() {
		// extra handling here
		cancel()
	}()

	if err := srv.Shutdown(ctx); err != nil {
		log.Error("Server Shutdown Failed: %+v", err)
		return err
	} else {
		log.Debug("Server Exited Properly")
		return nil
	}

}

func RunGrpcServer(ctx context.Context, services *Services, mainConfig *MainConfig) error {
	listenAddress := mainConfig.GrpcListenAddress
	log.Info("grpc listening on %s", listenAddress)
	lis, err := net.Listen("tcp", listenAddress)
	if err != nil {
		return stacktrace.Propagate(err, "unable to listen")
	}
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	hproto.RegisterHermesServiceServer(grpcServer, NewHermesServiceServer(services))
	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
	}()
	return grpcServer.Serve(lis)
}

func notFoundHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
}

func AddChannelRpc(services *Services, request *hproto.AddChannelRequest) (*hproto.AddChannelResponse, httpserver.HttpResponse) {

	mbox := services.MailboxStore.FetchMailboxByAdminKey(request.AdminKeyO())
	if mbox == nil {
		return nil, httpserver.ErrStrResponse(fmt.Sprintf("mailbox %s not found", request.AdminKey), http.StatusNotFound)
	}

	for _, channel := range request.ChannelNames() {
		err := mbox.AddChannel(channel, true)
		if err != nil {
			return nil, httpserver.ErrResponse(err, http.StatusConflict)
		}
	}

	return &hproto.AddChannelResponse{}, nil

}

func CreateMailboxRpc(services *Services, request *hproto.CreateMailboxRequest) (*hproto.CreateMailboxResponse, httpserver.HttpResponse) {

	log.Debug("creating mailbox -- %v", request)

	mbox, err := services.MailboxStore.CreateMailbox(request)
	if err != nil {
		return nil, httpserver.ErrResponse(err, http.StatusInternalServerError)
	}

	response := &hproto.CreateMailboxResponse{
		AdminKey:  mbox.AdminKey().String(),
		Address:   mbox.Address().String(),
		ReaderKey: mbox.ReaderKey().String(),
		Channels:  mbox.ChannelNamesAsStrings(),
	}

	log.Debug("created mailbox %+v", response)

	return response, nil
}

type MessageBuilder = func() proto.Message

func BuildProtoToJsonHandler() http.HandlerFunc {

	key := func(schema string, isRequest bool) string {
		schemaLc := strings.ToLower(schema)
		return fmt.Sprintf("%s-%v", schemaLc, isRequest)
	}

	schemas := map[string]MessageBuilder{}

	addEndPoint := func(endPoint model.RpcEndPoint) {
		schemas[key(endPoint.Path, true)] = endPoint.Request
		schemas[key(endPoint.Path, false)] = endPoint.Response
	}

	addServer := func(rpcServer model.RpcServer) {
		for _, endPoint := range rpcServer.EndPoints {
			addEndPoint(endPoint)
		}
	}

	addServer(launchy_proto.AllEndPoints)
	addServer(rpc.AllEndPoints)

	ProtoToJsonImpl := func(httpReq *http.Request) ([]byte, error) {

		schemaName := strings.ToLower(httpReq.URL.Query().Get("schema"))
		frameType := strings.ToLower(httpReq.URL.Query().Get("frametype"))
		base64DecodeStr := strings.ToLower(httpReq.URL.Query().Get("base64"))

		if schemaName == "" || frameType == "" {
			return nil, stacktrace.NewError("schema and frametype query params are required")
		}

		if frameType != "request" && frameType != "response" {
			return nil, stacktrace.NewError("frametype query param must be request | response")
		}

		base64Decode := base64DecodeStr == "true"

		schemaIsRequest := frameType == "request"

		schemaKey := key(schemaName, schemaIsRequest)

		msgBuilder, ok := schemas[schemaKey]
		if !ok {
			return nil, stacktrace.NewError("schema %s not found", schemaKey)
		}

		protoObj := msgBuilder()

		requestBodyBytes, err := io.ReadAll(httpReq.Body)
		if err != nil {
			return nil, stacktrace.Propagate(err, "error reading request body")
		}

		var xyz rpc.ListServicesRequest
		xyz.MinionUid = "IOis6tmDyFR13OGWewNUSm5VoyAX6lf5"
		xyzBytes, err := proto.Marshal(&xyz)
		log.Debug("xyzBytes %s err %c", base64.StdEncoding.EncodeToString(xyzBytes), err)

		var messageBytes []byte
		if base64Decode {
			messageBytes, err = base64.StdEncoding.DecodeString(string(requestBodyBytes))
			if err != nil {
				return nil, stacktrace.Propagate(err, "error base64 decoding request body")
			}
		} else {
			messageBytes = requestBodyBytes
		}

		var pdq rpc.ListServicesRequest
		err = proto.Unmarshal(messageBytes, &pdq)
		log.Debug("pdq %v", err)

		var xyz_r rpc.ListServicesRequest
		err = proto.Unmarshal(xyzBytes, &xyz_r)
		log.Debug("xyz_r %v", err)

		err = proto.Unmarshal(messageBytes, protoObj)
		if err != nil {
			return nil, stacktrace.Propagate(err, "error unmarshalling request body")
		}

		json, err := protojson.Marshal(protoObj)
		if err != nil {
			return nil, stacktrace.Propagate(err, "error marshalling proto to json")
		}

		return json, nil

	}

	ProtoToJson := func(w http.ResponseWriter, r *http.Request) {
		json, err := ProtoToJsonImpl(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.Write(json)
		}
	}

	return ProtoToJson

}
