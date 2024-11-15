package listener

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"time"

	"accur8.io/godev/log"
	"github.com/jackc/pgx"
	"golang.org/x/sync/errgroup"

	"accur8.io/godev/wal-listener/config"
	"accur8.io/godev/wal-listener/publisher"
)

// Logical decoding plugin.
const pgOutputPlugin = "pgoutput"

type eventPublisher interface {
	Publish(context.Context, string, publisher.Event) error
}

type parser interface {
	ParseWalMessage([]byte, *WalTransaction) error
}

type replication interface {
	CreateReplicationSlotEx(slotName, outputPlugin string) (consistentPoint string, snapshotName string, err error)
	DropReplicationSlot(slotName string) (err error)
	StartReplication(slotName string, startLsn uint64, timeline int64, pluginArguments ...string) (err error)
	WaitForReplicationMessage(ctx context.Context) (*pgx.ReplicationMessage, error)
	SendStandbyStatus(k *pgx.StandbyStatus) (err error)
	IsAlive() bool
	Close() error
}

type repository interface {
	CreatePublication(name string) error
	GetSlotLSN(slotName string) (string, error)
	NewStandbyStatus(walPositions ...uint64) (status *pgx.StandbyStatus, err error)
	IsAlive() bool
	Close() error
}

type monitor interface {
	IncPublishedEvents(subject, table string)
	IncFilterSkippedEvents(table string)
	IncProblematicEvents(kind string)
}

// Listener main service struct.
type Listener struct {
	cfg        *config.Config
	monitor    monitor
	mu         sync.RWMutex
	publisher  eventPublisher
	replicator replication
	repository repository
	parser     parser
	lsn        uint64
}

// NewWalListener create and initialize new service instance.
func NewWalListener(
	cfg *config.Config,
	repo repository,
	repl replication,
	pub eventPublisher,
	parser parser,
	monitor monitor,
) *Listener {
	return &Listener{
		monitor:    monitor,
		cfg:        cfg,
		publisher:  pub,
		repository: repo,
		replicator: repl,
		parser:     parser,
	}
}

// Process is main service entry point.
func (l *Listener) Process(ctx context.Context) error {

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	log.Info("service was started")

	if err := l.repository.CreatePublication(publicationName); err != nil {
		log.Warn("publication creation was skipped %s", err)
	}

	slotIsExists, err := l.slotIsExists()
	if err != nil {
		return fmt.Errorf("slot is exists: %w", err)
	}

	if !slotIsExists {
		consistentPoint, _, err := l.replicator.CreateReplicationSlotEx(l.cfg.Listener.SlotName, pgOutputPlugin)
		if err != nil {
			return fmt.Errorf("create replication slot: %w", err)
		}

		lsn, err := pgx.ParseLSN(consistentPoint)
		if err != nil {
			return fmt.Errorf("parse lsn: %w", err)
		}

		l.setLSN(lsn)

		log.Info("new slot was created %s", slog.String("slot", l.cfg.Listener.SlotName))
	} else {
		log.Info("slot already exists, LSN updated")
	}

	group := new(errgroup.Group)

	group.Go(func() error {
		err := l.Stream(ctx)
		if err != nil {
			log.Error(err.Error())
		}
		return err
	})
	group.Go(func() error {
		return l.checkConnection(ctx)
	})

	if err = group.Wait(); err != nil {
		return err
	}

	return nil
}

// checkConnection periodically checks connections.
func (l *Listener) checkConnection(ctx context.Context) error {
	refresh := time.NewTicker(l.cfg.Listener.RefreshConnection)
	defer refresh.Stop()

	for {
		select {
		case <-refresh.C:
			if !l.replicator.IsAlive() {
				return fmt.Errorf("replicator: %w", errReplConnectionIsLost)
			}

			if !l.repository.IsAlive() {
				return fmt.Errorf("repository: %w", errConnectionIsLost)
			}
		case <-ctx.Done():
			log.Debug("cgeck connection: context was canceled")

			if err := l.Stop(); err != nil {
				log.Error("failed to stop service -- %s", err)
			}

			return nil
		}
	}
}

// slotIsExists checks whether a slot has already been created and if it has been created uses it.
func (l *Listener) slotIsExists() (bool, error) {
	restartLSNStr, err := l.repository.GetSlotLSN(l.cfg.Listener.SlotName)
	if err != nil {
		return false, fmt.Errorf("get slot lsn: %w", err)
	}

	if len(restartLSNStr) == 0 {
		log.Warn("restart LSN not found %s", slog.String("slot_name", l.cfg.Listener.SlotName))
		return false, nil
	}

	lsn, err := pgx.ParseLSN(restartLSNStr)
	if err != nil {
		return false, fmt.Errorf("parse lsn: %w", err)
	}

	l.setLSN(lsn)

	return true, nil
}

const (
	protoVersion    = "proto_version '1'"
	publicationName = "wal-listener"
)

const (
	problemKindParse   = "parse"
	problemKindPublish = "publish"
	problemKindAck     = "ack"
)

