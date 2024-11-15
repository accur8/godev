package main

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	"accur8.io/godev/a8"
	"accur8.io/godev/nefario/checks"
	"github.com/palantir/stacktrace"
)

// global checks
// 1. wal ping
// 2. postgres replication slot
//         pg_stat_replication - postgres has an active wal listener
//         slot lag query

// per minion checks
// 1. check nefario central consumer info
// 2. rpc ping minion process
// 3. is minion process running

func main() {
	a8.RunApp(App)
}

func App(ctx context.Context) error {

	services, err := checks.InitServices(ctx)
	if err != nil {
		err = stacktrace.Propagate(err, "error initializing services")
		return err
	}

	defer func() {
		services.NatsConn.Close()
	}()

	timeoutContext, cancelFn := context.WithTimeout(ctx, 30*time.Second)
	defer cancelFn()

	results := checks.RunAllChecks(timeoutContext, services)

	for _, result := range results {
		println("=======", result.Check.Name, "=======")
		println(result.Status)
		for _, line := range result.Details {
			println("    " + line)
		}
	}

	return nil

}

// func systemdJsonParse() {

// 	jsonStr := `{"status": "SystemdServiceStatus_Stopped", "unitName": "snapd.session-agent.service", "unitExists": true, "statusSource": "systemctl", "unitDescription": "snapd user session agent"}`

// 	var serviceUnitJson rpc.ServiceUnitJson
// 	err := protojson.Unmarshal([]byte(jsonStr), &serviceUnitJson)
// 	// err := json.Unmarshal([]byte(jsonStr), serviceUnitJson)
// 	if err != nil {
// 		err = stacktrace.Propagate(err, "bad json in systemdunitjson for")
// 		println(err.Error())
// 	}
// 	println(serviceUnitJson.String())

// 	println("Hello, world!")

// }

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

func FetchServiceInfo(sshName string, serviceName string) (*ServiceInfo, error) {

	args := []string{
		sshName,
		"--",
		"systemctl",
		"--user",
		"show",
		"--no-pager",
		serviceName,
	}

	cmd := exec.Command(
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
