package launchy

import (
	"accur8.io/godev/a8"
	"accur8.io/godev/nefario/rpc"
)

type RunResult struct {
	ExitCode    int
	ExitMessage string
}

type LaunchArgs struct {
	// aka ServerUid
	TraceLogging    bool
	Logging         bool
	NoExit          bool
	ConsolePassThru bool
	Category        string
	NoStart         bool
	Command         []string
	MinionUid       string
	ExtraData       string
}

func NowTimeStamp() *rpc.Timestamp {
	return &rpc.Timestamp{UnixEpochMillis: a8.NowUtcTimestamp().InEpochhMillis()}
}
