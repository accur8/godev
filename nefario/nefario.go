package nefario

import (
	context "context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	"accur8.io/godev/a8"
	"accur8.io/godev/a8nats"
	"accur8.io/godev/cdc"
	base "accur8.io/godev/hermes/api"
	"accur8.io/godev/hermes/model"
	"accur8.io/godev/hermes/rpcserver"
	"accur8.io/godev/iter"
	"accur8.io/godev/iterable"
	log "accur8.io/godev/log"
	"accur8.io/godev/nefario/db"
	"accur8.io/godev/nefario/rpc"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/palantir/stacktrace"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type NefarioServiceImpl struct {
	ConnectionPool *pgxpool.Pool
	NatsConn       a8nats.NatsConnI
}

func WithQueries[Req interface{}](ctx context.Context, name string, req *Req, ns *NefarioServiceImpl, queriesFn func(queries *db.Queries) error) error {
	label := fmt.Sprintf("WithQueries %s processing %s", name, a8.ToJson(req))
	log.Debug("%s - started", label)
	withConn := func(conn *pgxpool.Conn) error {
		queries := db.New(conn)
		return queriesFn(queries)
	}
	err := ns.ConnectionPool.AcquireFunc(ctx, withConn)
	if err != nil {
		log.Debug("%s - completed in error - %v", label, err)
	} else {
		log.Debug("%s - completed successfully", label)
	}
	return err
}

func toText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{Valid: false}
	} else {
		return pgtype.Text{String: s, Valid: true}
	}
}

func ProcessSubjectName(processUid string, channelName string) a8nats.Subject {
	processUid = ScrubSubjectPart(processUid)
	channelName = ScrubSubjectPart(channelName)
	return a8nats.NewSubject(fmt.Sprintf("launchy.%s.%s", processUid, channelName))
}

func ProcessStreamName(processUid string, channelName string) a8nats.StreamName {
	processUid = ScrubSubjectPart(processUid)
	channelName = ScrubSubjectPart(channelName)
	return a8nats.NewStreamName(fmt.Sprintf("launchy-%s-%s", processUid, channelName))
}

func (ns *NefarioServiceImpl) AddStream(req *rpc.ProcessStartedRequest, channelName string) error {

	subjectName := ProcessSubjectName(req.ProcessUid, channelName)
	streamName := ProcessStreamName(req.ProcessUid, channelName)

	if log.IsTraceEnabled {
		log.Trace("AddStream(%+v, %+v) -> Subject: %+v  Stream: %+v", req.ProcessUid, channelName, subjectName, streamName)
	}

	// create nats streams here
	si, err := ns.NatsConn.AddStream(
		&nats.StreamConfig{
			Name:        streamName.String(),
			Subjects:    []string{subjectName.String()},
			Compression: nats.S2Compression,
			Retention:   nats.LimitsPolicy,
			MaxAge:      time.Duration(24 * time.Hour),
		},
	)

	if err != nil {
		return stacktrace.Propagate(err, "error creating stream %s", streamName)
	}

	if false && log.IsTraceEnabled {
		log.Trace("AddStream(%+v, %+v) -> %+v", req.ProcessUid, channelName, si)
	}

	return nil

}

