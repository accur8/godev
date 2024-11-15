package rpcserver

import (
	"context"
	"reflect"
	"strings"

	"accur8.io/godev/hermes/api"
	"accur8.io/godev/hermes/hproto"
	"accur8.io/godev/hermes/model"
	"accur8.io/godev/log"
	"github.com/palantir/stacktrace"
	jsonpb "google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type RpcServerS struct {
	HandlerByEndPoint map[string]*HandlerS
	Sender            api.MailboxAddress
}

type HandlerS struct {
	EndPoint string
	Handler  func(msg *hproto.Message) *hproto.SendMessageRequest
	Server   *RpcServerS
}

func NewRpcServer(sender api.MailboxAddress) *RpcServerS {
	rs := &RpcServerS{
		Sender:            sender,
		HandlerByEndPoint: make(map[string]*HandlerS),
	}
	rs.RegisterHandler(
		Handler[*hproto.Ping, *hproto.Pong](
			"ping",
			func(req *hproto.Ping) (*hproto.Pong, error) {
				return &hproto.Pong{Payload: req.Payload}, nil
			},
		),
	)
	return rs
}

func (rs *RpcServerS) RegisterHandler(handler *HandlerS) {
	endPointLc := strings.ToLower(handler.EndPoint)
	rs.HandlerByEndPoint[handler.EndPoint] = handler
	rs.HandlerByEndPoint[endPointLc] = handler
}
func (rs *RpcServerS) RegisterHandlers(handler ...*HandlerS) {
	for _, h := range handler {
		rs.RegisterHandler(h)
		h.Server = rs
	}
}

func (rs *RpcServerS) ProcessRpcCall(msg *hproto.Message) *hproto.SendMessageRequest {
	log.Debug("processing rpc call hermes message %+v", msg)
	endPoint := msg.GetHeader().GetRpcHeader().GetEndPoint()
	endPointLc := strings.ToLower(endPoint)
	handler := rs.HandlerByEndPoint[endPointLc]
	if handler == nil {
		return ErrorResp(msg, stacktrace.NewError("no handler for endpoint %v", endPoint))
	}
	smr := handler.Handler(msg)
	return smr

}

func ErrorResp(msg *hproto.Message, err error) *hproto.SendMessageRequest {

	errorInfo := &hproto.RpcErrorInfo{
		ErrorCode:  0,
		Message:    err.Error(),
		StackTrace: err.Error(),
	}

	responseMsg := &hproto.Message{
		Header: &hproto.MessageHeader{
			Sender: msg.Header.Sender,
			RpcHeader: &hproto.RpcHeader{
				CorrelationId: msg.GetHeader().GetRpcHeader().GetCorrelationId(),
				EndPoint:      msg.GetHeader().GetRpcHeader().GetEndPoint(),
				FrameType:     hproto.RpcFrameType_ErrorResponse,
				ErrorInfo:     errorInfo,
			},
		},
	}

	smr := &hproto.SendMessageRequest{
		To:           []string{msg.Header.Sender},
		Channel:      api.RpcInbox.String(),
		IdempotentId: model.RandomIdempotentId().String(),
		Message:      responseMsg,
	}

	return smr

}

func Handler[Req proto.Message, Resp proto.Message](endPoint string, handler func(req Req) (Resp, error)) *HandlerS {

	handlerS := &HandlerS{
		EndPoint: endPoint,
	}

	handlerS.Handler = func(msg *hproto.Message) *hproto.SendMessageRequest {

		useProto := msg.GetHeader().GetContentType() == hproto.ContentType_Protobuf

		var err error
		var req Req
		reqType := reflect.TypeOf(req).Elem()
		req = reflect.New(reqType).Interface().(Req)

		if useProto {
			err = proto.Unmarshal(msg.Data, req)
		} else {
			if msg.Data == nil {
				msg.Data = []byte("{}")
			}
			err = jsonpb.Unmarshal(msg.Data, req)
		}
		if err != nil {
			return ErrorResp(msg, err)
		}

		if log.IsTraceEnabled {
			log.Trace("processing rpc request %s - %+v", endPoint, req)
		}

		resp, err := handler(req)
		if err != nil {
			return ErrorResp(msg, err)
		}

		var data []byte
		if useProto {
			data, err = proto.Marshal(resp)
		} else {
			data, err = jsonpb.Marshal(resp)
		}
		if err != nil {
			return ErrorResp(msg, err)
		}
		if log.IsTraceEnabled {
			log.Trace("rpc response %s -  %+v", endPoint, resp)
		}

		responseMsg := &hproto.Message{
			Header: &hproto.MessageHeader{
				Sender: handlerS.Server.Sender.String(),
				RpcHeader: &hproto.RpcHeader{
					CorrelationId: msg.GetHeader().GetRpcHeader().GetCorrelationId(),
					EndPoint:      msg.GetHeader().GetRpcHeader().GetEndPoint(),
					FrameType:     hproto.RpcFrameType_SuccessResponse,
				},
			},
			Data: data,
		}

		smr := &hproto.SendMessageRequest{
			To:           []string{msg.Header.Sender},
			IdempotentId: model.RandomIdempotentId().String(),
			Channel:      api.RpcInbox.String(),
			Message:      responseMsg,
		}

		return smr
	}

	return handlerS

}

func (rs *RpcServerS) RunMessageReaderLoop(
	ctx context.Context,
	mailbox api.MailboxI,
) error {

	msgHandler := func(msg *hproto.Message) error {
		var err error
		smr := rs.ProcessRpcCall(msg)
		if smr != nil {
			_, err = mailbox.PublishSendMessageRequest(smr)
			if err != nil {
				err = stacktrace.Propagate(err, "publishSendMessageRequest returned an error")
				log.Error("%v", err)
			}
		} else {
			err = stacktrace.Propagate(err, "ProcessRpcCall returned a nil response")
			log.Error("%v", err)
		}
		return nil
	}

	return mailbox.RunRpcInboxReader(
		ctx,
		msgHandler,
	)

}
