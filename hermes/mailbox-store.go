package hermes

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"accur8.io/godev/a8"
	"accur8.io/godev/a8nats"
	"accur8.io/godev/hermes/api"
	"accur8.io/godev/hermes/hproto"
	"accur8.io/godev/hermes/model"
	"accur8.io/godev/log"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/palantir/stacktrace"
	"google.golang.org/protobuf/proto"
)

type MailboxStore interface {
	NatsService() a8nats.NatsConnI
	FetchOrCreateNamedMailbox(address api.MailboxAddress) (api.MailboxI, error)
	CreateMailbox(req *hproto.CreateMailboxRequest) (api.MailboxI, error)
	RemoveMailbox(adminKey api.AdminKey)
	UpdateMailbox(mbox *Mailbox)
	FetchMailboxByAdminKey(adminKey api.AdminKey) *Mailbox
	FetchMailboxByAddress(address api.MailboxAddress) *Mailbox
	FetchMailboxByReaderKey(readerKey api.ReaderKey) *Mailbox
	ForEachMailbox(func(adminKey api.AdminKey) error) error
	ForEachMailboxInMemory(func(mbox *Mailbox) error) error
	Purge(mbox *Mailbox) error
	PublishSendMessageRequest(smr *hproto.SendMessageRequest, from api.MailboxI) (*hproto.SendMessageResponse, error)
}

type MailboxStoreStruct struct {
	Nats                 a8nats.NatsConnI
	AdminMailboxesKV     nats.KeyValue
	MailboxesByRWKeysKV  nats.KeyValue
	Mutex                *sync.Mutex
	InMemoryMailboxes    a8.SyncMap[string, *Mailbox]
	UseDirectSendMessage bool
}

func NewMailboxStoreStruct(natsConn a8nats.NatsConnI, useDirectSendMessage bool) *MailboxStoreStruct {

	jetStream := natsConn.JetStream()

	createKeyValue := func(keyValueBucket string) nats.KeyValue {
		var err error
		keyValue, _ := jetStream.KeyValue(keyValueBucket)
		if keyValue == nil {
			keyValue, err = jetStream.CreateKeyValue(&nats.KeyValueConfig{
				Bucket: keyValueBucket,
			})
			if err != nil {
				err = stacktrace.Propagate(err, "create key value failed")
				log.Error("create key value failed %s", err)
				panic(err)
			}
		}
		return keyValue
	}

	return &MailboxStoreStruct{
		AdminMailboxesKV:     createKeyValue("HermesMailboxesByAdminKey"),
		MailboxesByRWKeysKV:  createKeyValue("HermesMailboxesByRWKeys"),
		Mutex:                &sync.Mutex{},
		InMemoryMailboxes:    a8.NewSyncMap[string, *Mailbox](),
		Nats:                 natsConn,
		UseDirectSendMessage: useDirectSendMessage,
	}

}

func (mbs *MailboxStoreStruct) FetchOrCreateNamedMailbox(address api.MailboxAddress) (api.MailboxI, error) {
	threeMonths := int64(3 * 30 * 24 * 60 * 60 * 1000) // 3 months
	mbox := mbs.FetchMailboxByAddress(address)
	if mbox != nil {
		return mbox, nil
	}
	return mbs._CreateMailboxImpl(
		&hproto.CreateMailboxRequest{
			CloseTimeoutInMillis: threeMonths,
			PurgeTimeoutInMillis: threeMonths,
			Channels:             api.RpcChannelsAsStrings,
		},
		address,
	)
}

func (mbs *MailboxStoreStruct) CreateMailbox(req *hproto.CreateMailboxRequest) (api.MailboxI, error) {
	return mbs._CreateMailboxImpl(req, model.NewMailboxAddress(""))
}

