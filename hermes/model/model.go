package model

import (
	"accur8.io/godev/a8"
	"google.golang.org/protobuf/proto"
)

func RandomProcessUid() ProcessUid {
	return NewProcessUid(a8.RandomUid())
}

func RandomCorrelationId() CorrelationId {
	return NewCorrelationId(a8.RandomUid())
}

func RandomIdempotentId() IdempotentId {
	return NewIdempotentId(a8.RandomUid())
}

// type ChannelName string
// type AdminKey string
// type CorrelationId string
// type IdempotentId string
// type ReaderKey string
// type MailboxAddress string
// type ProcessUid string

// func (s CorrelationId) String() string {
// 	return string(s)
// }

// func (s ProcessUid) String() string {
// 	return string(s)
// }

// func (s ChannelName) String() string {
// 	return string(s)
// }

// func (s AdminKey) String() string {
// 	return string(s)
// }

// func (s ReaderKey) String() string {
// 	return string(s)
// }

// func (s IdempotentId) String() string {
// 	return string(s)
// }

// func (s MailboxAddress) String() string {
// 	return string(s)
// }

var NefarioRpcMailbox = NewMailboxAddress("nefario-rpc")

var ConsoleChannel = NewChannelName("console")

const NefarioCentral = "nefario-central"
const NefarioPing = "nefario-ping"

type CreateMessageFn = func() proto.Message

type RpcEndPoint struct {
	Path     string
	Request  CreateMessageFn
	Response CreateMessageFn
}

type RpcServer struct {
	EndPoints []RpcEndPoint
}

func (s *RpcServer) RegisterEndPoint(path string, request CreateMessageFn, response CreateMessageFn) *RpcEndPoint {
	ep := RpcEndPoint{Path: path, Request: request, Response: response}
	s.EndPoints = append(s.EndPoints, ep)
	return &ep
}
