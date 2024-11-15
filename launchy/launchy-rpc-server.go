package launchy

import (
	"accur8.io/godev/hermes/directclient"
	"accur8.io/godev/hermes/hproto"
	"accur8.io/godev/hermes/rpcserver"
	launchy_proto "accur8.io/godev/launchy/proto"
	"accur8.io/godev/log"
	"accur8.io/godev/nefario/rpc"
	"github.com/palantir/stacktrace"
)

func SubmitRpcListenAndServe(lc LaunchyController) error {

	mailbox, err := directclient.CreateRpcMailbox(
		lc.LaunchyContext(),
		&directclient.RpcMailboxConfig{
			Description: "launcy rpc",
		},
	)
	if err != nil {
		panic(err)
	}

	server := rpcserver.NewRpcServer(mailbox.Address())

	server.RegisterHandlers(
		rpcserver.Handler(launchy_proto.EndPointsByPath.Ping.Path, lc.RpcPing),
		rpcserver.Handler(launchy_proto.EndPointsByPath.StartProcess.Path, lc.RpcStartProcess),
		rpcserver.Handler(launchy_proto.EndPointsByPath.StopProcess.Path, lc.RpcStopProcess),
		rpcserver.Handler(launchy_proto.EndPointsByPath.ProcessStatus.Path, lc.RpcProcessStatus),
		rpcserver.Handler(launchy_proto.EndPointsByPath.StopLaunchy.Path, lc.RpcStopLaunchy),
	)

	if log.IsTraceEnabled {
		log.Trace("Address %v", mailbox.Address())
	}

	lc.NefarioClient().UpdateMailbox(lc.LaunchyContext(), &rpc.UpdateMailboxRequest{ProcessUid: lc.ProcessUid().String(), Mailbox: mailbox.Address().String()})

	msgHander := func(msg *hproto.Message) error {
		smr := server.ProcessRpcCall(msg)
		log.Debug("sending rpc response %+v", smr)
		mailbox.PublishSendMessageRequest(smr)
		return nil
	}

	go func() {
		err := mailbox.RunRpcInboxReader(lc.LaunchyContext(), msgHander)
		if err != nil {
			err = stacktrace.Propagate(err, "mailbox.RunRpcInboxReader error")
			log.Error(err.Error())
		}
		log.Debug("RpcInboxReader goroutine ending")
	}()

	return nil

}
