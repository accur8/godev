package hermes

import (
	context "context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	reflect "reflect"
	"sync"
	"syscall"
	"time"

	"accur8.io/godev/a8"
	"accur8.io/godev/a8nats"
	"accur8.io/godev/hermes/api"
	"accur8.io/godev/hermes/hproto"
	"accur8.io/godev/httpserver"
	"accur8.io/godev/log"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"
	"github.com/palantir/stacktrace"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

type ActiveWebSocket struct {
	ClientInfo     *ClientInfo
	SendLock       sync.Locker
	WebSocketConn  *websocket.Conn
	RequestContext context.Context
}

func (aws *ActiveWebSocket) Context() context.Context {
	return aws.RequestContext
}

func (aws *ActiveWebSocket) GetClientInfoX() *ClientInfo {
	return aws.ClientInfo
}

func (agrpc *ActiveGrpcSendReceive) GetClientInfoX() *ClientInfo {
	return agrpc.ClientInfo
}

func (agrpc *ActiveGrpcSendReceive) Shutdown() {
	log.Error("??? TODO implement ActiveGrpcSendReceive Shutdown")
}

func (agrpc *ActiveGrpcSendReceive) StartProtocol(acw *ActiveClientW) error {
	return nil
}

func (aws *ActiveWebSocket) SendMessage(msg *hproto.MessageToClient) error {
	// log.Debug("SendMessage %+v", aws)
	aws.SendLock.Lock()
	defer aws.SendLock.Unlock()
	var msgType int
	var msgBytes []byte
	var err error
	if aws.ClientInfo.UseJson {
		msgBytes, err = protojson.Marshal(msg)
		msgType = websocket.TextMessage
	} else {
		msgBytes, err = proto.Marshal(msg)
		msgType = websocket.BinaryMessage
	}
	if err != nil {
		return err
	}
	return aws.WebSocketConn.WriteMessage(msgType, msgBytes)
}

type ActiveGrpcSendReceive struct {
	ClientInfo *ClientInfo
	GrpcStream hproto.HermesService_SendReceiveServer
}

func (agrpc *ActiveGrpcSendReceive) Context() context.Context {
	return agrpc.GrpcStream.Context()
}

func (agrpc *ActiveGrpcSendReceive) GetClientInfo() *ClientInfo {
	return agrpc.ClientInfo
}

func (agrpc *ActiveGrpcSendReceive) SendMessage(msg *hproto.MessageToClient) error {
	return agrpc.GrpcStream.Send(msg)
}
func (agrpc *ActiveGrpcSendReceive) ReadMessage() (*hproto.MessageFromClient, error) {
	return agrpc.GrpcStream.Recv()
}

type ClientInfo struct {
	Uuid              *uuid.UUID
	Mailbox           *Mailbox
	SubscriptionsById a8.SyncMap[string, []*ClientSubscription]
	LastActivity      a8.UtcTimestamp
	IsShutdown        bool
	IsClosed          bool
	AddressVerified   bool
	FirstMessage      *hproto.FirstMessage
	UseJson           bool
}

type ClientSubscription struct {
	Id                    string
	Uid                   a8.Uid
	ProtoSubscriptionJson json.RawMessage
	NatsSubscription      *nats.Subscription
	Subject               a8nats.Subject
	StreamName            a8nats.StreamName
	StartSeq              string
}

type ActiveClient interface {
	GetClientInfoX() *ClientInfo
	StartProtocol(activeClientW *ActiveClientW) error
	ReadMessage() (*hproto.MessageFromClient, error)
	SendMessage(*hproto.MessageToClient) error
	Shutdown()
	IsClosed() bool
	Context() context.Context
}

type ActiveClientW struct {
	Client          ActiveClient
	GlobalServices0 *Services
	Mutex           sync.Mutex
}

func (acw *ActiveClientW) AddSubscription(sub *ClientSubscription) {
	acw.Mutex.Lock()
	defer acw.Mutex.Unlock()
	subs, _ := acw.SubscriptionsById().Load(sub.Id)
	subs = append(subs, sub)
	acw.SubscriptionsById().Store(sub.Id, subs)
}

func (acw *ActiveClientW) SubscriptionsById() a8.SyncMap[string, []*ClientSubscription] {
	return acw.Client.GetClientInfoX().SubscriptionsById
}

func (acw *ActiveClientW) IsAddressVerified() bool {
	return acw.Client.GetClientInfoX().AddressVerified
}

func (acw *ActiveClientW) GlobalServices() *Services {
	return acw.GlobalServices0
}

func (acw *ActiveClientW) Mailbox() *Mailbox {
	return acw.Client.GetClientInfoX().Mailbox
}

func (acw *ActiveClientW) IsClosed() bool {
	return acw.Client.IsClosed()
}

func (acw *ActiveClientW) Uuid() *uuid.UUID {
	return acw.Client.GetClientInfoX().Uuid
}

func (acw *ActiveClientW) UuidStr() string {
	return acw.Client.GetClientInfoX().Uuid.String()
}

func (acw *ActiveClientW) StartProtocol() error {

	if log.IsTraceEnabled {
		log.Debug("StartProtocol %v", acw.UuidStr())
	}

	msg, err := acw.Client.ReadMessage()
	if err != nil {
		return stacktrace.Propagate(err, "unable to start reader")
	}

	switch m := msg.GetTestOneof().(type) {
	case *hproto.MessageFromClient_FirstMessage:
		firstMessage := m.FirstMessage

		readerKey := firstMessage.GetSenderInfo().ReaderKeyO()
		// log.Debug("acw %+v", acw)
		// log.Debug("acw %#v", acw)
		log.Debug("acw.GlobalServices %#v", acw.GlobalServices())
		mbox := acw.GlobalServices().MailboxStore.FetchMailboxByReaderKey(readerKey)
		if mbox == nil {
			errorMessage := fmt.Sprintf("error mbox %s not found", readerKey)
			acw.SendNotifcation(errorMessage)
			return stacktrace.NewError(errorMessage)
		}

		ci := acw.Client.GetClientInfoX()

		ci.Mailbox = mbox

		address := firstMessage.GetSenderInfo().AddressO()
		if mbox.Address() == address {
			ci.AddressVerified = true
		}
		ci.FirstMessage = firstMessage

		if log.IsTraceEnabled {
			log.Trace("received first message %v", a8.ToJson(firstMessage))
		}
		err := acw.Client.StartProtocol(acw)
		if err != nil {
			return stacktrace.Propagate(err, "error starting client protocol")
		}

		acw.UpdateLastActivity()

		acw.GlobalServices().Register(acw)

		for _, sub := range firstMessage.Subscriptions {
			if sub.GetMailbox() != nil {
				err := RegisterMailboxSub(acw, sub.GetMailbox())
				if err != nil {
					log.Debug("error registering mailbox sub %s - %v", err, a8.ToProtoJson(sub.GetMailbox()))
				}
			}
			if sub.GetNefario() != nil {
				err := RegisterNefarioSub(acw, sub.GetNefario())
				if err != nil {
					log.Debug("error registering nefario sub %s - %v", err, a8.ToProtoJson(sub.GetNefario()))
				}
			}
			if sub.GetUnsubscribe() != nil {
				err := UnRegisterSub(acw, sub.GetUnsubscribe().Id)
				if err != nil {
					log.Debug("error UnRegisterSub %s - %v", err, a8.ToProtoJson(sub.GetUnsubscribe()))
				}
			}
		}

		return runClientReader(acw)

	default:
		return stacktrace.NewError("expected firstMessage and instead received unexpected received %v", msg)
	}

}

func (acw *ActiveClientW) SendNotifcation(message string) error {
	msg := &hproto.MessageToClient{
		TestOneof: &hproto.MessageToClient_Notification{
			Notification: &hproto.Notification{
				Message: message,
			},
		},
	}
	return acw.Client.SendMessage(msg)
}

func (acw *ActiveClientW) UpdateLastActivity() {
	now := a8.NowUtcTimestamp()
	ci := acw.Client.GetClientInfoX()
	ci.Mailbox.UpdateLastActivity()
	ci.LastActivity = now
}

func (aws *ActiveWebSocket) StartProtocol(acw *ActiveClientW) error {

	delegatePongHandler := aws.WebSocketConn.PongHandler()
	delegatePingHandler := aws.WebSocketConn.PingHandler()

	aws.WebSocketConn.SetPongHandler(func(appData string) error {
		log.Debug("received ws pong - %v", aws.ClientInfo.Uuid)
		acw.UpdateLastActivity()
		return delegatePongHandler(appData)
	})
	aws.WebSocketConn.SetPingHandler(func(appData string) error {
		log.Debug("received ws ping - %v", aws.ClientInfo.Uuid)
		acw.UpdateLastActivity()
		return delegatePingHandler(appData)
	})

	return nil
}

func (aws *ActiveWebSocket) ReadMessage() (*hproto.MessageFromClient, error) {
	msgType, msgBytes, err := aws.WebSocketConn.ReadMessage()
	if err != nil {
		switch err.(type) {
		case *websocket.CloseError:
			aws.ClientInfo.IsClosed = true
			return nil, stacktrace.NewError("web socket is closed")
		default:
			netErr, ok := err.(net.Error)
			if ok {
				// we will not receive timeout's here
				if netErr.Timeout() {
					log.Debug("received net.Error Timeout this should not happen")
				} else {
					aws.ClientInfo.IsClosed = true
					return nil, stacktrace.NewError("web socket is closed")
				}
			} else {
				log.Debug("error wsConn.ReadMessage() %s - %s", reflect.TypeOf(err), err)
				log.Debug("classifyNetworkError = %s", classifyNetworkError(err))
			}
		}
	}
	// log.Trace("read message from web socket")
	msg := &hproto.MessageFromClient{}
	switch msgType {
	case websocket.PongMessage:
		log.Debug("received pong")
	case websocket.PingMessage:
		sendPong := func() {
			aws.SendLock.Lock()
			defer aws.SendLock.Unlock()
			aws.WebSocketConn.WriteControl(websocket.PongMessage, msgBytes, time.Now().Add(5*time.Second))
		}
		sendPong()
		log.Debug("received ping and sent pong")
	case websocket.BinaryMessage:
		err = proto.Unmarshal(msgBytes, msg)
		if err != nil {
			err = stacktrace.Propagate(err, "error unmarshalling binary message")
			return nil, err
		}
	case websocket.TextMessage:
		// log.Trace("unmasrshalling json message %s", string(msgBytes))
		err = protojson.Unmarshal(msgBytes, msg)
		if err != nil {
			err = stacktrace.Propagate(err, "error unmarshalling text message - %v", string(msgBytes))
			return nil, err
		}
	}
	if err != nil {
		err = stacktrace.Propagate(err, "catchall error this should not happen")
		return nil, err
	}
	log.Trace("read message from web socket - %v", a8.ToJson(msg))
	return msg, nil
}

func classifyNetworkError(err error) string {
	cause := err
	for {
		// Unwrap was added in Go 1.13.
		// See https://github.com/golang/go/issues/36781
		if unwrap, ok := cause.(interface{ Unwrap() error }); ok {
			cause = unwrap.Unwrap()
			continue
		}
		break
	}

	// DNSError.IsNotFound was added in Go 1.13.
	// See https://github.com/golang/go/issues/28635
	if cause, ok := cause.(*net.DNSError); ok && cause.Err == "no such host" {
		return "name not found"
	}

	if cause, ok := cause.(syscall.Errno); ok {
		if cause == 10061 || cause == syscall.ECONNREFUSED {
			return "connection refused"
		}
	}

	if cause, ok := cause.(net.Error); ok && cause.Timeout() {
		return "timeout"
	}

	return fmt.Sprintf("unknown network error: %s", err)
}

func BuildSendReceiveWsHandler(services *Services, useJson bool) func(w http.ResponseWriter, r *http.Request) {

	return func(w http.ResponseWriter, r *http.Request) {

		httpserver.SetCorsHeaders(w)
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		uuid, err := uuid.NewRandom()
		if err != nil {
			panic(err)
		}

		upgrader := websocket.Upgrader{ReadBufferSize: 8 * 1024, WriteBufferSize: 8 * 1024}
		upgrader.Error = func(w http.ResponseWriter, r *http.Request, status int, reason error) {
			// don't return errors to maintain backwards compatibility
			log.Debug("error upgrading %v", uuid)
		}
		upgrader.CheckOrigin = func(r *http.Request) bool {
			// allow all connections by default
			return true
		}

		wsConn, err := upgrader.Upgrade(w, r, w.Header())
		if err != nil {
			HttpError(w, "Could not upgrade to websocket connection", http.StatusBadRequest)
			return
		}

		activeWebSocket := &ActiveWebSocket{
			WebSocketConn: wsConn,
			ClientInfo: &ClientInfo{
				Uuid:              &uuid,
				LastActivity:      a8.NowUtcTimestamp(),
				UseJson:           useJson,
				SubscriptionsById: a8.NewSyncMap[string, []*ClientSubscription](),
			},
			SendLock:       &sync.Mutex{},
			RequestContext: r.Context(),
		}

		// send websocket pings every 30 seconds
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-r.Context().Done():
					return
				case <-ticker.C:
					log.Debug("sending ping - %v", uuid)
					err := wsConn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(5*time.Second))
					if err != nil {
						err = stacktrace.Propagate(err, "error sending ping")
						log.Debug(err.Error())
					}
				}
			}
		}()

		activeW := &ActiveClientW{
			Client:          activeWebSocket,
			GlobalServices0: services,
		}

		activeW.StartProtocol()
	}

}