func (mbs *MailboxStoreStruct) _CreateMailboxImpl(req *hproto.CreateMailboxRequest, address api.MailboxAddress) (*Mailbox, error) {
	mbs.Mutex.Lock()
	defer mbs.Mutex.Unlock()
	randomKey := func(prefix string) string {
		v := uuid.NewString()
		return prefix + prefix + strings.Replace(v, "-", "", -1)
	}

	pubm, err := req.PublicMetadata.MarshalJSON()
	if err != nil {
		return nil, stacktrace.Propagate(err, "unable to marshal public metadata")
	}

	privm, err := req.PrivateMetadata.MarshalJSON()
	if err != nil {
		return nil, stacktrace.Propagate(err, "unable to marshal private metadata")
	}

	isNamed := !address.IsEmpty()
	if !isNamed {
		address = model.NewMailboxAddress(randomKey("a"))
	}

	keys := &api.MailboxKeys{
		Address:   address,
		ReaderKey: model.NewReaderKey(randomKey("r")),
		AdminKey:  model.NewAdminKey(randomKey("z")),
	}

	// technically we should not have duplicate keys because keys are uuid's
	// but this is a just in case

	v0, _ := mbs.MailboxesByRWKeysKV.Get(keys.Address.String())
	if v0 != nil {
		return nil, stacktrace.NewError("duplicate address - %s", keys.Address)
	}
	v0, _ = mbs.MailboxesByRWKeysKV.Get(keys.ReaderKey.String())
	if v0 != nil {
		return nil, stacktrace.NewError("duplicate readerkey")
	}
	v0, _ = mbs.AdminMailboxesKV.Get(keys.AdminKey.String())
	if v0 != nil {
		return nil, stacktrace.NewError("duplicate adminkey")
	}

	purgeTimeoutInMillis := req.PurgeTimeoutInMillis
	if purgeTimeoutInMillis == 0 {
		purgeTimeoutInMillis = (24 * time.Hour).Milliseconds()
	}

	closeTimeoutInMillis := req.CloseTimeoutInMillis
	if closeTimeoutInMillis == 0 {
		closeTimeoutInMillis = (8 * time.Hour).Milliseconds()
	}

	mbox := Mailbox{
		MailboxKeys:          keys,
		PublicMetadata:       pubm,
		PrivateMetadata:      privm,
		PurgeTimeoutInMillis: purgeTimeoutInMillis,
		CloseTimeoutInMillis: closeTimeoutInMillis,
		LastActivity:         a8.NowUtcTimestamp().InEpochhMillis(),
		Channels:             make([]model.ChannelName, 0, len(req.Channels)),
		mailboxStore:         mbs,
		correlations:         a8.NewSyncMap[model.CorrelationId, chan *hproto.Message](),
		IsNamed_:             isNamed,
	}

	for _, channel := range req.ChannelNames() {
		err := mbox.AddChannel(channel, false)
		if err != nil {
			return nil, stacktrace.Propagate(err, "unable to add channel %s to %s", channel, mbox.AdminKey())
		}
	}

	mboxJson, err := json.Marshal(mbox)
	if err != nil {
		return nil, stacktrace.Propagate(err, "unable to create mboxJson")
	}

	mbox.UpdateLastActivity()

	put := func(key fmt.Stringer, value []byte, kv nats.KeyValue) {
		_, err := kv.Put(key.String(), value)
		if err != nil {
			err = stacktrace.Propagate(err, "swallowing unexpected error putting %v in MailboxesKeyValue", key)
			log.Error(err.Error())
		}
	}

	put(mbox.ReaderKey(), []byte(mbox.AdminKey().String()), mbs.MailboxesByRWKeysKV)
	put(mbox.Address(), []byte(mbox.AdminKey().String()), mbs.MailboxesByRWKeysKV)
	put(mbox.AdminKey(), mboxJson, mbs.AdminMailboxesKV)

	return &mbox, nil

}

func (mbs *MailboxStoreStruct) PublishSendMessageRequest(smr *hproto.SendMessageRequest, from api.MailboxI) (*hproto.SendMessageResponse, error) {
	if mbs.UseDirectSendMessage {
		return mbs.DirectPublishSendMessageRequestX(smr, from)
	} else {
		err := mbs.LeafPublishSendMessageRequest(smr)
		return nil, err
	}
}

func (mbs *MailboxStoreStruct) UpdateMailbox(mbox *Mailbox) {
	mbs.Mutex.Lock()
	defer mbs.Mutex.Unlock()
	mboxJson, err := json.Marshal(mbox)
	if err != nil {
		log.Warn("unable to create mboxJson %s", err)
		return
	}
	_, err = mbs.AdminMailboxesKV.Put(mbox.AdminKey().String(), mboxJson)
	if err != nil {
		err = stacktrace.Propagate(err, "swallowing unexpected error updating %v in MailboxesKeyValue", mbox.AdminKey())
		log.Error(err.Error())
	}
}

