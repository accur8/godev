package rpc

import (
	"accur8.io/godev/a8"
)

func NowTimeStamp() *Timestamp {
	return &Timestamp{
		UnixEpochMillis: a8.NowUtcTimestamp().InEpochhMillis(),
	}
}

func ProcessPingReq(processPing *ProcessPing) *ProcessPingRequest {
	return &ProcessPingRequest{
		Pings:     []*ProcessPing{processPing},
		Timestamp: NowTimeStamp(),
	}
}
