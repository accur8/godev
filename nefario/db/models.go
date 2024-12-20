// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.25.0

package db

import (
	"github.com/jackc/pgx/v5/pgtype"
)

type Minion struct {
	Uid                     string
	ServerUid               string
	Login                   string
	MostRecentProcessRunUid pgtype.Text
	Description             string
	Extraconfig             []byte
	AuditVersion            pgtype.Int8
	AuditUsergroupuid       pgtype.Text
}

type ProcessRun struct {
	Uid                 string
	ProcessDefUid       pgtype.Text
	MinionUid           string
	ParentProcessRunUid pgtype.Text
	Category            pgtype.Text
	StartedAt           pgtype.Timestamp
	CompletedAt         pgtype.Timestamp
	Lastpingat          pgtype.Timestamp
	Mailbox             pgtype.Text
	ProcessStart        []byte
	LastPing            []byte
	ProcessComplete     []byte
	ExtraData           []byte
	ServiceUid          string
}

type Processconfigfile struct {
	Uid               string
	ProcessDefUid     string
	Filename          string
	Content           string
	AuditVersion      pgtype.Int8
	Extraconfig       []byte
	AuditUsergroupuid pgtype.Text
}

type Processdef struct {
	Uid               string
	Name              string
	Description       string
	ServerUid         string
	Harnessname       string
	Harnessdata       []byte
	Extraconfig       []byte
	AuditVersion      pgtype.Int8
	AuditUsergroupuid pgtype.Text
}

type Server struct {
	Uid               string
	Name              string
	Description       string
	Vpndomainname     string
	NatsUrl           string
	Extraconfig       []byte
	AuditVersion      pgtype.Int8
	AuditUsergroupuid pgtype.Text
}

type Service struct {
	Uid                     string
	Name                    string
	MinionUid               string
	MinionEnabled           bool
	Extraconfig             []byte
	SystemdUnitJson         []byte
	MostRecentProcessRunUid pgtype.Text
	AuditVersion            pgtype.Int8
	AuditUsergroupuid       pgtype.Text
}

type WalPing struct {
	Uid  string
	Data []byte
}
