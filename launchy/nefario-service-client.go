package launchy

import (
	"context"

	"accur8.io/godev/a8nats"
	"accur8.io/godev/hermes/model"
	"accur8.io/godev/log"
	"accur8.io/godev/nefario/rpc"
	"github.com/palantir/stacktrace"
	"google.golang.org/protobuf/proto"
)

type NefarioServiceClientStruct struct {
	NatsConn_ a8nats.NatsConnI
	Subject   a8nats.Subject
}

type NefarioServiceClient interface {
	ProcessStart(ctx context.Context, req *rpc.ProcessStartedRequest) error
	ProcessPing(ctx context.Context, req *rpc.ProcessPingRequest) error
	ProcessCompleted(ctx context.Context, req *rpc.ProcessCompletedRequest) error
	UpdateMailbox(ctx context.Context, req *rpc.UpdateMailboxRequest) error
	StreamWrite(ctx context.Context, req *rpc.StreamWrite) error
	Shutdown()
}

func NewNefarioServiceClient(natsConfig a8nats.ConnArgs, nefarioSubject a8nats.Subject) (NefarioServiceClient, error) {
	natsConn, err := a8nats.ResolveConn(natsConfig)
	if err != nil {
		return nil, stacktrace.Propagate(err, "unable to connect to nats")
	}

	if nefarioSubject == "" {
		nefarioSubject = model.NefarioCentral
	}

	client := &NefarioServiceClientStruct{
		NatsConn_: natsConn,
		Subject:   nefarioSubject,
	}

	return client, nil
}

func (ns *NefarioServiceClientStruct) Publish(ctx context.Context, msg *rpc.MessageFromLaunchy) error {
	bytes, err := proto.Marshal(msg)
	if err != nil {
		return stacktrace.Propagate(err, "error marshalling protobuf message to bytes")
	}
	if log.IsTraceEnabled {
		log.Trace("Publishing to %s -- %+v", ns.Subject, msg)
	}
	err = ns.NatsConn_.Publish(ns.Subject, bytes)
	if err != nil {
		return stacktrace.Propagate(err, "error publishing message to nats")
	}
	return nil
}

func (ns *NefarioServiceClientStruct) ProcessStart(ctx context.Context, req *rpc.ProcessStartedRequest) error {
	return ns.Publish(
		ctx,
		&rpc.MessageFromLaunchy{
			TestOneof: &rpc.MessageFromLaunchy_ProcessStartedRequest{
				ProcessStartedRequest: req,
			},
		},
	)
}

func (ns *NefarioServiceClientStruct) ProcessPing(ctx context.Context, req *rpc.ProcessPingRequest) error {
	return ns.Publish(
		ctx,
		&rpc.MessageFromLaunchy{
			TestOneof: &rpc.MessageFromLaunchy_ProcessPingRequest{
				ProcessPingRequest: req,
			},
		},
	)
}

func (ns *NefarioServiceClientStruct) Shutdown() {

	var err error

	err = ns.NatsConn_.Drain()
	if err != nil {
		err = stacktrace.Propagate(err, "error draining nats connection")
		log.Debug(err.Error())
	}

	ns.NatsConn_.Close()

}

func (ns *NefarioServiceClientStruct) ProcessCompleted(ctx context.Context, req *rpc.ProcessCompletedRequest) error {
	err := ns.Publish(
		ctx,
		&rpc.MessageFromLaunchy{
			TestOneof: &rpc.MessageFromLaunchy_ProcessCompletedRequest{
				ProcessCompletedRequest: req,
			},
		},
	)
	return err
}

func (ns *NefarioServiceClientStruct) StreamWrite(ctx context.Context, req *rpc.StreamWrite) error {
	return ns.Publish(
		ctx,
		&rpc.MessageFromLaunchy{
			TestOneof: &rpc.MessageFromLaunchy_StreamWrite{
				StreamWrite: req,
			},
		},
	)
}

func (ns *NefarioServiceClientStruct) UpdateMailbox(ctx context.Context, req *rpc.UpdateMailboxRequest) error {
	return ns.Publish(
		ctx,
		&rpc.MessageFromLaunchy{
			TestOneof: &rpc.MessageFromLaunchy_UpdateMailboxRequest{
				UpdateMailboxRequest: req,
			},
		},
	)
}
