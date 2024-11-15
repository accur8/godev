package directclient

import (
	"context"
	"fmt"
	"strings"
	"time"

	"accur8.io/godev/a8"
	"accur8.io/godev/a8nats"
	"accur8.io/godev/hermes"
	"accur8.io/godev/hermes/api"
	"accur8.io/godev/hermes/hproto"
	"accur8.io/godev/log"
	"github.com/palantir/stacktrace"
)

type ChannelName = string

type MailboxManagerConfig struct {
	Nats                 a8nats.ConnArgs
	UseDirectSendMessage bool
}

type RpcMailboxConfig struct {
	Url                  string // nats: grpc: ws:
	UseDirectSendMessage bool
	Description          string
	MailboxKey           api.MailboxAddress
	Name                 string
	UpdateTick           time.Duration
}

func CreateRpcMailbox(ctx context.Context, config *RpcMailboxConfig) (api.MailboxI, error) {

	if config.Name == "" {
		config.Name = "nats-central-rpc"
	}

	url := config.Url
	if strings.HasPrefix(url, "nats:") {
		mailboxManager, err := NewMailboxManager(
			ctx,
			&MailboxManagerConfig{
				Nats:                 a8nats.ConnArgs{NatsUrl: config.Url, Name: config.Name},
				UseDirectSendMessage: config.UseDirectSendMessage,
			},
		)
		if err != nil {
			return nil, stacktrace.Propagate(err, "unable to create mailbox manager")
		}
		var mbox api.MailboxI
		if config.MailboxKey.IsEmpty() {
			mbox, err = mailboxManager.CreateMailbox(&hproto.CreateMailboxRequest{
				Channels: api.RpcChannelsAsStrings,
			})
		} else {
			mbox, err = mailboxManager.FetchOrCreateNamedMailbox(config.MailboxKey)
		}
		if err != nil {
			return nil, stacktrace.Propagate(err, "unable to create mailbox")
		}

		a8.SubmitGoRoutine("update-mailbox-last-activity", func() error {
			updateTick := config.UpdateTick
			if updateTick == 0 {
				updateTick = 5 * time.Minute
			}
			ticker := time.NewTicker(updateTick)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return nil
				case <-ticker.C:
					log.Debug("update tick for mailbox %s", mbox.Address())
					mbox.UpdateLastActivity()
					mbox.UpdateInStore()
				}
			}
		})

		return mbox, nil
		// } else if strings.HasPrefix(url, "grpc") {
		// } else if strings.HasPrefix(url, "ws") {
	} else {
		panic(fmt.Sprintf("don't know how to handle url %s", url))
	}
}

// func (ns *natsStream) Write(msg *hproto.Message) error {

// 	if log.IsTraceEnabled {
// 		log.Trace("publishing to subject %s", ns.subject)
// 	}

// 	msgBytes, err := proto.Marshal(msg)
// 	if err != nil {
// 		return stacktrace.Propagate(err, "error marshalling message to bytes %s", err)
// 	}

// 	_, err = ns.natsMailbox.natsService.jetStream.Publish(ns.subject, msgBytes)
// 	if err != nil {
// 		return stacktrace.Propagate(err, "error sending message %s", err)
// 	}

// 	return nil

// }

// func (nm *natsMailbox) AddChannel(contex context.Context, channelName ChannelName) error {

// 	if nm.mbox.IsValidChannel(channelName) {
// 		return stacktrace.NewError("mailbox %s channel %s already exists", nm.mbox.AdminKey(), channelName)
// 	}

// 	if channelName == "" {
// 		return stacktrace.NewError("channel must be supplied")
// 	}

// 	subject := nm.mbox.NatsSubject(channelName)
// 	streamName := nm.mbox.NatsStreamName(channelName)
// 	stream, _ := nm.natsService.jetStream.StreamInfo(streamName)
// 	if stream == nil {
// 		_, err := nm.natsService.jetStream.AddStream(&nats.StreamConfig{
// 			Name:     streamName,
// 			Subjects: []string{subject},
// 			Storage:  nats.FileStorage,
// 		})
// 		if err != nil {
// 			errorMsg := fmt.Sprintf("error adding stream %s -- %s", streamName, err)
// 			log.Debug(errorMsg)
// 			return errors.New(errorMsg)
// 		}
// 	}

// 	// mbox.Channels = append(mbox.Channels, channelName)
// 	err := nm.Mailbox().UpdateInStore()
// 	if err != nil {
// 		return stacktrace.Propagate(err, "unable to update mailbox in store")
// 	}

// 	log.Debug("added nats stream subject %s stream %s", subject, streamName)
// 	log.Debug("created channel %s in %s", channelName, nm.mbox.AdminKey())

// 	return nil

// }

func NewMailboxManager(ctx context.Context, config *MailboxManagerConfig) (api.MailboxManagerI, error) {

	natsConn, err := a8nats.ResolveConn(config.Nats)
	if err != nil {
		return nil, stacktrace.Propagate(err, "nats connect failure")
	}

	mailboxStore := hermes.NewMailboxStoreStruct(
		natsConn,
		config.UseDirectSendMessage,
	)

	return mailboxStore, nil

}