func (mbs *MailboxStoreStruct) FetchMailboxByAdminKey(adminKey model.AdminKey) *Mailbox {
	mailboxJsonEntry, err := mbs.AdminMailboxesKV.Get(adminKey.String())
	if err != nil {
		log.Warn("error mailboxJsonEntry Lookup adminKey %s not found-- %s", adminKey, err)
		return nil
	}

	mailboxJson := mailboxJsonEntry.Value()
	mbox := &Mailbox{}
	err = json.Unmarshal(mailboxJson, mbox)
	if err != nil {
		log.Warn("error marshalling mailbox json %s -- %s", mailboxJson, err)
		return nil
	}
	mbox.correlations = a8.NewSyncMap[model.CorrelationId, chan *hproto.Message]()
	mbox.mailboxStore = mbs
	if mbox.MailboxKeys.AdminKey != adminKey {
		// fetched mailbox does not match adminKey
		return nil
	}
	// log.Debug("fetched mailbox %s", string(mailboxJson))
	return mbox
}

func (mbs *MailboxStoreStruct) RemoveMailbox(adminKey model.AdminKey) {
	mbs.Mutex.Lock()
	defer mbs.Mutex.Unlock()
}

func (mbs *MailboxStoreStruct) FetchMailboxByReaderKey(readerKey api.ReaderKey) *Mailbox {
	return mbs.MailboxLookup(
		"ReaderKey",
		readerKey.String(),
		func(mbox *Mailbox) string { return mbox.ReaderKey().String() },
	)
}

func (mbs *MailboxStoreStruct) FetchMailboxByAddress(address api.MailboxAddress) *Mailbox {
	return mbs.MailboxLookup(
		"Address",
		address.String(),
		func(mbox *Mailbox) string { return mbox.Address().String() },
	)
}

func (mbs *MailboxStoreStruct) MailboxLookup(label string, key string, getKeyFn func(*Mailbox) string) *Mailbox {
	// if log.IsTraceEnabled {
	// 	log.Trace("MailboxLookup %s - %s", label, key)
	// }
	mbox, _ := mbs.InMemoryMailboxes.Load(key)
	if mbox != nil {
		if getKeyFn(mbox) == key {
			return mbox
		} else {
			return nil
		}
	} else {
		mbs.Mutex.Lock()
		defer mbs.Mutex.Unlock()
		mbox, _ := mbs.InMemoryMailboxes.Load(key)
		if mbox != nil {
			if getKeyFn(mbox) == key {
				return mbox
			} else {
				return nil
			}
		} else {
			adminKeyEntry, err := mbs.MailboxesByRWKeysKV.Get(key)
			if err != nil {
				log.Warn("error MailboxLookup %s -- %s", key, err)
				return nil
			}
			adminKey := model.NewAdminKey(string(adminKeyEntry.Value()))
			mbox = mbs.FetchMailboxByAdminKey(adminKey)
			if mbox == nil {
				log.Warn("error MailboxLookup on adminKey %s -- not found - this should not happen as the %s %s was found", adminKey, label, key)
				return nil
			}

			mbs.InMemoryMailboxes.Store(mbox.AdminKey().String(), mbox)
			mbs.InMemoryMailboxes.Store(mbox.ReaderKey().String(), mbox)
			mbs.InMemoryMailboxes.Store(mbox.Address().String(), mbox)

			return mbox
		}

	}
}

