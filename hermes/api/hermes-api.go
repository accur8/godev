package api

import (
	"context"
	"reflect"
	"time"

	"accur8.io/godev/a8"
	"accur8.io/godev/a8nats"
	"accur8.io/godev/hermes/hproto"
	"accur8.io/godev/hermes/model"
	"github.com/nats-io/nats.go"
	"github.com/palantir/stacktrace"
	"google.golang.org/protobuf/proto"
)

var (
	RpcSent  = model.NewChannelName("rpc-sent")
	RpcInbox = model.NewChannelName("rpc-inbox")
)

var RpcChannels = []model.ChannelName{RpcSent, RpcInbox}
var RpcChannelsAsStrings = []string{RpcSent.String(), RpcInbox.String()}

type MailboxManagerI interface {
	CreateMailbox(request *hproto.CreateMailboxRequest) (MailboxI, error)
	FetchOrCreateNamedMailbox(mailboxKey MailboxAddress) (MailboxI, error)
}

type MailboxI interface {
	Keys() *MailboxKeys
	Address() MailboxAddress
	ReaderKey() ReaderKey
	AdminKey() AdminKey
	ChannelNames() []ChannelName
	ChannelNamesAsStrings() []string
	NatsSubject(channelName ChannelName) a8nats.Subject
	NatsStreamName(channelName ChannelName) a8nats.StreamName
	PublishSendMessageRequest(request *hproto.SendMessageRequest) (*hproto.SendMessageResponse, error)
	IsValidChannel(channelName ChannelName) bool
	AddChannel(channelName ChannelName, updateChannelInStore bool) error
	UpdateInStore() error
	UpdateLastActivity()
	Stream(channel ChannelName) (StreamI, error)
	HasSent() bool
	IsNamedMailbox() bool

	RegisterCorrelation(correlationId CorrelationId, callback chan *hproto.Message)
	RemoveCorrelation(correlationId CorrelationId)

	RunRpcInboxReader(ctx context.Context, rpcRequestHandler func(msg *hproto.Message) error) error
	RawRpcCall(requestBody []byte, request *RpcRequest) (*hproto.Message, error)
}

type RpcRequest struct {
	From                   MailboxI
	To                     MailboxAddress
	EndPoint               string
	Headers                map[string]string
	Context                context.Context
	Channel                ChannelName
	RequestBodyContentType hproto.ContentType
}

func RpcCall[A proto.Message, B proto.Message](reqBody A, req *RpcRequest) (*B, error) {

	reqBodyBytes, err := proto.Marshal(reqBody)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to marshal body")
	}

	responseMsg, err := RawRpcCall(reqBodyBytes, req)
	if err != nil {
		return nil, stacktrace.Propagate(err, "internal error in RawRpcCall")
	}

	responseFrameType := responseMsg.GetHeader().GetRpcHeader().GetFrameType()

	switch responseFrameType {
	case hproto.RpcFrameType_UnspecifiedRFT:
		return nil, stacktrace.NewError("unexpected frame type: %v", responseFrameType)
	case hproto.RpcFrameType_ErrorResponse:
		return nil, stacktrace.NewError("rpc error response: %v", responseMsg.GetHeader().GetRpcHeader().ErrorInfo.GetMessage())
	case hproto.RpcFrameType_SuccessResponse:

		var err error
		var response B
		responseType := reflect.TypeOf(response).Elem()
		response = reflect.New(responseType).Interface().(B)

		err = proto.Unmarshal(responseMsg.GetData(), response)
		if err != nil {
			return nil, stacktrace.Propagate(err, "failed to unmarshal response body")
		}
		return &response, nil
	default:
		return nil, stacktrace.NewError("unexpected frame type: %v", responseFrameType)
	}

}

func RawRpcCall(reqBody []byte, req *RpcRequest) (*hproto.Message, error) {

	correlationId := model.RandomCorrelationId()
	idempotentId := model.RandomIdempotentId()
	callback := make(chan *hproto.Message)

	if req.Context == nil {
		tempCtx, cancelFn := context.WithTimeout(context.Background(), 15*time.Second)
		req.Context = tempCtx
		defer cancelFn()
	}

	defer func() {
		req.From.RemoveCorrelation(correlationId)
		close(callback)
	}()

	channel := req.Channel
	if channel.IsEmpty() {
		channel = RpcInbox
	}

	contentType := req.RequestBodyContentType
	if contentType == hproto.ContentType_UnspecifiedCT {
		contentType = hproto.ContentType_Protobuf
	}

	req.From.RegisterCorrelation(correlationId, callback)
	_, err := req.From.PublishSendMessageRequest(&hproto.SendMessageRequest{
		To: []string{req.To.String()},
		Message: &hproto.Message{
			Header: &hproto.MessageHeader{
				Sender:      req.From.Address().String(),
				ContentType: contentType,
				RpcHeader: &hproto.RpcHeader{
					EndPoint:      req.EndPoint,
					FrameType:     hproto.RpcFrameType_Request,
					CorrelationId: correlationId.String(),
				},
				ExtraHeaders: hproto.ToExtraHeaders(req.Headers),
			},
			SenderEnvelope: &hproto.SenderEnvelope{
				Created: a8.NowUtcTimestamp().InEpochhMillis(),
			},
			Data: reqBody,
		},
		Channel:      channel.String(),
		IdempotentId: idempotentId.String(),
	})

	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to publish rpc request message")
	}

	select {
	case responseMsg := <-callback:
		return responseMsg, nil
	case <-req.Context.Done():
		return nil, stacktrace.NewError("rpc call timed out")
	}

}

type StreamI interface {
	ChannelName() ChannelName
	// Write(msg *hproto.Message) error
	StartPos(startPos string) StreamI
	ConsumerName(consumerName a8nats.ConsumerName) StreamI
	Subscribe(ctx context.Context, receiverFn func(*hproto.Message) error) (*nats.Subscription, error)
	RunReadLoop(ctx context.Context, receiverFn func(*hproto.Message) error) error
}

type MailboxKeys struct {
	Address   MailboxAddress
	ReaderKey ReaderKey
	AdminKey  AdminKey
}

type ChannelName = model.ChannelName
type AdminKey = model.AdminKey
type MailboxAddress = model.MailboxAddress
type ReaderKey = model.ReaderKey
type IdempotentId = model.IdempotentId
type CorrelationId = model.CorrelationId
type ProcessUid = model.ProcessUid

type ChangeDataCaptureSubjectRoot string

func (s ChangeDataCaptureSubjectRoot) String() string {
	return string(s)
}

type DatabaseName string
type TableName string

type RawAddress interface {
}
