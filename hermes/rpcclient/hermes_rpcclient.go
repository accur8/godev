package rpcclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"reflect"
	"sync"

	"accur8.io/godev/a8"
	"accur8.io/godev/hermes/api"
	"accur8.io/godev/hermes/hproto"
	"accur8.io/godev/hermes/model"

	// . "accur8.io/godev/hermes"
	"accur8.io/godev/jsonrpc"
	"accur8.io/godev/log"
	"github.com/gorilla/websocket"
	"github.com/palantir/stacktrace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	jsonpb "google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type HermesClient interface {
	ClientMailbox() *ClientMailbox
	// this can return nil, nil in circumstances where the message actually read was handled
	ReadMessage() (*hproto.MessageToClient, error)
	Send(msg *hproto.MessageFromClient) error
	// SendPing(*Ping) (*Pong, error)
	// SendPong(*Pong) error
}

type HermesClientW struct {
	Client HermesClient
}

func MarshalResponse[A proto.Message](requestMsg *hproto.Message, responseA *A, responseErr error) (*hproto.SendMessageRequest, error) {

	contentType := requestMsg.GetHeader().GetContentType()

	var err error
	var bytes []byte

	if responseA != nil {
		switch contentType {
		case hproto.ContentType_Protobuf:
			bytes, err = proto.Marshal(*responseA)
			if err != nil {
				return nil, stacktrace.Propagate(err, "unable to unmarshal proto message")
			}
		case hproto.ContentType_Json:
			bytes, err = jsonpb.Marshal(*responseA)
			if err != nil {
				return nil, stacktrace.Propagate(err, "unable to unmarshal proto message")
			}
		default:
			return nil, stacktrace.NewError("invalid content type for rpc %v", contentType)
		}
	}

	sendMessageRequest := &hproto.SendMessageRequest{
		To:           []string{requestMsg.GetHeader().GetSender()},
		Channel:      requestMsg.GetServerEnvelope().GetChannel(),
		IdempotentId: model.RandomIdempotentId().String(),
		Message: &hproto.Message{
			Header: &hproto.MessageHeader{
				ContentType: contentType,
				RpcHeader: &hproto.RpcHeader{
					CorrelationId: requestMsg.GetHeader().GetRpcHeader().GetCorrelationId(),
					EndPoint:      requestMsg.GetHeader().GetRpcHeader().GetEndPoint(),
				},
			},
			SenderEnvelope: &hproto.SenderEnvelope{
				Created: a8.NowUtcTimestamp().InEpochhMillis(),
			},
			Data: bytes,
		},
	}

	if responseErr != nil {
		sendMessageRequest.Message.Header.RpcHeader.FrameType = hproto.RpcFrameType_ErrorResponse
		sendMessageRequest.Message.Header.RpcHeader.ErrorInfo = &hproto.RpcErrorInfo{
			Message: responseErr.Error(),
		}
	}

	if responseA != nil {
		sendMessageRequest.Message.Header.RpcHeader.FrameType = hproto.RpcFrameType_SuccessResponse
		sendMessageRequest.Message.Header.RpcHeader.ErrorInfo = nil
	}

	return sendMessageRequest, nil

}

func UnmarshalRequest[A proto.Message](msg *hproto.Message) (*A, error) {
	contentType := msg.GetHeader().GetContentType()

	var a *A
	aType := reflect.TypeOf(a).Elem()
	aPtr := reflect.New(aType)
	a = aPtr.Elem().Interface().(*A)
	var err error

	switch msg.GetHeader().GetContentType() {
	case hproto.ContentType_Protobuf:
		err = proto.Unmarshal(msg.Data, *a)
		if err != nil {
			err = stacktrace.Propagate(err, "unable to unmarshal proto message")
		}
	case hproto.ContentType_Json:
		err = jsonpb.Unmarshal(msg.Data, *a)
		if err != nil {
			err = stacktrace.Propagate(err, "unable to unmarshal proto message")
		}
	default:
		return nil, stacktrace.NewError("invalid content type for rpc %v", contentType)
	}
	return a, err
}

func (hcw *HermesClientW) ReadMessage() (*hproto.Message, error) {
	for {
		msg, err := hcw._ReadMessageImpl()
		if err != nil || msg != nil {
			return msg, err
		}
	}
}