func (ns *NefarioServiceImpl) ProcessStart(ctx context.Context, req *rpc.ProcessStartedRequest) error {

	withQueries := func(queries *db.Queries) error {
		if req.ExtraDataJsonStr == "" {
			req.ExtraDataJsonStr = "{}"
		}
		extraData := []byte(req.ExtraDataJsonStr)
		err := queries.InsertStartDto(ctx, db.InsertStartDtoParams{
			Uid:                 req.ProcessUid,
			MinionUid:           req.MinionUid,
			Category:            toText(req.Category),
			ProcessStart:        a8.ToJsonBytes(req),
			ParentProcessRunUid: toText(req.ParentProcessRunUid),
			ExtraData:           extraData,
			ServiceUid:          req.ServiceUid,
		})
		if err != nil {
			return stacktrace.Propagate(err, "error inserting process start")
		}

		if req.MinionUid != "" {
			err = queries.UpdateServiceWithRunningProcess(ctx, db.UpdateServiceWithRunningProcessParams{
				Uid:                     req.ServiceUid,
				MostRecentProcessRunUid: toText(req.ProcessUid),
			})
			if err != nil {
				return stacktrace.Propagate(err, "error UpdateRunningProcess in service table")
			}
		}
		return nil

	}

	err := WithQueries(ctx, "ProcessStart", req, ns, withQueries)
	if err != nil {
		return stacktrace.Propagate(err, "ProcessStart error")
	}

	for _, ch := range req.Channels {
		err := ns.AddStream(req, ch)
		if err != nil {
			return stacktrace.Propagate(err, "AddStream error")
		}
	}

	return nil

}

type WalPingRecordDto struct {
	Uid  string          `json:"uid"`
	Data json.RawMessage `json:"data"`
}

func (ns *NefarioServiceImpl) FetchReplicationSlotStatus(ctx context.Context, slotName string) (*db.FetchReplicationSlotStatusRow, error) {

	var row *db.FetchReplicationSlotStatusRow

	action := func(queries *db.Queries) error {
		row0, err0 := queries.FetchReplicationSlotStatus(ctx, pgtype.Text{String: slotName, Valid: true})
		row = &row0
		return err0
	}
	err := WithQueries(ctx, "FetchReplicationSlotStatus", ns, ns, action)
	return row, err
}

type PingResult struct {
	Latency0InMillis int
	Latency1InMillis int
	TimeoutMsg       string
	Err              error
}

func (ns *NefarioServiceImpl) WalPing(ctx context.Context) *PingResult {

	pingRecord := &WalPingRecordDto{
		Uid:  a8.RandomUid(),
		Data: []byte("{}"),
	}

	log.Debug("ping is %s", pingRecord.Uid)

	insertPing := func(record *WalPingRecordDto) error {
		action := func(queries *db.Queries) error {
			return queries.InsertPing(ctx, db.InsertPingParams{
				Uid:  record.Uid,
				Data: record.Data,
			})
		}
		err := WithQueries(ctx, "Insert Ping", record, ns, action)
		if err != nil {
			err = stacktrace.Propagate(err, "Insert Ping error")
			log.Error(err.Error())
		}
		return err
	}

	deletePing := func(record *WalPingRecordDto) error {
		action := func(queries *db.Queries) error {
			return queries.DeletePing(ctx, record.Uid)
		}
		err := WithQueries(ctx, "Delete Ping", record, ns, action)
		if err != nil {
			err = stacktrace.Propagate(err, "Delete Ping error")
			log.Error(err.Error())
		}
		return err
	}

	udpatePing := func(record *WalPingRecordDto) error {
		action := func(queries *db.Queries) error {
			return queries.UpdatePing(ctx, db.UpdatePingParams{
				Uid:  record.Uid,
				Data: record.Data,
			})
		}
		err := WithQueries(ctx, "Update Ping", record, ns, action)
		if err != nil {
			err = stacktrace.Propagate(err, "Update Ping error")
			log.Error(err.Error())
		}
		return err
	}

	Error := func(err error) *PingResult {
		return &PingResult{
			Err: err,
		}
	}

	// make sure we have at least one ping record in the database
	err := insertPing(pingRecord)
	if err != nil {
		return Error(stacktrace.Propagate(err, "error inserting first ping"))
	}

	// then startup cdc
	firstCdcEvent := true
	waitForFirstCdcEvent := make(chan struct{}, 1)

	pingResultCh := make(chan struct{}, 1)

	log.Debug("waiting for ping to be seen as wal / cdc event")

	waitForPing := func(e *cdc.ChangeDataCaptureEvent, record *WalPingRecordDto) error {
		log.Debug("received ping for %s  Commit Time: %s   Action: %s", record.Uid, e.CommitTime, e.Action)
		if firstCdcEvent {
			firstCdcEvent = false
			waitForFirstCdcEvent <- struct{}{}
		}
		if record.Uid == pingRecord.Uid && e.Action == "UPDATE" {
			pingResultCh <- struct{}{}
		}
		return nil
	}

	a8.SubmitGoRoutine(
		"cdc.RunChangeDataCapture",
		func() error {
			return cdc.RunChangeDataCapture[WalPingRecordDto](
				cdc.ChangeDataCaptureConfig{
					Ctx:      ctx,
					NatsConn: ns.NatsConn,
					Root:     "wal_listener",
					Database: "nefario",
					Table:    "wal_ping",
					StartSeq: "last",
				},
				waitForPing,
			)
		},
	)

	log.Debug("waiting for first cdc event")
	select {
	case <-waitForFirstCdcEvent:
		// first cdc event received ready for actual ping
	case <-ctx.Done():
		err = stacktrace.NewError("context cancelled before first cdc event")
		log.Debug("WalPing returning error - %s", err.Error())
		return Error(err)
	}

	pingStart0 := time.Now()
	log.Debug("updating ping record")
	err = udpatePing(pingRecord)
	if err != nil {
		err = stacktrace.Propagate(err, "error updating ping record")
		log.Debug("WalPing returning error - %s", err.Error())
		return Error(err)
	}
	pingStart1 := time.Now()

	log.Debug("deleting ping record")
	err = deletePing(pingRecord)
	if err != nil {
		err = stacktrace.Propagate(err, "error deleting ping record")
		log.Debug("WalPing returning error - %s", err.Error())
		return Error(err)
	}

	select {
	case <-pingResultCh:
		log.Debug("pingResultCh returned value")
		return &PingResult{
			Latency0InMillis: int(time.Since(pingStart0).Milliseconds()),
			Latency1InMillis: int(time.Since(pingStart1).Milliseconds()),
		}
	case <-ctx.Done():
		log.Debug("context timeout before pingResultCh returned value")
		var msg string
		if firstCdcEvent {
			msg = "Ping timed out.  No change data capture records received."
		} else {
			msg = "Ping timed out."
		}
		return &PingResult{
			TimeoutMsg: msg,
		}
	}

}

