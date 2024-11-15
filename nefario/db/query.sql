-- name: GetProcessRun :one
SELECT * FROM processrun
WHERE uid = $1 LIMIT 1
;

-- -- name: ListProcessRuns :many
-- SELECT * FROM processrun
-- ORDER BY startedat;

-- name: InsertStartDto :exec
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
RETURNING *
;

-- name: UpdateLastPing :exec
UPDATE processrun
  set 
    LastPingAt = current_timestamp,
    LastPing = $2
WHERE Uid = $1
;

-- name: UpdateProcessComplete :exec
UPDATE processrun
  set 
    CompletedAt = current_timestamp,
    ProcessComplete = $2
WHERE Uid = $1
;

-- name: UpdateMailbox :exec
UPDATE processrun
  set 
    Mailbox = $2
WHERE Uid = $1
;

-- -- name: DeleteAuthor :exec
-- DELETE FROM authors
-- WHERE id = $1;

-- name: ListServers :many
SELECT * FROM server
;

-- name: InsertService :exec
INSERT INTO service (
	uid,
	name,
	minionuid,
  systemdUnitJson,
	minionenabled
) VALUES (
  $1, $2, $3, $4, $5
)
RETURNING *
;


-- name: UpdateService :exec
UPDATE service 
SET 
  systemdUnitJson = $2
WHERE 
  uid = $1
;

-- name: UpdateServiceWithRunningProcess :exec
UPDATE service 
SET 
  mostRecentProcessRunUid = $2
WHERE 
  uid = $1
;

-- name: DeleteService :exec
DELETE FROM service 
WHERE 
  uid = $1  
;

-- name: ListServicesForMinion :many
SELECT
  *
FROM service 
WHERE 
  minionUid = $1 
;

-- name: InsertPing :exec
INSERT INTO wal_ping (
  Uid,  
  Data
) VALUES (
  $1, $2
)
;

-- name: UpdatePing :exec
UPDATE
  wal_ping 
  SET Data = $2
  WHERE Uid = $1
;

-- name: DeletePing :exec
DELETE 
  FROM wal_ping 
  WHERE Uid = $1
;


-- name: FetchReplicationSlotStatus :one
SELECT 
  slot_name, 
  database, 
  active, 
  pg_wal_lsn_diff(pg_current_wal_insert_lsn(), restart_lsn) AS ret_bytes
FROM 
  pg_replication_slots
WHERE
  slot_name = sqlc.arg(slotName)
;