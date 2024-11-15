package checks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"accur8.io/godev/a8"
	"accur8.io/godev/a8nats"
	"accur8.io/godev/log"
	"accur8.io/godev/nefario"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/palantir/stacktrace"
)

// make a simple hello world function

// global checks
// 1. wal ping
// 2. postgres replication slot
//         pg_stat_replication - postgres has an active wal listener
//         slot lag query

// per minion checks
// 1. check nefario central consumer info
// 2. rpc ping minion process
// 3. is minion process running

// minion rpc
// 1. update services

type Config struct {
	NatsCentralUrl      string         `json:"natsCentralUrl"`
	DatabaseUrl         string         `json:"databaseUrl"`
	Minions             []MinionConfig `json:"minions"`
	ReplicationSlotName string         `json:"replicationSlotName"`
}

type CheckStatus string

const (
	CheckStatus_RedLight   CheckStatus = "red"
	CheckStatus_GreenLight CheckStatus = "green"
	CheckStatus_Error      CheckStatus = "error"
)

type CheckAction func(args *RunCheckArgs) *CheckResult

type Check struct {
	Name   string
	Action CheckAction
}

type RunCheckArgs struct {
	Context  context.Context
	Check    *Check
	Services *Services
}

func (c *Check) Result(details []string, status CheckStatus) *CheckResult {
	return &CheckResult{
		Check:   c,
		Status:  status,
		Details: details,
	}
}

func (c *Check) Error(err error) *CheckResult {
	return &CheckResult{
		Check:   c,
		Status:  CheckStatus_Error,
		Details: []string{err.Error()},
	}
}

type CheckResult struct {
	Check   *Check
	Status  CheckStatus
	Details []string
}

type MinionConfig struct {
	Name    string `json:"name"`
	Url     string `json:"url"`
	SshName string `json:"sshName"`
}

// "ahs-connectria", "nats://4yXpjEBeTbXGtEjVrxvfa:o7oHnCGqLH7ri4jVMXr9@ahs-connectria.emu-dojo.ts.net:4222"
// "tulip", "nats://4yXpjEBeTbXGtEjVrxvfa:o7oHnCGqLH7ri4jVMXr9@tulip.accur8.net:4222"

type Services struct {
	Config         Config
	NatsConn       a8nats.NatsConnI
	ConnectionPool *pgxpool.Pool
	NefarioService *nefario.NefarioServiceImpl
}

func BuildChecks(services *Services) []*Check {

	checks := []*Check{
		{
			Name:   "wal ping",
			Action: runWalPing,
		},
		{
			Name:   "replication slot status",
			Action: replicationSlotStatus,
		},
	}

	for _, minion0 := range services.Config.Minions {
		minion := minion0
		check0 := &Check{
			Name:   "nefario-central " + minion.Name,
			Action: buildNefarioCentralCheck(&minion),
		}
		check1 := &Check{
			Name:   "minion-service " + minion.Name,
			Action: buildMinionServiceCheck(&minion),
		}
		checks = append(checks, check0, check1)
	}
	return checks
}

func RunAllChecks(context context.Context, services *Services) []*CheckResult {

	checks := BuildChecks(services)

	results := a8.RunPar(
		checks,
		func(check *Check) *CheckResult {
			log.Trace("starting check %s", check.Name)
			args := &RunCheckArgs{
				Context:  context,
				Check:    check,
				Services: services,
			}
			result := check.Action(args)
			log.Trace("check completed %s", check.Name)
			return result
		},
	)

	return results

}

func BuildHttpHandler(ctx context.Context) func(w http.ResponseWriter, r *http.Request) {

	initialized := false
	var services *Services
	var err error

	go func() {
		services, err = InitServices(ctx)
		initialized = true
	}()

	return func(w http.ResponseWriter, r *http.Request) {

		timeoutContext, cancelFn := context.WithTimeout(ctx, 30*time.Second)
		defer cancelFn()

		if !initialized {
			http.Error(w, "still initializing", http.StatusInternalServerError)
		} else if err != nil {
			http.Error(w, "unable to initialize services -- "+err.Error(), http.StatusInternalServerError)
		} else {
			results := RunAllChecks(timeoutContext, services)
			for _, result := range results {
				fmt.Fprintf(w, "======= %s =======\n", result.Check.Name)
				fmt.Fprintf(w, "%s\n", result.Status)
				for _, line := range result.Details {
					fmt.Fprintf(w, "    %s\n", line)
				}
			}
		}
	}
}