func (ns *NefarioServiceImpl) ProcessPing(ctx context.Context, req *rpc.ProcessPingRequest) error {
	err := iter.FromSlice(req.Pings).ForEachE(func(ping *rpc.ProcessPing) error {
		withQueries := func(queries *db.Queries) error {
			return queries.UpdateLastPing(ctx, db.UpdateLastPingParams{
				Uid:      ping.ProcessUid,
				LastPing: a8.ToJsonBytes(ping),
			})
		}

		err := WithQueries(ctx, "ProcessPing", req, ns, withQueries)
		if err != nil {
			return stacktrace.Propagate(err, "ProcessStart error")
		}
		return nil
	})

	return err

}

func (ns *NefarioServiceImpl) Ping(req *rpc.PingRequest) error {
	// Implement the Ping method logic here
	// return &rpc.PingResponse{Message: "pong"}, nil
	resp := req
	// &rpc.PingResponse{
	// 	Uid:     req.Uid,
	// 	Payload: req.Payload,
	// }
	bytes, err := a8.ToProtoJsonE(resp)
	if err != nil {
		return stacktrace.Propagate(err, "unable to marshal PingResponse")
	}
	err = ns.NatsConn.Publish(a8nats.NewSubject("nefario-ping."+req.Uid), bytes)
	if err != nil {
		return stacktrace.Propagate(err, "unable to publish PingResponse")
	}
	return nil
}

func (ns *NefarioServiceImpl) UpdateMailbox(ctx context.Context, req *rpc.UpdateMailboxRequest) error {

	withQueries := func(queries *db.Queries) error {
		return queries.UpdateMailbox(
			ctx,
			db.UpdateMailboxParams{
				Uid:     req.ProcessUid,
				Mailbox: toText(req.Mailbox),
			},
		)
	}

	err := WithQueries(ctx, "UpdateMailbox", req, ns, withQueries)
	if err != nil {
		return stacktrace.Propagate(err, "UpdateMailbox error")
	}

	return nil
}

