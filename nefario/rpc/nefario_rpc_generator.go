//go:generate protoc --go_out=. --go_opt=paths=source_relative nefario_rpc.proto

package rpc

import (
	"accur8.io/godev/hermes/model"
	"google.golang.org/protobuf/proto"
)

var AllEndPoints model.RpcServer

var EndPointsByPath struct {
	ListSystemdServiceRecords *model.RpcEndPoint
	SystemdServiceRecordCrud  *model.RpcEndPoint
}

func init() {
	EndPointsByPath.ListSystemdServiceRecords = AllEndPoints.RegisterEndPoint(
		"ListSystemdServiceRecords",
		func() proto.Message { return &ListServicesRequest{} },
		func() proto.Message { return &ListServicesResponse{} },
	)
	EndPointsByPath.SystemdServiceRecordCrud = AllEndPoints.RegisterEndPoint(
		"SystemdServiceRecordCrud",
		func() proto.Message { return &SystemdServiceRecordCrudRequest{} },
		func() proto.Message { return &SystemdServiceRecordCrudResponse{} },
	)
}