func Subjects(rm *hproto.RecordMatcher, root api.ChangeDataCaptureSubjectRoot) []a8nats.Subject {

	subjectImpl := func(pk *structpb.Value) a8nats.Subject {
		suffix := ".>"
		if pk != nil {
			suffix = fmt.Sprintf(".%s", pk)
		}
		return a8nats.NewSubject(fmt.Sprintf("%s.%s.%s%s", root, rm.Database, rm.Table, suffix))
	}

	if len(rm.PrimaryKeys) == 0 {
		return []a8nats.Subject{subjectImpl(nil)}
	} else {
		subjects := make([]a8nats.Subject, len(rm.PrimaryKeys))
		for _, pk := range rm.PrimaryKeys {
			subjects = append(subjects, subjectImpl(pk))
		}
		return subjects
	}

}

func RegisterChangeDataCaptureSub(acw *ActiveClientW, sub *hproto.ChangeDataCaptureSubscription) error {

	subjects := []a8nats.Subject{}
	for _, rm := range sub.Matchers {
		subjects = append(subjects, Subjects(rm, "wal_listener")...)
	}

	errs := []error{}

	for _, subject := range subjects {
		clientSubscription := &ClientSubscription{
			Id:                    sub.Id,
			ProtoSubscriptionJson: json.RawMessage(a8.ToProtoJson(sub)),
			Subject:               subject,
			StreamName:            a8nats.NewStreamName("wal_listener"),
			StartSeq:              sub.StartSeq,
		}
		err := RegisterClientSub(acw, clientSubscription)
		if err != nil {
			err = stacktrace.Propagate(err, "error RegisterClientSub")
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)

}

func RegisterMailboxSub(acw *ActiveClientW, sub *hproto.MailboxSubscription) error {

	mbox := acw.Mailbox().mailboxStore.FetchMailboxByReaderKey(sub.ReaderKeyO())
	if mbox == nil {
		return stacktrace.NewError("error mbox reader key %s not found ", sub.ReaderKey)
	}

	clientSubscription := &ClientSubscription{
		Id:                    sub.Id,
		ProtoSubscriptionJson: json.RawMessage(a8.ToProtoJson(sub)),
		Subject:               mbox.NatsSubject(sub.ChannelO()),
		StreamName:            mbox.NatsStreamName(sub.ChannelO()),
		StartSeq:              sub.StartSeq,
	}

	return RegisterClientSub(acw, clientSubscription)

}

func UnRegisterSub(acw *ActiveClientW, subscriptionId string) error {

	clientSubs, ok := acw.SubscriptionsById().Load(subscriptionId)
	if ok {
		for _, clientSub := range clientSubs {
			log.Debug("unsubscribe %s - %s", acw.UuidStr(), subscriptionId)
			err := clientSub.NatsSubscription.Unsubscribe()
			if err != nil {
				err = stacktrace.Propagate(err, "error unsubscribing %s", subscriptionId)
				log.Debug(err.Error())
			}
		}

		acw.SubscriptionsById().Delete(subscriptionId)
		return nil
	} else {
		return stacktrace.NewError("subscriptionId %s not found", subscriptionId)
	}

}

func RegisterNefarioSub(acw *ActiveClientW, sub *hproto.NefarioSubscription) error {

	subject := a8nats.NewSubject(fmt.Sprintf("launchy.%s.%s", sub.ProcessUid, sub.Channel))
	streamName := a8nats.NewStreamName(fmt.Sprintf("launchy-%s-%s", sub.ProcessUid, sub.Channel))

	clientSubscription := &ClientSubscription{
		Id:                    sub.Id,
		ProtoSubscriptionJson: json.RawMessage(a8.ToProtoJson(sub)),
		Subject:               subject,
		StreamName:            streamName,
		StartSeq:              sub.StartSeq,
	}

	return RegisterClientSub(acw, clientSubscription)

}

func RegisterClientSub(acw *ActiveClientW, clientSubscription *ClientSubscription) error {

	subscriptionId := clientSubscription.Id
	subject := clientSubscription.Subject
	streamName := clientSubscription.StreamName
	startSeq := clientSubscription.StartSeq

	if log.IsTraceEnabled {
		log.Trace("RegisterClientSender %v", acw.UuidStr())
	}

	messageHandler := func(natsMsg *nats.Msg) error {
		if log.IsTraceEnabled {
			log.Trace("received nats msg subject: %s", subject)
		}
		metadata, err := natsMsg.Metadata()
		if err != nil {
			log.Error("Unable to read metadata %s", err)
			return nil
		}
		// msgHeader := MessageHeader{Sequence: metadata.Sequence.Stream, Created: &EpochMills{Value: NowUnixEpochMillis()}}
		msg := &hproto.MessageToClient{
			TestOneof: &hproto.MessageToClient_MessageEnvelope{
				MessageEnvelope: &hproto.MessageEnvelope{
					MessageBytes: natsMsg.Data,
					ServerEnvelope: &hproto.ServerEnvelope{
						Sequence:       metadata.Sequence.Stream,
						Created:        metadata.Timestamp.UnixMilli(),
						SubscriptionId: subscriptionId,
					},
				},
			},
		}

		err = acw.Client.SendMessage(msg)
		if err != nil {
			netErr, ok := err.(net.Error)
			if ok {
				// we will not receive timeout's here
				if netErr.Timeout() {
					log.Debug("received net.Error Timeout this should not happen")
				}
			} else {
				log.Debug("error writing to client %v - %v - %v", reflect.TypeOf(err), classifyNetworkError(err), err)
			}
			if clientSubscription.NatsSubscription != nil {
				log.Debug("shutting down subscription %s for %v", subscriptionId, acw.Uuid())
				clientSubscription.NatsSubscription.Unsubscribe()
			}
		}
		return nil
	}

	go func() {
		a8nats.RunWorkQueue(
			acw.Client.Context(),
			&a8nats.WorkQueueConfig{
				Subject:      subject,
				Nats:         a8nats.ConnArgs{NatsConn: acw.GlobalServices().Nats()},
				StreamName:   streamName,
				CreateStream: false,
				AssumeProto:  true,
				StartSeq:     startSeq,
				BatchSize:    100,
			},
			messageHandler,
		)
	}()

	// startSeqDesc, subscriptionOptions := a8nats.StartSeqToSubscriptionOptions(startSeq)

	// natsSub, err := acw.GlobalServices().Nats().JetStream().Subscribe(
	// 	subject,
	// 	messageHandler,
	// 	nats.BindStream(streamName),
	// 	subscriptionOptions,
	// 	nats.Context(acw.Client.Context()),
	// 	nats.InactiveThreshold(24*time.Hour),
	// )
	// if err != nil {
	// 	return stacktrace.Propagate(err, "unable to subscribe %v", err)
	// }
	// clientSubscription.NatsSubscription = natsSub
	acw.AddSubscription(clientSubscription)

	log.Debug("RegisterClientSub subject: %s  stream: %s  startSeq: %v for %v", subject, streamName, startSeq, acw.UuidStr())

	return nil

}

func runClientReader(activeClient *ActiveClientW) error {

	var context = activeClient.Uuid().String()

	defer func() {
		log.Debug("client reader %v completed", context)
	}()

	log.Debug("client reader %v started", context)

	for {
		if log.IsTraceEnabled {
			log.Trace("reading websocket")
		}
		msg, err := activeClient.Client.ReadMessage()
		if err != nil {
			if activeClient.IsClosed() {
				log.Debug("normal operation client close detected")
			} else {
				err = stacktrace.Propagate(err, "error reading from active client")
				log.Error("error closing client %v", err)
			}
			return err
		}
		if log.IsTraceEnabled {
			log.Trace("received message from client %v", a8.ToProtoJson(msg))
		}

		activeClient.UpdateLastActivity()

		fromMbox := activeClient.Mailbox()

		activeClient.UpdateLastActivity()

		switch m := msg.GetTestOneof().(type) {
		case *hproto.MessageFromClient_SendMessageRequest:
			if log.IsTraceEnabled {
				json := a8.ToJson(m.SendMessageRequest)
				log.Debug("received SendMessageRequest -- %s", json)
			}
			if activeClient.IsAddressVerified() {
				response, err := fromMbox.PublishSendMessageRequest(m.SendMessageRequest)
				if err != nil {
					err = stacktrace.Propagate(err, "error PublishSendMessageRequest")
					log.Error(err.Error())
				}
				m2c := &hproto.MessageToClient{
					TestOneof: &hproto.MessageToClient_SendMessageResponse{
						SendMessageResponse: response,
					},
				}
				activeClient.Client.SendMessage(m2c)
			} else {
				activeClient.SendNotifcation("cannot process SendMessageRequest no address has been sent in the FirstMessge")
			}
		case *hproto.MessageFromClient_Ping:
			if log.IsTraceEnabled {
				json := a8.ToJson(m.Ping)
				log.Trace("received Ping request -- %s", json)
			}
			go func() {
				pong := &hproto.Pong{Payload: m.Ping.Payload}
				msg := &hproto.MessageToClient{
					TestOneof: &hproto.MessageToClient_Pong{
						Pong: pong,
					},
				}
				activeClient.Client.SendMessage(msg)
			}()
		case *hproto.MessageFromClient_SubscribeRequest:
			resp := activeClient.RegisterSubscriptions(m.SubscribeRequest)
			err := activeClient.Client.SendMessage(
				&hproto.MessageToClient{
					TestOneof: &hproto.MessageToClient_SubscribeResponse{
						SubscribeResponse: resp,
					},
				},
			)
			if err != nil {
				err = stacktrace.Propagate(err, "error sending SubscribeResponse")
				log.Debug(err.Error())
			}
		case *hproto.MessageFromClient_Pong:
			if log.IsTraceEnabled {
				json := a8.ToJson(m.Pong)
				log.Trace("received Pong request -- %s", json)
			}
		case *hproto.MessageFromClient_Notification:
			log.Debug("received notification from client %s", m.Notification.Message)
		default:
			log.Error("this should not happen unexpected message received %v", msg)
		}

	}
}

func (aws *ActiveWebSocket) IsClosed() bool {
	return aws.ClientInfo.IsClosed
}

func (agrpc *ActiveGrpcSendReceive) IsClosed() bool {
	if agrpc.ClientInfo.IsClosed {
		return true
	} else {
		select {
		case <-agrpc.GrpcStream.Context().Done():
			agrpc.ClientInfo.IsClosed = true
			return true
		default:
			return false
		}
	}
}

func (aws *ActiveWebSocket) Shutdown() {
	// ??? TODO get this working again (need to lock before the write)
	// if !aws.ClientInfo.IsClosed {
	// 	closeMessage := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "closing websocket as it has timedout")
	// 	// err := aws.WebSocketConn.WriteMessage(websocket.CloseMessage, closeMessage)
	// 	if err != nil {
	// 		log.Debug("unable to send websocket close message will force close the underlying connection - %s", err)
	// 	}

	// 	err = aws.WebSocketConn.Close()
	// 	if err != nil {
	// 		log.Debug("unable to force close underlying tcp socket for websocket - %s", err)
	// 	}
	// }
}

func (acw *ActiveClientW) Shutdown() {
	if acw.Client.GetClientInfoX().IsShutdown {
		return
	}
	acw.Client.GetClientInfoX().IsShutdown = true
	log.Debug("shutting down ActiveClient %v", acw.Uuid())

	unsubscribe := func(key string, subs []*ClientSubscription) bool {
		for _, sub := range subs {
			if sub != nil && sub.NatsSubscription.IsValid() {
				err := sub.NatsSubscription.Unsubscribe()
				if err != nil {
					log.Debug("error unsubscribing - %s", err)
				}
			}
		}
		return true
	}
	acw.SubscriptionsById().Range(unsubscribe)

	acw.Client.Shutdown()

}

func HttpError(w http.ResponseWriter, error string, code int) {
	log.Debug("responding with http error %d - %s", code, error)
	http.Error(w, error, code)
}

func (aw *ActiveClientW) RegisterSubscriptions(req *hproto.SubscribeRequest) *hproto.SubscribeResponse {
	var response = hproto.SubscribeResponse{}
	for _, sub := range req.GetSubscriptions() {
		var err error
		var state string
		if sub.GetChangeDataCapture() != nil {
			state = sub.GetChangeDataCapture().GetState()
			err = RegisterChangeDataCaptureSub(aw, sub.GetChangeDataCapture())
		} else if sub.GetMailbox() != nil {
			state = sub.GetMailbox().GetState()
			err = RegisterMailboxSub(aw, sub.GetMailbox())
		} else if sub.GetNefario() != nil {
			state = sub.GetNefario().GetState()
			err = RegisterNefarioSub(aw, sub.GetNefario())
		} else {
			err = stacktrace.NewError("unknown subscription type %+v", sub)
		}
		if err != nil {
			response.Errors = append(response.Errors, &hproto.SubscribeError{State: state, Message: err.Error()})
		} else {
			response.Succeeded = append(response.Succeeded, state)
		}
	}
	return &response
}