func (ns *NefarioServiceImpl) ProcessCompleted(ctx context.Context, req *rpc.ProcessCompletedRequest) error {

	withQueries := func(queries *db.Queries) error {
		return queries.UpdateProcessComplete(
			ctx,
			db.UpdateProcessCompleteParams{
				Uid:             req.ProcessUid,
				ProcessComplete: a8.ToJsonBytes(req),
			},
		)
	}

	err := WithQueries(ctx, "ProcessCompleted", req, ns, withQueries)
	if err != nil {
		return stacktrace.Propagate(err, "ProcessStart error")
	}

	return nil
}

func (ns *NefarioServiceImpl) ListServers(ctx context.Context) ([]db.Server, error) {

	var servers []db.Server

	withQueries := func(queries *db.Queries) error {
		servers0, err := queries.ListServers(ctx)
		servers = servers0
		return err
	}

	ack := &rpc.Ack{}

	err := WithQueries(ctx, "ListServers", ack, ns, withQueries)
	if err != nil {
		return nil, stacktrace.Propagate(err, "ProcessStart error")
	}

	return servers, nil

}

// nothing but alphanumeric and dashes
func ScrubSubjectPart(subjectPart string) string {
	var runes []rune
	for _, c := range subjectPart {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			runes = append(runes, c)
		}
	}
	return string(runes)
}

func (ns *NefarioServiceImpl) StreamWrite(req *rpc.StreamWrite) error {

	subjectName := ProcessSubjectName(req.ProcessUid, req.ChannelName)

	data, err := proto.Marshal(req.Record)
	if err != nil {
		return stacktrace.Propagate(err, "unable to marshal StreamWrite")
	}

	if log.IsTraceEnabled {
		log.Trace("streamWrite Publish to %s %d bytes", subjectName, len(data))
	}
	return ns.NatsConn.Publish(subjectName, data)
}

type Config struct {
	DatabaseUrl string
	NatsUrl     string
	NatsConn    a8nats.NatsConnI
	Mailbox     base.MailboxI
}

func Run(ctx context.Context, config *Config) error {

	pool, err := pgxpool.New(ctx, config.DatabaseUrl)
	if err != nil {
		return stacktrace.Propagate(err, "unable to create database connection pool")
	}

	natsConn, err := a8nats.ResolveConn(a8nats.ConnArgs{
		NatsUrl:  config.NatsUrl,
		NatsConn: config.NatsConn,
		Name:     "nefario",
	})
	if err != nil {
		return stacktrace.Propagate(err, "unable to connect to nats")
	}

	service := &NefarioServiceImpl{
		ConnectionPool: pool,
		NatsConn:       natsConn,
	}

	type Consumer struct {
		row      db.Server
		ctx      context.Context
		cancelFn context.CancelFunc
	}

	wg := sync.WaitGroup{}

	consumersByUid := map[string]Consumer{}

	runUpdateCycle := func() error {
		servers, err := service.ListServers(ctx)

		if err != nil {
			return stacktrace.Propagate(err, "error retrieving list of servers")
		}

		if len(servers) == 0 {
			return stacktrace.NewError("no servers found, exiting immediately")
		}

		adds := []db.Server{}
		deletes := []Consumer{}

		serversByUid := map[string]db.Server{}
		for _, server := range servers {
			serversByUid[server.Uid] = server
			consumer, ok := consumersByUid[server.Uid]
			if !ok {
				adds = append(adds, server)
			} else if reflect.DeepEqual(consumer.row, server) {
				// they are equal this is a no-op
			} else {
				deletes = append(deletes, consumer)
				adds = append(adds, server)
			}
		}

		for uid := range consumersByUid {
			_, ok := serversByUid[uid]
			if !ok {
				deletes = append(deletes, consumersByUid[uid])
			}
		}

		// shutdown
		for _, c := range deletes {
			log.Debug("shutting down consumer for %+v", c.row)
			c.cancelFn()
			delete(consumersByUid, c.row.Uid)
		}

		// startup
		wg.Add(len(adds))
		for _, server0 := range adds {
			server := server0
			log.Debug("starting consumer for %+v", server)
			consumerCtx, cancelFn := context.WithCancel(ctx)
			consumersByUid[server.Uid] = Consumer{
				row:      server,
				ctx:      consumerCtx,
				cancelFn: cancelFn,
			}
			go func() {
				defer cancelFn()
				e := runNefarioConsumer(consumerCtx, service, &server, &wg)
				if e != nil {
					e = stacktrace.Propagate(e, "error running nefario consumer for %s", server.Name)
					log.Error(e.Error())
				}
			}()
		}
		return nil
	}

	err = SubmitNefarioRpcService(ctx, config.Mailbox, service)
	if err != nil {
		log.Error(stacktrace.Propagate(err, "error submitting nefario rpc service").Error())
	}

	runUpdateCycle()

outer:
	for {

		// for now we don't run this since it just clouds the logs and we aren't adding / removing / changing servers
		if false {
			runUpdateCycle()
		}

		select {
		case <-ctx.Done():
			break outer
		case <-time.After(30 * time.Second):
		}

	}

	wg.Wait()

	return nil

}

