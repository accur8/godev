package hermes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"accur8.io/godev/a8nats"
	"accur8.io/godev/hermes/api"
	"accur8.io/godev/hermes/hproto"
	"accur8.io/godev/hermes/model"
	"accur8.io/godev/iter"
	"accur8.io/godev/log"
	"google.golang.org/protobuf/proto"

	"accur8.io/godev/a8"
	"github.com/nats-io/nats.go"
	"github.com/palantir/stacktrace"
)

type Mailbox struct {
	MailboxKeys          *api.MailboxKeys
	LastActivity         int64
	Channels             []api.ChannelName
	PublicMetadata       json.RawMessage
	PrivateMetadata      json.RawMessage
	PurgeTimeoutInMillis int64
	CloseTimeoutInMillis int64
	mailboxStore         *MailboxStoreStruct
	hasSent              *bool
	correlations         a8.SyncMap[model.CorrelationId, chan *hproto.Message]
	IsNamed_             bool `json:"isNamed"`
	needsUpdate          bool
}

type _StreamS struct {
	Mailbox        *Mailbox
	Channel        api.ChannelName
	NatsSubject    a8nats.Subject
	NatsStreamName a8nats.StreamName
	StartSeq       string
	ConsumerName_  a8nats.ConsumerName
}

func (mbox *Mailbox) HasSent() bool {
	if mbox.hasSent == nil {
		for _, channel := range mbox.Channels {
			if channel == api.RpcSent {
				mbox.hasSent = a8.NewTrue()
			}
		}
		if mbox.hasSent == nil {
			mbox.hasSent = a8.NewFalse()
		}
	}
	return *mbox.hasSent
}

func (mbox *Mailbox) NeedsUpdate() bool {
	return mbox.needsUpdate
}

func (mbox *Mailbox) IsPurgable() bool {
	now := a8.NowUtcTimestamp()
	expiredDelta := (12 * time.Hour) / time.Millisecond
	if mbox.PurgeTimeoutInMillis > 0 {
		expiredDelta = time.Duration(mbox.PurgeTimeoutInMillis)
	}
	actualDelta := now.InEpochhMillis() - mbox.LastActivity
	return actualDelta < int64(expiredDelta)
}

func (mbox *Mailbox) IsActive() bool {
	now := a8.NowUtcTimestamp()
	expiredDelta := (1 * time.Hour) / time.Millisecond
	if mbox.CloseTimeoutInMillis > 0 {
		expiredDelta = time.Duration(mbox.CloseTimeoutInMillis)
	}
	actualDelta := now.InEpochhMillis() - mbox.LastActivity
	return actualDelta < int64(expiredDelta)
}

func (mbox *Mailbox) AdminKey() api.AdminKey {
	return mbox.MailboxKeys.AdminKey
}

func (mbox *Mailbox) ReaderKey() api.ReaderKey {
	return mbox.MailboxKeys.ReaderKey
}

func (mbox *Mailbox) IsValidChannel(channel api.ChannelName) bool {
	for _, c := range mbox.Channels {
		if c == channel {
			return true
		}
	}
	return false
}

func (mbox *Mailbox) UpdateLastActivity() {
	mbox.LastActivity = a8.NowUtcTimestamp().InEpochhMillis()
	mbox.needsUpdate = true
}

func (mbox *Mailbox) ChannelNames() []api.ChannelName {
	return mbox.Channels
}

func (mbox *Mailbox) RegisterCorrelation(correlationId model.CorrelationId, callback chan *hproto.Message) {
	mbox.correlations.Store(correlationId, callback)
}

func (mbox *Mailbox) RemoveCorrelation(correlationId model.CorrelationId) {
	mbox.correlations.Delete(correlationId)
}

