package cdc

import (
	"context"
	"encoding/json"
	"strings"

	"accur8.io/godev/a8nats"
	"accur8.io/godev/hermes/api"
	"github.com/nats-io/nats.go"
	"github.com/palantir/stacktrace"
)

type ChangeDataCaptureEvent struct {
	Id         string          `json:"id"`
	Schema     string          `json:"schema"`
	Table      string          `json:"table"`
	Action     string          `json:"action"`
	Data       json.RawMessage `json:"data"`
	CommitTime string          `json:"commitTime"`
}

type ChangeDataCaptureConfig struct {
	Ctx          context.Context
	NatsConn     a8nats.NatsConnI
	Root         api.ChangeDataCaptureSubjectRoot
	Database     api.DatabaseName
	Table        api.TableName
	StartSeq     string
	ConsumerName a8nats.ConsumerName
}

func RunChangeDataCapture[RowType any](cfg ChangeDataCaptureConfig, listener func(event *ChangeDataCaptureEvent, row *RowType) error) error {

	root := cfg.Root
	database := cfg.Database
	table := cfg.Table

	parts := []string{root.String(), string(database), string(table), ">"}

	subject := a8nats.Subject(strings.Join(parts, "."))

	messageHandler := func(natsMsg *nats.Msg) error {
		cdcJson := string(natsMsg.Data)
		var cdcEvent ChangeDataCaptureEvent
		err := json.Unmarshal(natsMsg.Data, &cdcEvent)
		if err != nil {
			return stacktrace.Propagate(err, "Error unmarshalling ChangeDataCaptureEvent: %s", cdcJson)
		}
		row, err := JsonUnmarshalAny[RowType](cdcEvent.Data)
		if err != nil {
			return stacktrace.Propagate(err, "Error unmarshalling ChangeDataCaptureEvent.Data: %s", cdcJson)
		}
		err = listener(&cdcEvent, row)
		if err != nil {
			return stacktrace.Propagate(err, "error processing ChangeDataCaptureEvent: %s", cdcJson)
		}
		return nil
	}

	if cfg.ConsumerName == "" && cfg.StartSeq == "" {
		cfg.StartSeq = "new"
	}

	return a8nats.RunWorkQueue(
		cfg.Ctx,
		&a8nats.WorkQueueConfig{
			Nats: a8nats.ConnArgs{
				NatsConn: cfg.NatsConn,
			},
			Subject:      subject,
			CreateStream: false,
			StreamName:   "wal_listener",
			ConsumerName: cfg.ConsumerName,
			StartSeq:     cfg.StartSeq,
		},
		messageHandler,
	)

}

func JsonUnmarshalAny[A any](bytes []byte) (*A, error) {

	var a A

	// // Peek the type inside T (as T= *SomeProtoMsgType)
	// aType := reflect.TypeOf(a) //.Elem()

	// // Make a new one, and throw it back into msg
	// a, ok := reflect.New(aType).Interface().(A)
	// if !ok {
	// 	return nil, stacktrace.NewError("unable to cast to %v", aType)
	// }

	err := json.Unmarshal(bytes, &a)
	if err != nil {
		return nil, stacktrace.Propagate(err, "error unmarshalling json")
	}
	return &a, nil

}
