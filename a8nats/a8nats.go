package a8nats

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"
	"time"

	"accur8.io/godev/a8"
	"accur8.io/godev/hermes/model"
	"accur8.io/godev/log"
	"github.com/nats-io/nats.go"
	"github.com/palantir/stacktrace"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type WorkQueueConfig struct {
	Nats               ConnArgs
	Subject            Subject
	StreamName         StreamName
	ConsumerName       ConsumerName
	RetentionPolicy    nats.RetentionPolicy
	AssumeProto        bool
	MaxDeliver         int
	FetchTimeoutMillis int
	MaxAge             time.Duration
	MaxMsgs            int64
	MaxBytes           int64
	StartSeq           string
	CreateStream       bool
	BatchSize          int
}

func RunProtoWorkQueue[A proto.Message](ctx context.Context, config *WorkQueueConfig, taskFn func(A) error) error {

	protoTaskFn := func(msg *nats.Msg) error {
		a, err := Unmarshal[A](msg, config.AssumeProto)
		if err != nil {
			return stacktrace.Propagate(err, "error on Unmarshal - %s", config.ConsumerName)
		}
		if log.IsTraceEnabled {
			log.Trace("received proto message %+v", *a)
		}
		err = taskFn(*a)
		if err != nil {
			return stacktrace.Propagate(err, "error processing message in taskFn - %s", config.ConsumerName)
		}
		return nil
	}
	return RunWorkQueue(ctx, config, protoTaskFn)
}

func RunWorkQueue(ctx context.Context, config *WorkQueueConfig, taskFn func(*nats.Msg) error) error {

	log.Debug("RunWorkQueue starting %+v", config)
	defer func() {
		log.Debug("RunWorkQueue exiting %+v", config)
	}()

	natsConn, err := ResolveConn(config.Nats)
	if err != nil {
		return stacktrace.Propagate(err, "ResolveNatsConn error")
	}

	if config.Nats.NatsConn == nil {
		defer func() {
			natsConn.Drain()
			natsConn.Close()
		}()
	}

	streamInfo, _ := natsConn.StreamInfo(config.StreamName)
	if streamInfo == nil {
		if config.CreateStream {
			log.Debug("creating stream %s for subject %s", config.StreamName, config.Subject)
			streamInfo, err = natsConn.AddStream(
				&nats.StreamConfig{
					Name:        config.StreamName.String(),
					Description: fmt.Sprintf("stream for %s", config.ConsumerName),
					Subjects:    []string{config.Subject.String()},
					Retention:   config.RetentionPolicy,
					Storage:     nats.FileStorage,
					AllowDirect: true,
					MaxMsgs:     config.MaxMsgs,
					MaxBytes:    config.MaxBytes,
					MaxAge:      config.MaxAge,
				},
			)
			if err != nil {
				return stacktrace.Propagate(err, "error creating stream for %s", config.ConsumerName)
			}
		} else {
			return stacktrace.NewError("stream %s does not exists aka no streamInfo and CreateStream = false", config.StreamName)

		}
	}
	if streamInfo == nil {
		return stacktrace.NewError("no streamInfo means no stream for %s", config.ConsumerName)
	}

	subOpts := []nats.SubOpt{
		nats.MaxDeliver(config.MaxDeliver),
		nats.BindStream(config.StreamName.String()),
	}

	if config.StartSeq != "" {
		_, so := StartSeqToSubscriptionOptions(config.StartSeq)
		subOpts = append(subOpts, so)
	}

	if config.ConsumerName == "" {
		subOpts = append(subOpts, nats.InactiveThreshold(24*time.Hour))
	}

	subscription, err := natsConn.JetStream().PullSubscribe(config.Subject.String(), config.ConsumerName.String(), subOpts...)
	if err != nil {
		return stacktrace.Propagate(err, "PullSubscribe error - %s", config.ConsumerName)
	}

	batchSize := config.BatchSize
	if batchSize == 0 {
		batchSize = 1
	}

	log.Debug("starting RunWorkQueue %s nats read loop - subject=%s  stream=%s  consumer=%s", natsConn.Label(), config.Subject, config.StreamName, config.ConsumerName)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			msgArr, err := subscription.Fetch(batchSize, nats.MaxWait(config.FetchTimeoutMillis*int(time.Millisecond)))
			if err != nil {
				if errors.Is(err, nats.ErrTimeout) {
					if len(msgArr) > 0 {
						log.Debug("timeout on fetch from subscription - %s", config.ConsumerName)
					}
					continue
				} else if ctx.Err() != nil {
					log.Trace("context done - %s", config.ConsumerName)
				} else {
					return stacktrace.Propagate(err, "error on fetch from subscription - %s", config.ConsumerName)
				}
			}
			for _, msg := range msgArr {

				// if log.IsTraceEnabled {
				// 	log.Trace("received nats message %+v", msg)
				// }

				nak := func() {
					err := msg.Nak()
					if err != nil {
						log.Error("unable to nak %v", msg)
					} else if log.IsTraceEnabled {
						log.Trace("nak'ed")
					}
				}

				err = taskFn(msg)
				if err != nil {
					err = stacktrace.Propagate(err, "error on processing task %s nats - %s", natsConn.Label(), config.ConsumerName)
					log.Error(err.Error())
					nak()
					continue
				} else {
					msg.Ack()
				}
			}

		}
	}
}