func (hcw *HermesClientW) _ReadMessageImpl() (*hproto.Message, error) {
	m2c, err := hcw.Client.ReadMessage()
	if err != nil {
		return nil, stacktrace.Propagate(err, "error reading from client")
	}
	if m2c == nil {
		return nil, nil
	}

	switch m := m2c.GetTestOneof().(type) {
	case *hproto.MessageToClient_Ping:
		log.Debug("received ping and will respond %v", m.Ping)
		hcw.Client.Send(&hproto.MessageFromClient{
			TestOneof: &hproto.MessageFromClient_Pong{
				Pong: &hproto.Pong{Payload: m.Ping.Payload},
			},
		})
		return nil, nil
	case *hproto.MessageToClient_SendMessageResponse:
		log.Debug("received SendMessageResponse %+v", m.SendMessageResponse)
		return nil, nil
	case *hproto.MessageToClient_Pong:
		log.Debug("received pong %v", m.Pong)
		return nil, nil
	case *hproto.MessageToClient_Notification:
		log.Debug("received notification %v", m.Notification.Message)
		return nil, nil
	case *hproto.MessageToClient_MessageEnvelope:
		me := m.MessageEnvelope
		msg := &hproto.Message{}
		err := proto.Unmarshal(me.MessageBytes, msg)
		if err != nil {
			log.Debug("error unmarshalling message %v will drop the message", err)
			return nil, nil
		}
		msg.ServerEnvelope = me.ServerEnvelope
		return msg, nil
	default:
		log.Error("%s", stacktrace.NewError("this should never happen"))
		return nil, nil
	}

}

func (hcw *HermesClientW) SendMessageRequest(smr *hproto.SendMessageRequest) error {
	msg := &hproto.MessageFromClient{
		TestOneof: &hproto.MessageFromClient_SendMessageRequest{
			SendMessageRequest: smr,
		},
	}
	return hcw.Client.Send(msg)
}

func (hcw *HermesClientW) SendFirstMessage(fm *hproto.FirstMessage) error {
	msg := &hproto.MessageFromClient{
		TestOneof: &hproto.MessageFromClient_FirstMessage{
			FirstMessage: fm,
		},
	}
	return hcw.Client.Send(msg)
}

type ClientMailbox = hproto.CreateMailboxResponse

type RpcClient struct {
	listeners     a8.SyncMap[string, chan *hproto.Message]
	serverChannel model.ChannelName
	hermesClient  *HermesClientW
	// hermesAdminRpc hermesAdminRpc
	config *RpcClientConfig
}

func (rc *RpcClient) ClientMailbox() ClientMailbox {
	return *rc.hermesClient.Client.ClientMailbox()
}

type RpcClientConfig struct {
	HermesRootUrl *jsonrpc.Url
	UseGrpc       bool
	GrpcEncrypt   bool
	RpcChannel    model.ChannelName
	Context       context.Context
	ExtraChannels []model.ChannelName
}

const (
	HermesClientType_WebSocket = iota
	HermesClientType_Grpc
)

type hermesAdminRpc struct {
	CreateMailbox func(*hproto.CreateMailboxRequest) (*hproto.CreateMailboxResponse, error)
	AddChannel    func(*hproto.AddChannelRequest) (*hproto.AddChannelResponse, error)
}

type hermesWebSocket struct {
	clientMailbox *ClientMailbox
	wsConn        *websocket.Conn
	writerMutex   sync.Mutex
	readerMutex   sync.Mutex
}

type hermesGrpc struct {
	client        hproto.HermesService_SendReceiveClient
	clientMailbox *ClientMailbox
}

func (hg *hermesGrpc) ClientMailbox() *ClientMailbox {
	return hg.clientMailbox
}

func (hws *hermesWebSocket) _ReadWebSocket() ([]byte, error) {
	hws.readerMutex.Lock()
	defer hws.readerMutex.Unlock()
	_, messageBytes, err := hws.wsConn.ReadMessage()
	return messageBytes, err
}