func buildMinionServiceCheck(minion *MinionConfig) func(args *RunCheckArgs) *CheckResult {
	return func(args *RunCheckArgs) *CheckResult {

		check := args.Check

		serviceInfo, err := FetchServiceInfo(args.Context, minion.SshName, "minion")
		if err != nil {
			return check.Error(stacktrace.Propagate(err, "unable to fetch service info"))
		}
		status := CheckStatus_RedLight
		if serviceInfo.IsRunning() {
			status = CheckStatus_GreenLight
		}
		return check.Result(
			[]string{
				fmt.Sprintf("IsRunning %v", serviceInfo.IsRunning()),
				fmt.Sprintf("Exists %v", serviceInfo.Exists()),
			},
			status,
		)
	}
}

func buildNefarioCentralCheck(minion *MinionConfig) func(args *RunCheckArgs) *CheckResult {

	return func(args *RunCheckArgs) *CheckResult {

		check := args.Check

		natsConn, err := a8nats.ResolveConn(a8nats.ConnArgs{
			NatsUrl: minion.Url,
			Name:    minion.Name,
		})
		if err != nil {
			return check.Error(stacktrace.Propagate(err, "unable to create nats connection"))
		}

		defer natsConn.Close()

		consumerInfo, err := natsConn.JetStream().ConsumerInfo("nefario-central", "nefario-central")
		if err != nil {
			return check.Error(stacktrace.Propagate(err, "unable to create nats connection"))
		}

		now := time.Now()

		lastAckFloor := now.Sub(*consumerInfo.AckFloor.Last)
		lastDelivered := now.Sub(*consumerInfo.Delivered.Last)

		log.Trace("===== %v %v", minion.Name, "=====")
		log.Trace("NumPending %v", consumerInfo.NumPending)
		log.Trace("AckFloor.Last %v", lastAckFloor.Milliseconds())
		log.Trace("Delivered.Last %v", lastDelivered.Milliseconds())

		var status CheckStatus
		if consumerInfo.NumPending > 10 {
			status = CheckStatus_RedLight
		} else {
			status = CheckStatus_GreenLight
		}

		return check.Result(
			[]string{
				fmt.Sprintf("NumPending %v", consumerInfo.NumPending),
				fmt.Sprintf("AckFloor.Last %v", lastAckFloor.Milliseconds()),
				fmt.Sprintf("Delivered.Last %v", lastDelivered.Milliseconds()),
			},
			status,
		)

	}
}

func replicationSlotStatus(args *RunCheckArgs) *CheckResult {

	check := args.Check
	services := args.Services

	row, err := services.NefarioService.FetchReplicationSlotStatus(args.Context, services.Config.ReplicationSlotName)
	if err != nil {
		log.Error("unable to retrieve replication slot status from database -- %s", err.Error())
		return check.Error(err)
	}

	oneMeg := int64(1024 * 1024)
	bytesPending0, _ := row.RetBytes.Int64Value()

	bytesPending := bytesPending0.Int64

	var active *bool
	active0, err := row.Active.BoolValue()
	var activeStr string
	if err != nil {
		active = nil
		activeStr = "nil"
	} else {
		active = &active0.Bool
		activeStr = fmt.Sprintf("%t", *active)
	}

	log.Trace("===== Replication Slot - %v %v", row.SlotName.String, " =====")
	log.Trace("Bytes Pending %v", formatBytes(bytesPending))
	log.Trace("Active %v", activeStr)

	var status CheckStatus
	if *active && (bytesPending < oneMeg) {
		status = CheckStatus_GreenLight
	} else {
		status = CheckStatus_RedLight
	}

	return check.Result(
		[]string{
			fmt.Sprintf("Bytes Pending %v", formatBytes(bytesPending)),
			fmt.Sprintf("Active %v", activeStr),
		},
		status,
	)
}