func (mbox *Mailbox) AddChannel(channelName model.ChannelName, updateMailboxInStore bool) error {

	if mbox.IsValidChannel(channelName) {
		return stacktrace.NewError("mailbox %s channel %s already exists", mbox.AdminKey(), channelName)
	}

	if channelName.IsEmpty() {
		return stacktrace.NewError("channel must be supplied")
	}

	subject := mbox.NatsSubject(channelName)
	streamName := mbox.NatsStreamName(channelName)
	stream, _ := mbox.mailboxStore.Nats.StreamInfo(streamName)
	if stream == nil {
		_, err := mbox.mailboxStore.Nats.AddStream(&nats.StreamConfig{
			Name:     streamName.String(),
			Subjects: []string{subject.String()},
			Storage:  nats.FileStorage,
		})
		if err != nil {
			errorMsg := fmt.Sprintf("error adding stream %s -- %s", streamName, err)
			log.Debug(errorMsg)
			return errors.New(errorMsg)
		}
	}
	if channelName == api.RpcSent {
		mbox.hasSent = a8.NewTrue()
	}

	mbox.Channels = append(mbox.Channels, channelName)

	if updateMailboxInStore {
		mbox.mailboxStore.UpdateMailbox(mbox)
	}

	log.Debug("added nats stream subject %s stream %s", subject, streamName)
	log.Debug("created channel %s in %s", channelName, mbox.AdminKey())

	return nil

}

func (mbs *MailboxStoreStruct) ForEachMailbox(fn func(adminKey model.AdminKey) error) error {
	adminKeys, err := mbs.AdminMailboxesKV.Keys()
	if err != nil {
		return stacktrace.Propagate(err, "ForEachMailbox error getting adminKeys")
	}
	if log.IsTraceEnabled {
		log.Trace("ForEachMailbox %v mailboxes", len(adminKeys))
	}

	errorsArr := []error{}

	for _, adminKeyStr := range adminKeys {
		adminKey := model.NewAdminKey(adminKeyStr)
		err = fn(adminKey)
		if err != nil {
			errorsArr = append(errorsArr, err)
		}
	}

	return errors.Join(errorsArr...)

}

func (mbs *MailboxStoreStruct) Purge(mbox *Mailbox) error {
	log.Debug("Purging mailbox %s now=%v", a8.ToJson(mbox), a8.NowUtcTimestamp())

	func() {
		mbs.Mutex.Lock()
		defer mbs.Mutex.Unlock()
		mbs.MailboxesByRWKeysKV.Delete(mbox.ReaderKey().String())
		mbs.MailboxesByRWKeysKV.Delete(mbox.Address().String())
		mbs.AdminMailboxesKV.Delete(mbox.AdminKey().String())
		mbs.InMemoryMailboxes.Delete(mbox.AdminKey().String())
		mbs.InMemoryMailboxes.Delete(mbox.Address().String())
		mbs.InMemoryMailboxes.Delete(mbox.ReaderKey().String())
	}()

	for _, channel := range mbox.Channels {
		streamName := mbox.NatsStreamName(channel)
		err := mbs.Nats.DeleteStream(streamName)
		if err != nil {
			log.Warn("error deleting stream %s -- %s", streamName, err)
		}
	}
	return nil
}

func (mbox *Mailbox) Keys() *api.MailboxKeys {
	return mbox.MailboxKeys
}

func (mbox *Mailbox) Address() api.MailboxAddress {
	return mbox.Keys().Address
}

func (mbox *Mailbox) ChannelNamesAsStrings() []string {
	return iter.Map(
		iter.FromSlice(mbox.Channels),
		func(cn model.ChannelName) string {
			return cn.String()
		},
	).ToArray()
}

func (mbox *Mailbox) NatsSubject(channelName api.ChannelName) a8nats.Subject {
	return a8nats.NewSubject(fmt.Sprintf("hermes.%s.%s", mbox.AdminKey(), channelName))
}

func (mbox *Mailbox) NatsStreamName(channelName api.ChannelName) a8nats.StreamName {
	return a8nats.NewStreamName(fmt.Sprintf("hermes-%s-%s", mbox.AdminKey(), channelName))
}

func (mbox *Mailbox) PublishSendMessageRequest(request *hproto.SendMessageRequest) (*hproto.SendMessageResponse, error) {
	// ??? TODO have PublishSendMessageRequest return a proper error
	return mbox.mailboxStore.PublishSendMessageRequest(request, mbox)
}

func (mbox *Mailbox) IsNamedMailbox() bool {
	return mbox.IsNamed_
}

func (mbox *Mailbox) UpdateInStore() error {
	// ??? TODO have UpdateMailbox return a proper error
	mbox.mailboxStore.UpdateMailbox(mbox)
	return nil
}