func UnmarshalBytes[A proto.Message](bytes []byte, useProto bool) (*A, error) {
	var a A // Constrained to proto.Message

	// Peek the type inside T (as T= *SomeProtoMsgType)
	aType := reflect.TypeOf(a).Elem()

	// Make a new one, and throw it back into msg
	a, ok := reflect.New(aType).Interface().(A)
	if !ok {
		return nil, stacktrace.NewError("unable to cast to %v", aType)
	}

	if useProto {
		err := proto.Unmarshal(bytes, a)
		if err != nil {
			return nil, stacktrace.Propagate(err, "error unmarshalling protobuf %s", a.ProtoReflect().Descriptor().FullName())
		}
		return &a, nil
	} else {
		err := protojson.Unmarshal(bytes, a)
		if err != nil {
			return nil, stacktrace.Propagate(err, "error unmarshalling json")
		}
		return &a, nil
	}
}

func Unmarshal[A proto.Message](natsMsg *nats.Msg, assumeProto bool) (*A, error) {

	useProto := assumeProto
	if natsMsg.Header != nil {
		contentType := strings.ToLower(natsMsg.Header.Get("Content-Type"))

		if contentType == "json" || contentType == "application/json" {
			useProto = false
		}

		if contentType == "proto" || contentType == "protobuf" || contentType == "application/protobuf" {
			useProto = true
		}

	}
	return UnmarshalBytes[A](natsMsg.Data, useProto)

}

type NatsConnI interface {
	ActualConn() *nats.Conn
	JetStream() nats.JetStreamContext
	Drain() error
	Close()
	JetStreamPublish(subject Subject, data []byte, idempotentId model.IdempotentId) (*nats.PubAck, error)
	StreamInfo(streamName StreamName) (*nats.StreamInfo, error)
	AddStream(cfg *nats.StreamConfig, opts ...nats.JSOpt) (*nats.StreamInfo, error)
	Publish(subj Subject, data []byte) error
	DeleteStream(streamName StreamName) error
	Label() string
}

type natsConnS struct {
	conn      *nats.Conn
	jetStream nats.JetStreamContext
	args      *ConnArgs
}

func (nc *natsConnS) Conn() *nats.Conn {
	return nc.conn
}

func (nc *natsConnS) Drain() error {
	return nc.conn.Drain()
}

func (nc *natsConnS) DeleteStream(streamName StreamName) error {
	return nc.jetStream.DeleteStream(streamName.String())
}

func (nc *natsConnS) Close() {
	nc.conn.Close()
}

func (nc *natsConnS) JetStream() nats.JetStreamContext {
	return nc.jetStream
}

func NewConn(args ConnArgs) (NatsConnI, error) {

	if args.Context == nil {
		args.Context = a8.GlobalApp().Context()
	}

	natsUrl := args.NatsUrl

	options := []nats.Option{}

	addOption := func(opt nats.Option) {
		options = append(options, opt)
	}

	if args.MaxReconnects > 0 {
		addOption(nats.MaxReconnects(args.MaxReconnects))
	} else {
		addOption(nats.MaxReconnects(-1))
	}

	if args.Name != "" {
		addOption(nats.Name(args.Name))
	}

	addOption(nats.SetCustomDialer(args))

	addOption(nats.ReconnectWait(2 * time.Second))

	addOption(
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			if log.IsTraceEnabled {
				log.Trace("disconnected from %s nats - %s - %v", args.Label(), natsUrl, err)
			}
		}),
	)

	addOption(
		nats.ReconnectHandler(func(nc *nats.Conn) {
			if log.IsTraceEnabled {
				log.Trace("reconnected to %s nats - %s", args.Label(), natsUrl)
			}
		}),
	)

	addOption(
		nats.ConnectHandler(func(nc *nats.Conn) {
			if log.IsTraceEnabled {
				log.Trace("connected to %s nats - %s", args.Label(), natsUrl)
			}
		}),
	)

	conn, err := nats.Connect(natsUrl, options...)
	if err != nil {
		return nil, stacktrace.Propagate(err, "unable to connect to %s nats - %s", args.Label(), natsUrl)
	}

	js, err := conn.JetStream()
	if err != nil {
		return nil, stacktrace.Propagate(err, "unable to connect to %s nats - %s", args.Label(), natsUrl)
	}

	nc := &natsConnS{
		conn:      conn,
		jetStream: js,
		args:      &args,
	}

	return nc, nil

}