func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	if bytes < KB {
		return strconv.FormatInt(bytes, 10) + " B"
	} else if bytes < MB {
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	} else if bytes < GB {
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	} else {
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	}
}

func runWalPing(args *RunCheckArgs) *CheckResult {

	check := args.Check
	services := args.Services

	pingTimeoutCtx, cancelFn := context.WithTimeout(args.Context, 10*time.Second)
	defer func() {
		cancelFn()
	}()

	pingResult := services.NefarioService.WalPing(pingTimeoutCtx)
	if pingResult.Err != nil {
		return check.Error(pingResult.Err)
	} else if pingResult.TimeoutMsg != "" {
		return check.Result(
			[]string{
				fmt.Sprintf(pingResult.TimeoutMsg),
			},
			CheckStatus_RedLight,
		)
	} else {

		return check.Result(
			[]string{
				fmt.Sprintf("full db transaction roundtrip - %v millis", pingResult.Latency0InMillis),
				fmt.Sprintf("event roundtrip - %v millis", pingResult.Latency1InMillis),
			},
			CheckStatus_GreenLight,
		)
	}

}

type ServiceInfo struct {
	values map[string]string
}

func (s *ServiceInfo) IsRunning() bool {
	activeState := s.values["ActiveState"]
	subState := s.values["SubState"]
	return activeState == "active" && subState == "running"
}

func (s *ServiceInfo) Exists() bool {
	return s.values["LoadState"] == "loaded"
}

func FetchServiceInfo(ctx context.Context, sshName string, serviceName string) (*ServiceInfo, error) {

	args := []string{
		sshName,
		"--",
		"systemctl",
		"--user",
		"show",
		"--no-pager",
		serviceName,
	}

	log.Debug("running command -- ssh %v", strings.Join(args, " "))
	cmd := exec.CommandContext(
		ctx,
		"ssh",
		args...,
	)

	if cmd == nil {
		return nil, errors.New("unable to start ssh command")
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, stacktrace.Propagate(err, "error getting output")
	}

	log.Trace("command completed")

	kvp := parseKeyValuePairs(output)

	return &ServiceInfo{values: kvp}, nil

}

func parseKeyValuePairs(data []byte) map[string]string {
	// Create a map to hold the key-value pairs
	result := make(map[string]string)

	// Split the data into lines
	lines := bytes.Split(data, []byte("\n"))

	// Iterate over each line
	for _, line := range lines {
		// Trim any spaces or extra characters around the line
		lineStr := strings.TrimSpace(string(line))

		// Skip empty lines
		if lineStr == "" {
			continue
		}

		// Split the line by the '=' sign
		parts := strings.SplitN(lineStr, "=", 2)

		// Ensure there are exactly two parts: key and value
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])

			// Add the key-value pair to the map
			result[key] = value
		}
	}

	return result
}

func InitServices(ctx context.Context) (*Services, error) {

	config, err := a8.ConfigLookupE[Config]("nefario-checks")
	if err != nil {
		return nil, stacktrace.Propagate(err, "fatal: unable to load config")
	}

	natsConn, err := a8nats.ResolveConn(a8nats.ConnArgs{
		NatsUrl: config.NatsCentralUrl,
		Name:    "nefario",
		Context: ctx,
	})
	if err != nil {
		return nil, stacktrace.Propagate(err, "unable to create nats connection")
	}

	pool, err := pgxpool.New(ctx, config.DatabaseUrl)
	if err != nil {
		return nil, stacktrace.Propagate(err, "unable to create database connection pool")
	}

	nefarioService := &nefario.NefarioServiceImpl{
		NatsConn:       natsConn,
		ConnectionPool: pool,
	}

	return &Services{
		Config:         *config,
		NatsConn:       natsConn,
		ConnectionPool: pool,
		NefarioService: nefarioService,
	}, nil

}
