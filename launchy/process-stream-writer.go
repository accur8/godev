package launchy

import (
	"os"

	"accur8.io/godev/a8"
	"accur8.io/godev/hermes/api"
	"accur8.io/godev/hermes/model"
	"accur8.io/godev/log"
	"accur8.io/godev/nefario/rpc"
)

var ConsoleChannel = model.NewChannelName("console")

type ProcessStreamWriter struct {
	ProcessUid        a8.Uid
	Name              string
	LaunchyController LaunchyController
	PassThru          *os.File
	ChannelName       api.ChannelName
	Transport         rpc.Transport
	SequenceCounter   uint64
	ByteCount         uint64
}

func (pw *ProcessStreamWriter) PostLogSlice(data []byte) error {
	pw.SequenceCounter += 1
	pw.ByteCount += uint64(len(data))
	now := NowTimeStamp()
	return pw.LaunchyController.NefarioClient().StreamWrite(
		pw.LaunchyController.LaunchyContext(),
		&rpc.StreamWrite{
			ProcessUid: pw.ProcessUid,
			Record: &rpc.StreamRecord{
				Timestamp: now,
				Sequence:  pw.SequenceCounter,
				Buffers: []*rpc.Buffer{
					{
						Timestamp: now,
						Data:      data,
						Transport: pw.Transport,
					},
				},
			},
			ChannelName: pw.ChannelName.String(),
		},
	)
}

func (pw *ProcessStreamWriter) Write(data []byte) (n int, err error) {
	// n, err = pw.ringbuffer.Write(data)
	// if err != nil {
	// 	log.Error("error writing ring buffer " + pw.name + " " + err.Error())
	// }
	err = pw.PostLogSlice(data)
	if err != nil {
		log.Error("error posting log slice for %s - %v", pw.Name, err)
	}
	if pw.PassThru != nil {
		return pw.PassThru.Write(data)
	} else {
		return len(data), nil
	}
}