func (mbss *MailboxStoreStruct) DirectPublishSendMessageRequestX(request *hproto.SendMessageRequest, from api.MailboxI) (*hproto.SendMessageResponse, error) {
	if log.IsTraceEnabled {
		log.Trace("Publishing SendMessageRequest %+v - From Mailbox %+v", request, from.Keys())
	}
	request.Message.Header.Sender = from.Address().String()
	errorsArr := []*hproto.SendMessageError{}
	dupesArr := []string{}
	for _, to := range request.To {
		toMbox := mbss.FetchMailboxByAddress(model.NewMailboxAddress(to))
		if toMbox != nil {
			ack, err := mbss.PublishMessage(toMbox, request.ChannelO(), request.Message, request.IdempotentIdO())
			if err != nil {
				errorsArr = append(errorsArr, &hproto.SendMessageError{To: to, Message: err.Error()})
			} else if ack.Duplicate {
				dupesArr = append(dupesArr, to)
			}
		} else {
			errorsArr = append(errorsArr, &hproto.SendMessageError{To: to, Message: fmt.Sprintf("mailbox address %s not found", to)})
		}
	}

	errorsCt := len(errorsArr)
	if errorsCt > 0 && errorsCt == len(request.To) && request.Message.GetHeader().GetRpcHeader() != nil && request.Message.GetHeader().GetRpcHeader().FrameType == hproto.RpcFrameType_Request {
		errorMessage := ""
		for _, err := range errorsArr {
			if len(errorMessage) > 0 {
				errorMessage += ", "
			}
			errorMessage += err.Message
		}
		errorResponseMessage := &hproto.Message{
			Header: &hproto.MessageHeader{
				Sender: from.Address().String(),
				RpcHeader: &hproto.RpcHeader{
					CorrelationId: request.Message.GetHeader().GetRpcHeader().CorrelationId,
					FrameType:     hproto.RpcFrameType_ErrorResponse,
					EndPoint:      request.GetMessage().GetHeader().GetRpcHeader().GetEndPoint(),
					ErrorInfo: &hproto.RpcErrorInfo{
						Message: errorMessage,
					},
				},
			},
			SenderEnvelope: &hproto.SenderEnvelope{
				Created: a8.NowUtcTimestamp().InEpochhMillis(),
			},
		}
		// synthesize an rpc error response
		_, err := mbss.PublishMessage(from, request.ChannelO(), errorResponseMessage, model.RandomIdempotentId())
		if err != nil {
			err = stacktrace.Propagate(err, "swallowing error synthesizing rpc error response message")
			log.Error(err.Error())
		}
	}
	response := &hproto.SendMessageResponse{
		IdempotentId:  request.IdempotentId,
		Errors:        errorsArr,
		Duplicates:    dupesArr,
		CorrelationId: request.Message.GetHeader().GetRpcHeader().GetCorrelationId(),
	}

	if from.HasSent() {
		go func() {
			sendReceipt := &hproto.SendReceipt{
				Request:  request,
				Response: response,
			}
			err := mbss.PublishSendReceipt(from, sendReceipt)
			if err != nil {
				err = stacktrace.Propagate(err, "error publishing sent message")
				log.Debug(err.Error())
			}
		}()
	} else {
		if log.IsTraceEnabled {
			log.Trace("no sent folder")
		}
	}

	return response, nil
}

func (mbss *MailboxStoreStruct) PublishMessage(toMbox api.MailboxI, channel api.ChannelName, msg *hproto.Message, idmpotentId model.IdempotentId) (*nats.PubAck, error) {
	subject := toMbox.NatsSubject(channel)

	msgBytes, err := proto.Marshal(msg)
	if err != nil {
		return nil, stacktrace.Propagate(err, "error marshalling message to bytes")
	}

	if log.IsTraceEnabled {
		log.Trace("publishing message %+v to subject %s to mailbox %+v", msg, subject, toMbox.Keys())
	}
	ack, err := mbss.Nats.JetStreamPublish(subject, msgBytes, idmpotentId)
	if err != nil {
		return nil, stacktrace.Propagate(err, "error publishing message")
	}
	return ack, nil
}

func (mbss *MailboxStoreStruct) PublishSendReceipt(mbox api.MailboxI, sendReceipt *hproto.SendReceipt) error {
	channel := api.RpcSent
	subject := mbox.NatsSubject(channel)

	msgBytes, err := proto.Marshal(sendReceipt)
	if err != nil {
		return stacktrace.Propagate(err, "error marshalling message to bytes")
	}

	if log.IsTraceEnabled {
		log.Trace("publishing send receipt %+v to subject %s to mailbox %+v", sendReceipt, subject, mbox.Keys())
	}
	_, err = mbss.Nats.JetStreamPublish(subject, msgBytes, model.NewIdempotentId(""))
	if err != nil {
		return stacktrace.Propagate(err, "error publishing message")
	}
	return nil
}

func (mbss *MailboxStoreStruct) LeafPublishSendMessageRequest(smr *hproto.SendMessageRequest) error {
	bytes, err := proto.Marshal(smr)
	if err != nil {
		return stacktrace.Propagate(err, "unable to marshal SendMessageRequest into bytes")
	}
	if log.IsTraceEnabled {
		log.Trace("publishing SendMessageRequest %+v", smr)
	}
	return mbss.Nats.Publish(HermesSendMessageRequestWorkQueue.Subject(), bytes)
}

func (mbss *MailboxStoreStruct) NatsService() a8nats.NatsConnI {
	return mbss.Nats
}

func (mbss *MailboxStoreStruct) ForEachMailboxInMemory(fn func(mbox *Mailbox) error) error {
	var err error
	mbss.InMemoryMailboxes.Range(
		func(key string, value *Mailbox) bool {
			// only admin keys so we don't process each mailbox 3 times once for each key type
			if strings.HasPrefix(key, "zzz") {
				err0 := fn(value)
				if err0 != nil {
					err = err0
					return false
				}
			}
			return true
		},
	)
	return err
}
