package main

import (
	"context"

	"accur8.io/godev/a8"
	"accur8.io/godev/a8nats"
	"accur8.io/godev/log"
	"github.com/nats-io/nats.go"
)

func main() {
	// a8.RunMultiRoutineApp(App, App2)
	a8.RunMultiRoutineApp(
		func(ctx context.Context) error {
			return App(ctx, "A")
		},
		func(ctx context.Context) error {
			return App(ctx, "B")
		},
	)
	// a8.RunMultiRoutineApp(App2)
}

func App(ctx context.Context, label string) error {

	processMessage := func(msg *nats.Msg) error {
		log.Debug("received message %s - %s", label, string(msg.Data))
		return nil
	}

	config := &a8nats.WorkQueueConfig{
		Nats: a8nats.ConnArgs{
			NatsUrl: "nats://127.0.0.1:4222",
			Name:    "test-a8nats",
		},
		StreamName:         a8nats.NewStreamName("bob-stream"),
		Subject:            a8nats.NewSubject("bob"),
		ConsumerName:       a8nats.NewNatsConsumerName("test-a8nats"),
		RetentionPolicy:    nats.LimitsPolicy,
		AssumeProto:        false,
		MaxDeliver:         5,
		FetchTimeoutMillis: 1_000,
	}

	return a8nats.RunWorkQueue(ctx, config, processMessage)

}

func App2(ctx context.Context) error {

	natsConn, err := nats.Connect("nats://127.0.0.1:4222")
	if err != nil {
		return err
	}

	err = natsConn.Publish("foo", []byte("hello world"))
	if err != nil {
		return err
	}

	js, err := natsConn.JetStream()
	if err != nil {
		return err
	}

	si, err := js.StreamInfo("bob-stream")
	if err != nil {
		return err
	}

	log.Debug("stream info - %+v", si)

	log.Debug("boom")

	return nil

}
