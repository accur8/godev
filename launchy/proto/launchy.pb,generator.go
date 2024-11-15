//go:generate protoc --go_out=. --go_opt=paths=source_relative launchy.proto

package launchy_proto

import (
	"accur8.io/godev/hermes/model"
	"google.golang.org/protobuf/proto"
)

var AllEndPoints model.RpcServer

var EndPointsByPath struct {
	RestartProcess       *model.RpcEndPoint
	StopProcess          *model.RpcEndPoint
	ProcessStatus        *model.RpcEndPoint
	StartProcess         *model.RpcEndPoint
	StopLaunchy          *model.RpcEndPoint
	Ping                 *model.RpcEndPoint
	ListSystemdServices  *model.RpcEndPoint
	SystemdServiceAction *model.RpcEndPoint
}

func init() {
	EndPointsByPath.RestartProcess = AllEndPoints.RegisterEndPoint(
		"RestartProcess",
		func() proto.Message { return &RestartProcessRequest{} },
		func() proto.Message { return &RestartProcessResponse{} },
	)
	EndPointsByPath.StopProcess = AllEndPoints.RegisterEndPoint(
		"StopProcess",
		func() proto.Message { return &StopProcessRequest{} },
		func() proto.Message { return &StopProcessResponse{} },
	)
	EndPointsByPath.ProcessStatus = AllEndPoints.RegisterEndPoint(
		"ProcessStatus",
		func() proto.Message { return &ProcessStatusRequest{} },
		func() proto.Message { return &ProcessStatusResponse{} },
	)
	EndPointsByPath.StartProcess = AllEndPoints.RegisterEndPoint(
		"StartProcess",
		func() proto.Message { return &StartProcessRequest{} },
		func() proto.Message { return &StartProcessResponse{} },
	)
	EndPointsByPath.StopLaunchy = AllEndPoints.RegisterEndPoint(
		"StopLaunchy",
		func() proto.Message { return &StopLaunchyRequest{} },
		func() proto.Message { return &StopLaunchyResponse{} },
	)
	EndPointsByPath.Ping = AllEndPoints.RegisterEndPoint(
		"Ping",
		func() proto.Message { return &PingRequest{} },
		func() proto.Message { return &PingResponse{} },
	)
	EndPointsByPath.ListSystemdServices = AllEndPoints.RegisterEndPoint(
		"ListSystemdServices",
		func() proto.Message { return &ListSystemdServicesRequest{} },
		func() proto.Message { return &ListSystemdServicesResponse{} },
	)
	EndPointsByPath.SystemdServiceAction = AllEndPoints.RegisterEndPoint(
		"SystemdServiceAction",
		func() proto.Message { return &SystemdServiceActionRequest{} },
		func() proto.Message { return &SystemdServiceActionResponse{} },
	)
}