func (hws *hermesWebSocket) ClientMailbox() *ClientMailbox {
	return hws.clientMailbox
}
func (hws *hermesWebSocket) _WriteWebSocket(message *hproto.MessageFromClient) error {
	messageBytes, err := proto.Marshal(message)
	if err != nil {
		return stacktrace.Propagate(err, "error marshalling MessageFromClient to bytes")
	}
	return hws._WriteWebSocketBytes(messageBytes)
}

func (hws *hermesWebSocket) _WriteWebSocketBytes(messageBytes []byte) error {
	hws.writerMutex.Lock()
	defer hws.writerMutex.Unlock()
	return hws.wsConn.WriteMessage(websocket.BinaryMessage, messageBytes)
}

func (hg *hermesGrpc) ReadMessage() (*hproto.MessageToClient, error) {
	return hg.client.Recv()
}

func (hg *hermesGrpc) Send(msg *hproto.MessageFromClient) error {
	return hg.client.Send(msg)
}

func (hws *hermesWebSocket) ReadMessage() (*hproto.MessageToClient, error) {
	msgData, err := hws._ReadWebSocket()
	if err != nil {
		stacktrace.Propagate(err, "unable to read web socket message")
	}

	msg := &hproto.MessageToClient{}
	err = proto.Unmarshal(msgData, msg)
	if err != nil {
		log.Debug("error unmarhsalling message - %v", err)
		return nil, nil
	}

	return msg, nil

}

func (hws *hermesWebSocket) Send(msg *hproto.MessageFromClient) error {
	return hws._WriteWebSocket(msg)
}

func NewHermesClient(config *RpcClientConfig) (*HermesClientW, error) {

	var client HermesClient
	var err error
	if config.UseGrpc {
		client, err = newHermesGrpcClient(config)
	} else {
		client, err = newHermesWebSocketClient(config)
	}

	if err != nil {
		return nil, stacktrace.Propagate(err, "error creating hermes client")
	}

	clientMailbox := client.ClientMailbox()

	sub0 := &hproto.Subscription{
		TestOneof: &hproto.Subscription_Mailbox{
			Mailbox: &hproto.MailboxSubscription{
				Id:        config.RpcChannel.String(),
				Channel:   config.RpcChannel.String(),
				ReaderKey: clientMailbox.ReaderKey,
				StartSeq:  "first",
			},
		},
	}

	firstMessage := &hproto.MessageFromClient{
		TestOneof: &hproto.MessageFromClient_FirstMessage{
			FirstMessage: &hproto.FirstMessage{
				SenderInfo: &hproto.SenderInfo{
					ReaderKey: clientMailbox.ReaderKey,
					Address:   clientMailbox.Address,
				},
				Subscriptions: []*hproto.Subscription{sub0},
			},
		},
	}

	err = client.Send(firstMessage)
	if err != nil {
		return nil, stacktrace.Propagate(err, "error sending first message")
	}

	return &HermesClientW{Client: client}, nil

}

func newHermesWebSocketClient(config *RpcClientConfig) (HermesClient, error) {

	jsonRpcClient := jsonrpc.NewJsonRpcClient(config.HermesRootUrl, nil)

	hermesAdminRpc := hermesAdminRpc{
		CreateMailbox: jsonrpc.NewJsonRpcCaller[hproto.CreateMailboxRequest, hproto.CreateMailboxResponse]("/api/create_mailbox", jsonRpcClient, func() *hproto.CreateMailboxResponse { return &hproto.CreateMailboxResponse{} }),
		AddChannel:    jsonrpc.NewJsonRpcCaller[hproto.AddChannelRequest, hproto.AddChannelResponse]("/api/add_channel", jsonRpcClient, func() *hproto.AddChannelResponse { return &hproto.AddChannelResponse{} }),
	}

	channels := []string{config.RpcChannel.String()}
	for _, ch := range config.ExtraChannels {
		channels = append(channels, ch.String())
	}

	clientMailbox, err := hermesAdminRpc.CreateMailbox(&hproto.CreateMailboxRequest{Channels: channels})
	if err != nil {
		return nil, err
	}

	wsUrl := *config.HermesRootUrl

	wsUrl = *jsonrpc.AppendPath(&wsUrl, "/api/ws/send_receive_proto")

	if wsUrl.Scheme == "http" {
		wsUrl.Scheme = "ws"
	} else if wsUrl.Scheme == "https" {
		wsUrl.Scheme = "wss"
	}

	wsUrlStr := wsUrl.String()

	log.Debug("connecting to web socket %v", wsUrlStr)
	wsConn, _, err := websocket.DefaultDialer.Dial(wsUrlStr, nil)
	if err != nil {
		log.Error("wsReaderConn: %v", err)
		return nil, err
	}

	return &hermesWebSocket{wsConn: wsConn, clientMailbox: clientMailbox}, nil
}

