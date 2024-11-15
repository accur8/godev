package publisher

import (
	"context"
	"fmt"
	"log/slog"

	"accur8.io/godev/a8nats"
	"accur8.io/godev/log"
	"github.com/goccy/go-json"
	"github.com/nats-io/nats.go"
)

// NatsPublisher represent event publisher.
type NatsPublisher struct {
	conn a8nats.NatsConnI
	// conn   *nats.Conn
	// js     nats.JetStreamContext
	// logger *slog.Logger
}

// NewNatsPublisher return new NatsPublisher instance.
func NewNatsPublisher(conn a8nats.NatsConnI) (*NatsPublisher, error) {
	return &NatsPublisher{conn: conn}, nil
}

// Close connection.
func (n NatsPublisher) Close() error {
	n.conn.Close()
	return nil
}

// Publish serializes the event and publishes it on the bus.
func (n NatsPublisher) Publish(_ context.Context, subject string, event Event) error {
	msg, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal err: %w", err)
	}

	fmt.Println("publishing msg: ", string(msg))
	if err := n.conn.Publish(a8nats.NewSubject(subject), msg); err != nil {
		return fmt.Errorf("failed to publish: %w", err)
	}
	fmt.Println("published msg: ", string(msg))

	return nil
}

// CreateStream creates a stream by using JetStreamContext. We can do it manually.
func (n NatsPublisher) CreateStream(streamName string) error {
	stream, err := n.conn.StreamInfo(a8nats.NewStreamName(streamName))
	if err != nil {
		log.Warn("failed to get stream info %s", err)
	}

	if stream == nil {
		streamSubjects := streamName + ".>"

		if _, err = n.conn.AddStream(&nats.StreamConfig{
			Name:     streamName,
			Subjects: []string{streamSubjects},
		}); err != nil {
			return fmt.Errorf("add stream: %w", err)
		}

		log.Info("stream not exists, created %ss", slog.String("subjects", streamSubjects))
	}

	return nil
}
