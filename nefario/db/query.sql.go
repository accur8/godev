// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.25.0
// source: query.sql

package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

const deletePing = `-- name: DeletePing :exec
DELETE 
  FROM wal_ping 
  WHERE Uid = $1
`

func (q *Queries) DeletePing(ctx context.Context, uid string) error {
	_, err := q.db.Exec(ctx, deletePing, uid)
	return err
}

const deleteService = `-- name: DeleteService :exec
DELETE FROM service 
WHERE 
  uid = $1
`

func (q *Queries) DeleteService(ctx context.Context, uid string) error {
	_, err := q.db.Exec(ctx, deleteService, uid)
	return err
}

const fetchReplicationSlotStatus = `-- name: FetchReplicationSlotStatus :one
SELECT 
  slot_name, 
  database, 
  active, 
  pg_wal_lsn_diff(pg_current_wal_insert_lsn(), restart_lsn) AS ret_bytes
FROM 
  pg_replication_slots
WHERE
  slot_name = $1
`

type FetchReplicationSlotStatusRow struct {
	SlotName pgtype.Text
	Database pgtype.Text
	Active   pgtype.Bool
	RetBytes pgtype.Numeric
}

func (q *Queries) FetchReplicationSlotStatus(ctx context.Context, slotname pgtype.Text) (FetchReplicationSlotStatusRow, error) {
	row := q.db.QueryRow(ctx, fetchReplicationSlotStatus, slotname)
	var i FetchReplicationSlotStatusRow
	err := row.Scan(
		&i.SlotName,
		&i.Database,
		&i.Active,
		&i.RetBytes,
	)
	return i, err
}

const getProcessRun = `-- name: GetProcessRun :one
SELECT uid, processdefuid, minionuid, parentprocessrunuid, category, startedat, completedat, lastpingat, mailbox, processstart, lastping, processcomplete, extradata, serviceuid FROM processrun
WHERE uid = $1 LIMIT 1
`

func (q *Queries) GetProcessRun(ctx context.Context, uid string) (ProcessRun, error) {
	row := q.db.QueryRow(ctx, getProcessRun, uid)
	var i ProcessRun
	err := row.Scan(
		&i.Uid,
		&i.ProcessDefUid,
		&i.MinionUid,
		&i.ParentProcessRunUid,
		&i.Category,
		&i.StartedAt,
		&i.CompletedAt,
		&i.Lastpingat,
		&i.Mailbox,
		&i.ProcessStart,
		&i.LastPing,
		&i.ProcessComplete,
		&i.ExtraData,
		&i.ServiceUid,
	)
	return i, err
}

const insertPing = `-- name: InsertPing :exec
INSERT INTO wal_ping (
  Uid,  
  Data
) VALUES (
  $1, $2
)
`

type InsertPingParams struct {
	Uid  string
	Data []byte
}

func (q *Queries) InsertPing(ctx context.Context, arg InsertPingParams) error {
	_, err := q.db.Exec(ctx, insertPing, arg.Uid, arg.Data)
	return err
}

const insertService = `-- name: InsertService :exec
INSERT INTO service (
	uid,
	name,
	minionuid,
  systemdUnitJson,
	minionenabled
) VALUES (
  $1, $2, $3, $4, $5
)
RETURNING uid, name, minionuid, minionenabled, extraconfig, systemdunitjson, mostrecentprocessrunuid, audit_version, audit_usergroupuid
`

type InsertServiceParams struct {
	Uid             string
	Name            string
	MinionUid       string
	SystemdUnitJson []byte
	MinionEnabled   bool
}

func (q *Queries) InsertService(ctx context.Context, arg InsertServiceParams) error {
	_, err := q.db.Exec(ctx, insertService,
		arg.Uid,
		arg.Name,
		arg.MinionUid,
		arg.SystemdUnitJson,
		arg.MinionEnabled,
	)
	return err
}

const insertStartDto = `-- name: InsertStartDto :exec

INSERT INTO processrun (
  Uid,  
  parentprocessrunuid,
  MinionUid,
  Category,
  ProcessStart,
  StartedAt,
  LastPingAt,
  ExtraData,
  ServiceUid
) VALUES (
  $1, $2, $3, $4, $5, current_timestamp, current_timestamp, $6, $7
)
RETURNING uid, processdefuid, minionuid, parentprocessrunuid, category, startedat, completedat, lastpingat, mailbox, processstart, lastping, processcomplete, extradata, serviceuid
`

type InsertStartDtoParams struct {
	Uid                 string
	ParentProcessRunUid pgtype.Text
	MinionUid           string
	Category            pgtype.Text
	ProcessStart        []byte
	ExtraData           []byte
	ServiceUid          string
}