func runNefarioConsumer(ctx context.Context, service *NefarioServiceImpl, server *db.Server, wg *sync.WaitGroup) error {

	defer wg.Done()

	log.Debug("runNefarioConsumer() %+v", server.Name)
	// ??? TODO make stream name and subject configurable

	config := &a8nats.WorkQueueConfig{
		Nats: a8nats.ConnArgs{
			NatsUrl: server.NatsUrl,
			Name:    "nefario-" + server.Name,
		},
		StreamName:         a8nats.NewStreamName(model.NefarioCentral),
		Subject:            a8nats.NewSubject(model.NefarioCentral),
		ConsumerName:       a8nats.NewNatsConsumerName(model.NefarioCentral),
		RetentionPolicy:    nats.WorkQueuePolicy,
		AssumeProto:        true,
		MaxDeliver:         5,
		FetchTimeoutMillis: 1_000,
		CreateStream:       true,
	}

	processMessageFromLaunchy := func(msg *rpc.MessageFromLaunchy) error {

		var err error

		switch m := msg.GetTestOneof().(type) {
		case *rpc.MessageFromLaunchy_ProcessStartedRequest:
			err = service.ProcessStart(ctx, m.ProcessStartedRequest)
		case *rpc.MessageFromLaunchy_ProcessPingRequest:
			err = service.ProcessPing(ctx, m.ProcessPingRequest)
		case *rpc.MessageFromLaunchy_UpdateMailboxRequest:
			err = service.UpdateMailbox(ctx, m.UpdateMailboxRequest)
		case *rpc.MessageFromLaunchy_ProcessCompletedRequest:
			err = service.ProcessCompleted(ctx, m.ProcessCompletedRequest)
		case *rpc.MessageFromLaunchy_StreamWrite:
			err = service.StreamWrite(m.StreamWrite)
		case *rpc.MessageFromLaunchy_PingRequest:
			err = service.Ping(m.PingRequest)
		}

		if err != nil {
			err = stacktrace.Propagate(err, "error processing message from launchy %+v", msg)
			log.Error(err.Error())
		}

		return nil

	}

	return a8nats.RunProtoWorkQueue(ctx, config, processMessageFromLaunchy)

}

