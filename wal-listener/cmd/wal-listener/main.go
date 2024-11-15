package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"os"

	scfg "github.com/ihippik/config"
	"github.com/palantir/stacktrace"
	"github.com/urfave/cli/v2"

	"accur8.io/godev/a8nats"
	"accur8.io/godev/log"
	"accur8.io/godev/wal-listener/config"
	"accur8.io/godev/wal-listener/listener"
	"accur8.io/godev/wal-listener/publisher"
	"github.com/jackc/pgx"
)

func main() {
	cli.VersionFlag = &cli.BoolFlag{
		Name:    "version",
		Aliases: []string{"v"},
		Usage:   "print only the version",
	}

	version := scfg.GetVersion()

	app := &cli.App{
		Name:    "WAL-Listener",
		Usage:   "listen PostgreSQL events",
		Version: version,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Value:   "config.yml",
				Aliases: []string{"c"},
				Usage:   "path to config file",
			},
		},
		Action: func(c *cli.Context) error {
			cfg, err := config.InitConfig(c.String("config"))
			if err != nil {
				return fmt.Errorf("get config: %w", err)
			}

			if err = cfg.Validate(); err != nil {
				return fmt.Errorf("validate config: %w", err)
			}

			conn, rConn, err := initPgxConnections(cfg.Database)
			if err != nil {
				return fmt.Errorf("pgx connection: %w", err)
			}

			pub, err := factoryPublisher(c.Context, cfg.Publisher)
			if err != nil {
				return fmt.Errorf("factory publisher: %w", err)
			}

			defer func() {
				if err := pub.Close(); err != nil {
					log.Error("close publisher failed err=%s", err.Error())
				}
			}()

			service := listener.NewWalListener(
				cfg,
				listener.NewRepository(conn),
				rConn,
				pub,
				listener.NewBinaryParser(binary.BigEndian),
				config.NewMetrics(),
			)

			if err := service.Process(c.Context); err != nil {
				slog.Error("service process failed", "err", err.Error())
			}

			return nil
		},
	}

	if err := app.Run(os.Args); err != nil {
		slog.Error("service error", "err", err)
	}
}

// initPgxConnections initialise db and replication connections.
func initPgxConnections(cfg *config.DatabaseCfg) (*pgx.Conn, *pgx.ReplicationConn, error) {
	pgxConf := pgx.ConnConfig{
		LogLevel: pgx.LogLevelInfo,
		Logger:   pgxLogger{},
		Host:     cfg.Host,
		Port:     cfg.Port,
		Database: cfg.Name,
		User:     cfg.User,
		Password: cfg.Password,
	}

	pgConn, err := pgx.Connect(pgxConf)
	if err != nil {
		return nil, nil, fmt.Errorf("db connection: %w", err)
	}

	rConnection, err := pgx.ReplicationConnect(pgxConf)
	if err != nil {
		return nil, nil, fmt.Errorf("replication connect: %w", err)
	}

	return pgConn, rConnection, nil
}

type pgxLogger struct {
}

// Log DB message.
func (l pgxLogger) Log(_ pgx.LogLevel, msg string, _ map[string]any) {
	log.Debug(msg)
}

type eventPublisher interface {
	Publish(context.Context, string, publisher.Event) error
	Close() error
}

// factoryPublisher represents a factory function for creating a eventPublisher.
func factoryPublisher(ctx context.Context, cfg *config.PublisherCfg) (eventPublisher, error) {
	switch cfg.Type {
	// case config.PublisherTypeKafka:
	// 	producer, err := publisher.NewProducer(cfg)
	// 	if err != nil {
	// 		return nil, fmt.Errorf("kafka producer: %w", err)
	// 	}

	// 	return publisher.NewKafkaPublisher(producer), nil
	case config.PublisherTypeNats:
		conn, err := a8nats.NewConn(a8nats.ConnArgs{
			NatsUrl: cfg.Address,
		})
		if err != nil {
			return nil, err
		}

		pub, err := publisher.NewNatsPublisher(conn)
		if err != nil {
			return nil, stacktrace.Propagate(err, "new nats publisher")
		}

		if err := pub.CreateStream(cfg.Topic); err != nil {
			return nil, fmt.Errorf("create stream: %w", err)
		}

		return pub, nil
	// case config.PublisherTypeRabbitMQ:
	// 	conn, err := publisher.NewConnection(cfg)
	// 	if err != nil {
	// 		return nil, fmt.Errorf("new connection: %w", err)
	// 	}

	// 	p, err := publisher.NewPublisher(cfg.Topic, conn)
	// 	if err != nil {
	// 		return nil, fmt.Errorf("new publisher: %w", err)
	// 	}

	// 	pub, err := publisher.NewRabbitPublisher(cfg.Topic, conn, p)
	// 	if err != nil {
	// 		return nil, fmt.Errorf("new rabbit publisher: %w", err)
	// 	}

	// 	return pub, nil
	// case config.PublisherTypeGooglePubSub:
	// 	pubSubConn, err := publisher.NewPubSubConnection(ctx, logger, cfg.PubSubProjectID)
	// 	if err != nil {
	// 		return nil, fmt.Errorf("could not create pubsub connection: %w", err)
	// 	}

	// 	return publisher.NewGooglePubSubPublisher(pubSubConn), nil
	default:
		return nil, fmt.Errorf("unknown publisher type: %s", cfg.Type)
	}
}