func (nc *natsConnS) Label() string {
	label := nc.args.Name
	if label == "" {
		label = nc.args.NatsUrl
	}
	return label
}

func (nc *natsConnS) ActualConn() *nats.Conn {
	return nc.conn
}

func (nc *natsConnS) JetStreamPublish(subject Subject, data []byte, idempotentId model.IdempotentId) (*nats.PubAck, error) {
	if log.IsTraceEnabled {
		log.Trace("publishing message to subject %s", subject)
	}
	if idempotentId.IsEmpty() {
		return nc.jetStream.Publish(subject.String(), data, nats.MsgId(idempotentId.String()))
	} else {
		return nc.jetStream.Publish(subject.String(), data)
	}
}

func (nc *natsConnS) StreamInfo(streamName StreamName) (*nats.StreamInfo, error) {
	return nc.jetStream.StreamInfo(streamName.String())
}

func (nc *natsConnS) AddStream(cfg *nats.StreamConfig, opts ...nats.JSOpt) (*nats.StreamInfo, error) {
	return nc.jetStream.AddStream(cfg, opts...)
}

func (nc *natsConnS) Publish(subj Subject, data []byte) error {
	return nc.conn.Publish(subj.String(), data)
}

type ConnArgs struct {
	NatsUrl       string
	NatsConn      NatsConnI
	Name          string
	MaxReconnects int
	Context       context.Context
	RetryWait     time.Duration
	label         string
}

func ResolveConn(args ConnArgs) (NatsConnI, error) {
	if args.NatsConn != nil {
		return args.NatsConn, nil
	} else {
		return NewConn(args)
	}
}

type Subject string
type StreamName string

type ConsumerName string

func (s StreamName) String() string {
	return string(s)
}

func (s ConsumerName) String() string {
	return string(s)
}
func NewNatsConsumerName(s string) ConsumerName {
	return ConsumerName(s)
}

func (s Subject) String() string {
	return string(s)
}

func NewConsumerName(s string) ConsumerName {
	return ConsumerName(s)
}

func NewStreamName(s string) StreamName {
	return StreamName(s)
}

func NewSubject(s string) Subject {
	return Subject(s)
}

func (args ConnArgs) Label() string {
	if args.label == "" {
		args.label = args.Name
	}
	return args.label
}

func (args ConnArgs) Dial(network, address string) (net.Conn, error) {

	ctx := args.Context

	retryWait := args.RetryWait
	if retryWait == 0 {
		retryWait = 5 * time.Second
	}

	ticker := time.NewTicker(retryWait)
	defer ticker.Stop()

	for {
		log.Debug("NATS Dial %s Attempting to connect to %s", args.Label(), address)
		dialer := &net.Dialer{}
		if conn, err := dialer.DialContext(ctx, network, address); err == nil {
			log.Debug("NATS Dial %s Connected successfully to %s", args.Label(), address)
			return conn, nil
		} else {
			log.Debug("NATS Dial %s Unable to connect to %s, retrying in %v - %v", args.Label(), address, retryWait, err)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func StartSeqToSubscriptionOptions(startSeq string) (string, nats.SubOpt) {
	switch strings.ToLower(startSeq) {
	case "last":
		return "last", nats.DeliverLast()
	case "tail":
		return "new", nats.DeliverNew()
	case "new":
		return "new", nats.DeliverNew()
	case "first":
		return "all", nats.DeliverAll()
	case "all":
		return "all", nats.DeliverAll()
	case "":
		return "all", nats.DeliverAll()
	default:
		ssn, err := strconv.ParseUint(startSeq, 10, 64)
		if err != nil {
			if log.IsTraceEnabled {
				log.Debug("unable to parse %s to uint64 defaulting to DeliverAll", startSeq)
			}
			return "all", nats.DeliverAll()
		} else {
			return fmt.Sprintf("%v", ssn), nats.StartSequence(ssn)
		}
	}

}
