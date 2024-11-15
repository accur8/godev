package hermes

import (
	"context"
	"encoding/json"
	"time"

	"accur8.io/godev/a8"
	"accur8.io/godev/a8nats"
	"accur8.io/godev/hermes/api"
	"accur8.io/godev/hermes/hproto"
	"accur8.io/godev/hermes/model"
	"accur8.io/godev/httpserver"
	"accur8.io/godev/log"
	"github.com/google/uuid"
	"github.com/palantir/stacktrace"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/go-co-op/gocron/v2"
)

// var GlobalServices Services

type Config struct {
	WebSocketTimeoutInMs int64
}

type Services struct {
	MailboxStore  MailboxStore
	ActiveClients map[uuid.UUID]*ActiveClientW
	Config        *Config
	App           a8.App
	Scheduler     gocron.Scheduler
}

func (services *Services) Nats() a8nats.NatsConnI {
	return services.MailboxStore.NatsService()
}

func (services *Services) Register(acw *ActiveClientW) {
	services.ActiveClients[*acw.Uuid()] = acw
}

type RpcCallRequest struct {
	ToMailbox       api.MailboxAddress `json:"toMailbox"`
	EndPoint        string             `json:"endPoint"`
	Channel         api.ChannelName    `json:"channel"`
	TimeoutInMillis uint32             `json:"timeoutInMillis"`
	RequestBody     json.RawMessage    `json:"requestBody"`
	Headers         map[string]string  `json:"headers"`
}

type RpcCallResponse struct {
	ErrorMessage string          `json:"errorMessage,omitempty"`
	ResponseBody json.RawMessage `json:"responseBody,omitempty"`
}

type MessageHandler chan *hproto.Message

type MailboxInfoRequest struct {
	AdminKey model.AdminKey `json:"adminKey"`
}

func MailboxInfoHandler(services *Services, req *MailboxInfoRequest) (*Mailbox, error) {
	mbox := services.MailboxStore.FetchMailboxByAdminKey(req.AdminKey)
	if mbox == nil {
		return nil, httpserver.NewError("mailbox not found", 404)
	}
	return mbox, nil
}

func BuildRpcCallHandler(services *Services) func(interface{}, *RpcCallRequest) (*RpcCallResponse, error) {

	localMailbox, err := services.MailboxStore.CreateMailbox(
		&hproto.CreateMailboxRequest{
			Channels: api.RpcChannelsAsStrings,
			PublicMetadata: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"description": structpb.NewStringValue("hermes rpc call handler mailbox"),
				},
			},
			PrivateMetadata: &structpb.Struct{},
		},
	)

	services.RunEvery(
		"local mailbox keep alive",
		5*time.Minute,
		func() error {
			if log.IsTraceEnabled {
				log.Trace("local mailbox keep alive")
			}
			localMailbox.UpdateLastActivity()
			return localMailbox.UpdateInStore()
		},
	)

	localMailbox.UpdateLastActivity()

	if err != nil {
		panic(stacktrace.Propagate(err, "unable to create local mailbox"))
	}

	if localMailbox == nil {
		panic("unable to create local mailbox")
	}
	log.Debug("local mailbox created %+v", *localMailbox.Keys())

	services.App.SubmitSubProcess(
		"rpc-call-inbox-reader",
		func(ctx a8.ContextI) error {
			localMailbox.RunRpcInboxReader(ctx, nil)
			return nil
		},
	)

	return func(env interface{}, req *RpcCallRequest) (*RpcCallResponse, error) {
		log.Debug("RunRpcCall() %+v", req)

		rpcTimeout := time.Duration(req.TimeoutInMillis) * time.Millisecond
		if rpcTimeout == 0 {
			rpcTimeout = time.Duration(5) * time.Minute
		}

		ctx, cancelFn := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancelFn()

		channel := req.Channel
		if channel.IsEmpty() {
			channel = api.RpcInbox
		}

		responseMsg, err := localMailbox.RawRpcCall(
			req.RequestBody,
			&api.RpcRequest{
				From:                   localMailbox,
				To:                     req.ToMailbox,
				EndPoint:               req.EndPoint,
				Headers:                req.Headers,
				Channel:                channel,
				Context:                ctx,
				RequestBodyContentType: hproto.ContentType_Json,
			},
		)
		if err != nil {
			err = stacktrace.Propagate(err, "error calling localMailbox.RawRpcCall")
			return &RpcCallResponse{
				ErrorMessage: err.Error(),
			}, nil
		}

		var response *RpcCallResponse
		if responseMsg == nil {
			response = &RpcCallResponse{
				ErrorMessage: "request timed out before response received",
			}
		} else {
			errInfo := responseMsg.GetHeader().GetRpcHeader().GetErrorInfo()
			var errMsg string
			if errInfo != nil {
				errMsg = errInfo.Message
			}
			response = &RpcCallResponse{
				ErrorMessage: errMsg,
				ResponseBody: responseMsg.Data,
			}
		}

		return response, nil
	}
}

func RunMailboxPurge(ctx a8.ContextI, services *Services) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(30 * time.Minute):
			err := MailboxPurgeSinglePass(ctx, services)
			if err != nil {
				log.Warn("MailboxPurgeSinglePass error(s) %s", err)
			}
		}
	}
}

func MailboxPurgeSinglePass(ctx a8.ContextI, services *Services) error {
	log.Debug("running mailbox purge")
	count := 0
	err := services.MailboxStore.ForEachMailbox(func(adminKey model.AdminKey) error {
		mbox := services.MailboxStore.FetchMailboxByAdminKey(adminKey)
		if !mbox.IsPurgable() {
			count += 1
			services.MailboxStore.Purge(mbox)
		}
		return nil
	})
	log.Debug("purged %v mailboxes returning with %v", count, err)
	return err
}

func RunMailboxSyncToDisk(ctx a8.ContextI, services *Services) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Minute):
			err := MailboxSyncToDiskSinglePass(ctx, services)
			if err != nil {
				log.Warn("MailboxSyncToDiskSinglePass error(s) %s", err)
			}
		}
	}
}

func MailboxSyncToDiskSinglePass(ctx a8.ContextI, services *Services) error {
	// log.Debug("running MailboxSyncToDiskSinglePass")
	count := 0
	err := services.MailboxStore.ForEachMailboxInMemory(func(mbox *Mailbox) error {
		if mbox.NeedsUpdate() {
			count += 1
			mbox.UpdateInStore()
		}
		return nil
	})
	if count > 0 {
		log.Debug("synced %v mailboxes returning with %v", count, err)
	}
	return err
}

func (s *Services) RunEvery(
	label string,
	duration time.Duration,
	taskFn func() error,
) {

	wrappedFn := func() {
		err := taskFn()
		if err != nil {
			err = stacktrace.Propagate(err, "swallowing error processing %s, will continue with schedule", label)
			log.Debug(err.Error())
		}
	}

	_, err := s.Scheduler.NewJob(
		gocron.DurationJob(
			duration,
		),
		gocron.NewTask(wrappedFn),
	)
	if err != nil {
		err = stacktrace.Propagate(err, "swallowing error unable to schedule %s", label)
		log.Error(err.Error())
	}
}