// Stream receive event from PostgreSQL.
// Accept message, apply filter and  publish it in NATS server.
func (l *Listener) Stream(ctx context.Context) error {
	if err := l.replicator.StartReplication(
		l.cfg.Listener.SlotName,
		l.readLSN(),
		-1,
		protoVersion,
		publicationNames(publicationName),
	); err != nil {
		return fmt.Errorf("start replication: %w", err)
	}

	go l.SendPeriodicHeartbeats(ctx)

	tx := NewWalTransaction(l.monitor)

	for {
		if err := ctx.Err(); err != nil {
			log.Warn("stream: context canceled -- %s", err)
			return nil
		}

		msg, err := l.replicator.WaitForReplicationMessage(ctx)
		if err != nil {
			return fmt.Errorf("wait for replication message: %w", err)
		}

		if msg == nil {
			log.Debug("got empty message")
			continue
		}

		if err = l.processMessage(ctx, msg, tx); err != nil {
			log.Error(fmt.Errorf("process message: %w", err).Error())
		}

		l.processHeartBeat(msg)
	}
}

func (l *Listener) processMessage(ctx context.Context, msg *pgx.ReplicationMessage, tx *WalTransaction) error {
	if msg.WalMessage == nil {
		log.Debug("empty wal-message")
		return nil
	}

	log.Debug("WAL message has been received -- %s", slog.Uint64("wal", msg.WalMessage.WalStart))

	if err := l.parser.ParseWalMessage(msg.WalMessage.WalData, tx); err != nil {
		l.monitor.IncProblematicEvents(problemKindParse)
		return fmt.Errorf("parse: %w", err)
	}

	if tx.CommitTime != nil {
		for _, event := range tx.CreateEventsWithFilter(l.cfg.Listener.Filter.Tables) {
			subjectName := event.SubjectName(l.cfg)

			if err := l.publisher.Publish(ctx, subjectName, event); err != nil {
				l.monitor.IncProblematicEvents(problemKindPublish)
				return fmt.Errorf("publish: %w", err)
			}

			l.monitor.IncPublishedEvents(subjectName, event.Table)

			log.Info(
				"event was sent %s %s %s %s",
				slog.String("subject", subjectName),
				slog.String("action", event.Action),
				slog.String("table", event.Table),
				slog.Uint64("lsn", l.readLSN()),
			)
		}

		tx.Clear()
	}

	if msg.WalMessage.WalStart > l.readLSN() {
		if err := l.AckWalMessage(msg.WalMessage.WalStart); err != nil {
			l.monitor.IncProblematicEvents(problemKindAck)
			return fmt.Errorf("ack: %w", err)
		}

		log.Debug("ack WAL message %s", slog.Uint64("lsn", l.readLSN()))
	}

	return nil
}

func (l *Listener) processHeartBeat(msg *pgx.ReplicationMessage) {
	if msg.ServerHeartbeat == nil {
		return
	}

	log.Debug(
		"received server heartbeat %s %s",
		slog.Uint64("server_wal_end", msg.ServerHeartbeat.ServerWalEnd),
		slog.Uint64("server_time", msg.ServerHeartbeat.ServerTime),
	)

	if msg.ServerHeartbeat.ReplyRequested == 1 {
		log.Debug("status requested")

		if err := l.SendStandbyStatus(); err != nil {
			log.Warn("send standby status: %v", err)
		}
	}
}

func publicationNames(publication string) string {
	return fmt.Sprintf(`publication_names '%s'`, publication)
}

// Stop is a finalizer function.
func (l *Listener) Stop() error {
	if err := l.repository.Close(); err != nil {
		return fmt.Errorf("repository close: %w", err)
	}

	if err := l.replicator.Close(); err != nil {
		return fmt.Errorf("replicator close: %w", err)
	}

	log.Info("service was stopped")

	return nil
}

// SendPeriodicHeartbeats send periodic keep alive heartbeats to the server.
func (l *Listener) SendPeriodicHeartbeats(ctx context.Context) {
	heart := time.NewTicker(l.cfg.Listener.HeartbeatInterval)
	defer heart.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Warn("periodic heartbeats: context was canceled")
			return
		case <-heart.C:
			if err := l.SendStandbyStatus(); err != nil {
				log.Error("failed to send heartbeat status %s", err)
				continue
			}

			log.Debug("sending periodic heartbeat status")
		}
	}
}

// SendStandbyStatus sends a `StandbyStatus` object with the current RestartLSN value to the server.
func (l *Listener) SendStandbyStatus() error {
	lsn := l.readLSN()

	standbyStatus, err := l.repository.NewStandbyStatus(lsn)
	if err != nil {
		return fmt.Errorf("unable to create StandbyStatus object: %w", err)
	}

	standbyStatus.ReplyRequested = 0

	if err := l.replicator.SendStandbyStatus(standbyStatus); err != nil {
		return fmt.Errorf("unable to send StandbyStatus object: %w", err)
	}

	return nil
}

// AckWalMessage acknowledge received wal message.
func (l *Listener) AckWalMessage(lsn uint64) error {
	l.setLSN(lsn)

	if err := l.SendStandbyStatus(); err != nil {
		return fmt.Errorf("send status: %w", err)
	}

	return nil
}

func (l *Listener) readLSN() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return l.lsn
}

func (l *Listener) setLSN(lsn uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.lsn = lsn
}