func (mbox *Mailbox) Stream(channel api.ChannelName) (api.StreamI, error) {
	if mbox.IsValidChannel(channel) {
		stream := &_StreamS{
			Mailbox:        mbox,
			Channel:        channel,
			NatsSubject:    mbox.NatsSubject(channel),
			NatsStreamName: mbox.NatsStreamName(channel),
			StartSeq:       "",
			ConsumerName_:  "",
		}
		return stream, nil
	} else {
		return nil, stacktrace.NewError("channel %s does not exist", channel)
	}
}

func (stream *_StreamS) StartPos(startPos string) api.StreamI {
	stream.StartSeq = startPos
	return stream
}

func (stream *_StreamS) ConsumerName(consumerName a8nats.ConsumerName) api.StreamI {
	stream.ConsumerName_ = consumerName
	return stream
}

func (stream *_StreamS) ChannelName() api.ChannelName {
	return stream.Channel
}

func (stream *_StreamS) Subscribe(ctx context.Context, receiverFn func(*hproto.Message) error) (*nats.Subscription, error) {

	msgHandler := func(natsMsg *nats.Msg) {
		// if log.IsTraceEnabled {
		// 	log.Trace("received nats message %+v", natsMsg)
		// }
		err := natsMsg.Ack()
		if err != nil {
			log.Error(stacktrace.Propagate(err, "error acking message").Error())
		}

		var msg hproto.Message
		err = proto.Unmarshal(natsMsg.Data, &msg)
		if err != nil {
			err = stacktrace.Propagate(err, "error on channel %s unable to unmarshal message", stream.Channel)
			log.Error("%v", err)
			return
		}
		err = receiverFn(&msg)
		if err != nil {
			err = stacktrace.Propagate(err, "channel %s error while calling receiverFn", stream.Channel)
			log.Error("%v", err)
		}
	}

	_, startSeq := a8nats.StartSeqToSubscriptionOptions(stream.StartSeq)

	subOpts := []nats.SubOpt{
		nats.BindStream(stream.NatsStreamName.String()),
		nats.Context(ctx),
		startSeq,
	}

	if stream.ConsumerName_ != "" {
		subOpts = append(subOpts, nats.Durable(stream.ConsumerName_.String()))
	}
	// nats.ConsumerName(stream.ConsumerName_)

	subscription, err := stream.Mailbox.mailboxStore.Nats.JetStream().Subscribe(
		stream.NatsSubject.String(),
		msgHandler,
		subOpts...,
	)
	return subscription, err
}

func (stream *_StreamS) RunReadLoop(ctx context.Context, messageHandler func(*hproto.Message) error) error {

	sub, err := stream.Subscribe(ctx, messageHandler)
	if err != nil {
		return stacktrace.Propagate(err, "error subscribing")
	}

	defer sub.Unsubscribe()

	<-ctx.Done()

	return nil

}

func (mbox *Mailbox) RunRpcInboxReader(ctx context.Context, rpcRequestHandler func(msg *hproto.Message) error) error {

	channel := api.RpcInbox
	stream, err := mbox.Stream(channel)
	if err != nil {
		return stacktrace.Propagate(err, "error creating stream")
	}

	if mbox.IsNamed_ {
		stream = stream.ConsumerName(a8nats.ConsumerName(mbox.Address().String()))
	}

	messageHandler := func(msg *hproto.Message) error {

		if log.IsTraceEnabled {
			log.Trace("%s read loop proto msg %+v", channel, msg)
		}

		frameType := msg.GetHeader().GetRpcHeader().FrameType

		if frameType == hproto.RpcFrameType_ErrorResponse || frameType == hproto.RpcFrameType_SuccessResponse {

			correlationId := msg.GetHeader().GetRpcHeader().CorrelationIdO()
			if correlationId.IsEmpty() {
				return nil
			}

			ch, ok := mbox.correlations.Load(correlationId)
			if ok {
				ch <- msg
			} else {
				log.Debug("no correlation found for %s", correlationId)
			}

			return nil

		} else if frameType == hproto.RpcFrameType_Request {
			if rpcRequestHandler != nil {
				return rpcRequestHandler(msg)
			} else {
				return stacktrace.NewError("received a request frame and rpcRequestHandler is nil")
			}
		} else {
			return nil
			// return stacktrace.NewError("unexpected frame type: %v", frameType)
		}

	}

	return stream.RunReadLoop(ctx, messageHandler)

}

func (mbox *Mailbox) RawRpcCall(requestBody []byte, request *api.RpcRequest) (*hproto.Message, error) {
	return api.RawRpcCall(requestBody, request)
}