func (nsi *NefarioServiceImpl) SystemdServiceCrud(req *rpc.SystemdServiceRecordCrudRequest) (*rpc.SystemdServiceRecordCrudResponse, error) {

	ctx := context.Background()

	withQueries := func(queries *db.Queries) error {

		validRecords := iterable.FromSlice(req.CrudActions).Filter(func(crudAction *rpc.SystemdServiceRecordCrud) bool {
			return crudAction.Action == rpc.CrudAction_CrudAction_unspecified || crudAction.Record == nil
		}).IsEmpty()

		if !validRecords {
			return stacktrace.NewError("invalid request, either nil record or unspecified crud action")
		}

		for _, crudAction := range req.CrudActions {
			var err error

			nilRecord := crudAction.Record == nil
			if nilRecord {
				log.Debug("boom")
			}

			systemdUnitJson := []byte(a8.ToProtoJson(crudAction.Record.UnitJson))

			switch crudAction.Action {
			case rpc.CrudAction_insert:
				record := db.InsertServiceParams{
					Uid:             crudAction.Record.Uid,
					Name:            crudAction.Record.Name,
					MinionUid:       crudAction.Record.MinionUid,
					MinionEnabled:   crudAction.Record.MinionEnabled,
					SystemdUnitJson: systemdUnitJson,
				}
				err = queries.InsertService(ctx, record)

			case rpc.CrudAction_update:
				record := db.UpdateServiceParams{
					Uid:             crudAction.Record.Uid,
					SystemdUnitJson: systemdUnitJson,
				}
				err = queries.UpdateService(ctx, record)

				// case rpc.CrudAction_delete:
				// serviceUnitJson.UnitExists = false
				// record := db.UpdateServiceParams{
				// 	Uid:             crudAction.Record.Uid,
				// 	SystemdUnitJson: systemdUnitJson,
				// }
				// err = queries.UpdateService(ctx, record)

			}
			if err != nil {
				return stacktrace.Propagate(err, "error processing systemd service crud action %v", crudAction)
			}
		}

		return nil
	}

	err := WithQueries(
		ctx,
		"SystemdServiceCrud",
		req,
		nsi,
		withQueries,
	)

	if err != nil {
		return nil, stacktrace.Propagate(err, "SystemdServiceCrud error")
	} else {
		return &rpc.SystemdServiceRecordCrudResponse{}, nil
	}

}

func (nsi *NefarioServiceImpl) ListSystemdServices(req *rpc.ListServicesRequest) (*rpc.ListServicesResponse, error) {

	ctx := context.Background()
	var response rpc.ListServicesResponse

	withQueries := func(queries *db.Queries) error {

		services, err := queries.ListServicesForMinion(
			ctx,
			req.MinionUid,
		)
		if err != nil {
			return stacktrace.Propagate(err, "error listing systemd services")
		}

		for _, service := range services {

			var serviceUnitJson rpc.ServiceUnitJson
			err := protojson.Unmarshal(service.SystemdUnitJson, &serviceUnitJson)
			if err != nil {
				err = stacktrace.Propagate(err, "bad json in systemdunitjson for %s", service.Uid)
				log.Error(err.Error())
			}

			record := &rpc.ServiceRecord{
				Uid:           service.Uid,
				Name:          service.Name,
				MinionUid:     service.MinionUid,
				MinionEnabled: service.MinionEnabled,
				UnitJson:      &serviceUnitJson,
			}
			response.Records = append(response.Records, record)
		}

		return nil
	}

	err := WithQueries(
		ctx,
		"ListSystemdServices",
		req,
		nsi,
		withQueries,
	)

	if err != nil {
		return nil, stacktrace.Propagate(err, "ListSystemdServices error")
	} else {
		return &response, nil
	}

}

func SubmitNefarioRpcService(
	ctx context.Context,
	mailbox base.MailboxI,
	services *NefarioServiceImpl,
) error {

	server := rpcserver.NewRpcServer(mailbox.Address())

	server.RegisterHandlers(
		rpcserver.Handler(rpc.EndPointsByPath.ListSystemdServiceRecords.Path, services.ListSystemdServices),
		rpcserver.Handler(rpc.EndPointsByPath.SystemdServiceRecordCrud.Path, services.SystemdServiceCrud),
	)

	a8.SubmitGoRoutine(
		"nefario-rpc-read-loop",
		func() error {
			return server.RunMessageReaderLoop(ctx, mailbox)
		},
	)

	return nil

}
