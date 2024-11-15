package hermes

import (
	context "context"

	"accur8.io/godev/a8"
	"accur8.io/godev/hermes/hproto"
	"accur8.io/godev/hermes/model"
	"accur8.io/godev/log"
	"github.com/google/uuid"
	"github.com/palantir/stacktrace"
)

type HermesServiceServerStruct struct {
	services *Services
	hproto.UnsafeHermesServiceServer
}

func NewHermesServiceServer(services *Services) hproto.HermesServiceServer {
	return &HermesServiceServerStruct{
		services: services,
	}
}

func (hs *HermesServiceServerStruct) CreateMailbox(ctx context.Context, req *hproto.CreateMailboxRequest) (*hproto.CreateMailboxResponse, error) {

	log.Debug("creating mailbox -- %v", a8.ToJson(req))

	mbox, err := hs.services.MailboxStore.CreateMailbox(req)
	if err != nil {
		return nil, stacktrace.Propagate(err, "unable to create mailbox")

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

func (hs *HermesServiceServerStruct) AddChannel(ctx context.Context, req *hproto.AddChannelRequest) (*hproto.AddChannelResponse, error) {
	adminKey := model.NewAdminKey(req.AdminKey)
	mbox := hs.services.MailboxStore.FetchMailboxByAdminKey(adminKey)
	if mbox == nil {
		return nil, stacktrace.NewError("unable to find mailbox %s", adminKey)
	}
	for _, channel := range req.ChannelNames() {
		err := mbox.AddChannel(channel, true)
		if err != nil {
			stacktrace.Propagate(err, "error adding channel %s to %s", channel, adminKey)
		}
	}
	return &hproto.AddChannelResponse{}, nil
}

func (hsss *HermesServiceServerStruct) SendReceive(stream hproto.HermesService_SendReceiveServer) error {
	uuid, err := uuid.NewRandom()
	if err != nil {
		return err
	}

	log.Debug("grpc SendReceive started %v", uuid)
	defer log.Debug("grpc SendReceive completed %v", uuid)

	// msg, err := stream.Recv()
	// if err != nil {
	// 	err = stacktrace.Propagate(err, "error reading SendRecv stream")
	// 	log.Debug("error reading stream", err)
	// 	return err
	// }

	activeGrpc := &ActiveGrpcSendReceive{
		GrpcStream: stream,
		ClientInfo: &ClientInfo{
			Uuid:              &uuid,
			SubscriptionsById: a8.NewSyncMap[string, []*ClientSubscription](),
		},
	}

	acw := &ActiveClientW{
		Client:          activeGrpc,
		GlobalServices0: hsss.services,
	}

	return acw.StartProtocol()

}