// -- name: ListProcessRuns :many
// SELECT * FROM processrun
// ORDER BY startedat;
func (q *Queries) InsertStartDto(ctx context.Context, arg InsertStartDtoParams) error {
	_, err := q.db.Exec(ctx, insertStartDto,
		arg.Uid,
		arg.ParentProcessRunUid,
		arg.MinionUid,
		arg.Category,
		arg.ProcessStart,
		arg.ExtraData,
		arg.ServiceUid,
	)
	return err
}

const listServers = `-- name: ListServers :many

SELECT uid, name, description, vpndomainname, natsurl, extraconfig, audit_version, audit_usergroupuid FROM server
`

// -- name: DeleteAuthor :exec
// DELETE FROM authors
// WHERE id = $1;
func (q *Queries) ListServers(ctx context.Context) ([]Server, error) {
	rows, err := q.db.Query(ctx, listServers)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Server
	for rows.Next() {
		var i Server
		if err := rows.Scan(
			&i.Uid,
			&i.Name,
			&i.Description,
			&i.Vpndomainname,
			&i.NatsUrl,
			&i.Extraconfig,
			&i.AuditVersion,
			&i.AuditUsergroupuid,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const listServicesForMinion = `-- name: ListServicesForMinion :many
SELECT
  uid, name, minionuid, minionenabled, extraconfig, systemdunitjson, mostrecentprocessrunuid, audit_version, audit_usergroupuid
FROM service 
WHERE 
  minionUid = $1
`

func (q *Queries) ListServicesForMinion(ctx context.Context, minionuid string) ([]Service, error) {
	rows, err := q.db.Query(ctx, listServicesForMinion, minionuid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Service
	for rows.Next() {
		var i Service
		if err := rows.Scan(
			&i.Uid,
			&i.Name,
			&i.MinionUid,
			&i.MinionEnabled,
			&i.Extraconfig,
			&i.SystemdUnitJson,
			&i.MostRecentProcessRunUid,
			&i.AuditVersion,
			&i.AuditUsergroupuid,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const updateLastPing = `-- name: UpdateLastPing :exec
UPDATE processrun
  set 
    LastPingAt = current_timestamp,
    LastPing = $2
WHERE Uid = $1
`

type UpdateLastPingParams struct {
	Uid      string
	LastPing []byte
}

func (q *Queries) UpdateLastPing(ctx context.Context, arg UpdateLastPingParams) error {
	_, err := q.db.Exec(ctx, updateLastPing, arg.Uid, arg.LastPing)
	return err
}

const updateMailbox = `-- name: UpdateMailbox :exec
UPDATE processrun
  set 
    Mailbox = $2
WHERE Uid = $1
`

type UpdateMailboxParams struct {
	Uid     string
	Mailbox pgtype.Text
}

func (q *Queries) UpdateMailbox(ctx context.Context, arg UpdateMailboxParams) error {
	_, err := q.db.Exec(ctx, updateMailbox, arg.Uid, arg.Mailbox)
	return err
}

const updatePing = `-- name: UpdatePing :exec
UPDATE
  wal_ping 
  SET Data = $2
  WHERE Uid = $1
`

type UpdatePingParams struct {
	Uid  string
	Data []byte
}

func (q *Queries) UpdatePing(ctx context.Context, arg UpdatePingParams) error {
	_, err := q.db.Exec(ctx, updatePing, arg.Uid, arg.Data)
	return err
}

const updateProcessComplete = `-- name: UpdateProcessComplete :exec
UPDATE processrun
  set 
    CompletedAt = current_timestamp,
    ProcessComplete = $2
WHERE Uid = $1
`

type UpdateProcessCompleteParams struct {
	Uid             string
	ProcessComplete []byte
}

func (q *Queries) UpdateProcessComplete(ctx context.Context, arg UpdateProcessCompleteParams) error {
	_, err := q.db.Exec(ctx, updateProcessComplete, arg.Uid, arg.ProcessComplete)
	return err
}

const updateService = `-- name: UpdateService :exec
UPDATE service 
SET 
  systemdUnitJson = $2
WHERE 
  uid = $1
`

type UpdateServiceParams struct {
	Uid             string
	SystemdUnitJson []byte
}

func (q *Queries) UpdateService(ctx context.Context, arg UpdateServiceParams) error {
	_, err := q.db.Exec(ctx, updateService, arg.Uid, arg.SystemdUnitJson)
	return err
}

const updateServiceWithRunningProcess = `-- name: UpdateServiceWithRunningProcess :exec
UPDATE service 
SET 
  mostRecentProcessRunUid = $2
WHERE 
  uid = $1
`

type UpdateServiceWithRunningProcessParams struct {
	Uid                     string
	MostRecentProcessRunUid pgtype.Text
}

func (q *Queries) UpdateServiceWithRunningProcess(ctx context.Context, arg UpdateServiceWithRunningProcessParams) error {
	_, err := q.db.Exec(ctx, updateServiceWithRunningProcess, arg.Uid, arg.MostRecentProcessRunUid)
	return err
}