func NewMailboxServicesClientViaNginx(ctx context.Context, hermesRootUrl *jsonrpc.Url) (hproto.HermesServiceClient, error) {
	var opts []grpc.DialOption

	certPool, err := x509.SystemCertPool()
	if err != nil {
		return nil, stacktrace.Propagate(err, "unable to get system cert pool")
	}
	// Create the credentials and return it
	config := &tls.Config{
		RootCAs: certPool,
	}
	tlsCredentials := credentials.NewTLS(config)

	opts = append(opts, grpc.WithTransportCredentials(tlsCredentials))
	// opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.Dial(hermesRootUrl.Host, opts...)
	if err != nil {
		return nil, stacktrace.Propagate(err, "unable to connect to grpc server")
	}

	return hproto.NewHermesServiceClient(conn), nil

}

func NewMailboxServicesClient(config *RpcClientConfig) (hproto.HermesServiceClient, error) {
	var opts []grpc.DialOption

	if config.GrpcEncrypt {
		certPool, err := x509.SystemCertPool()
		if err != nil {
			return nil, stacktrace.Propagate(err, "unable to get system cert pool")
		}
		// Create the credentials and return it
		config := &tls.Config{
			RootCAs: certPool,
		}
		tlsCredentials := credentials.NewTLS(config)

		opts = append(opts, grpc.WithTransportCredentials(tlsCredentials))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	conn, err := grpc.Dial(config.HermesRootUrl.Host, opts...)
	if err != nil {
		return nil, stacktrace.Propagate(err, "unable to connect to grpc server")
	}

	return hproto.NewHermesServiceClient(conn), nil

}

func newHermesGrpcClient(config *RpcClientConfig) (HermesClient, error) {

	hermesServiceClient, err := NewMailboxServicesClient(config)
	if err != nil {
		return nil, stacktrace.Propagate(err, "unable to connect to grpc server")
	}

	clientMailbox, err := hermesServiceClient.CreateMailbox(config.Context, &hproto.CreateMailboxRequest{Channels: []string{config.RpcChannel.String()}})
	if err != nil {
		return nil, stacktrace.Propagate(err, "unable to create mailbox")
	}

	sendReceiveClient, err := hermesServiceClient.SendReceive(config.Context)
	if err != nil {
		return nil, stacktrace.Propagate(err, "unable to calling grpc SendReceive")
	}

	return &hermesGrpc{client: sendReceiveClient, clientMailbox: clientMailbox}, nil
}

func NewRpcClient(config *RpcClientConfig) (*RpcClient, error) {
	/*
		Worth noting that we have an rpc client to hermes to create the mailbox
		Then we create the hermes client that talks to the mailbox
	*/

	rpcChannel := api.RpcInbox

	hermesClient, err := NewHermesClient(config)
	if err != nil {
		return nil, stacktrace.Propagate(err, "newHermesClient error")
	}

	rpcClient := &RpcClient{
		listeners:     a8.NewSyncMap[string, chan *hproto.Message](),
		serverChannel: rpcChannel,
		hermesClient:  hermesClient,
		// hermesAdminRpc: hermesAdminRpc,
		config: config,
	}

	// submit the reader
	go func() {
		for {
			msg, err := hermesClient.ReadMessage()
			if err != nil {
				log.Error("error reading message - %v", err)
				return
			}

			correlationId := msg.CorrelationId()
			if correlationId == "" {
				log.Debug("received message without correlation id - %v", msg)
				continue
			}

			ch, ok := rpcClient.listeners.Load(correlationId)
			if !ok {
				log.Debug("no correlated listener found for message - %v", msg)
				continue
			}

			ch <- msg

			close(ch)

		}
	}()

	return rpcClient, nil
}
