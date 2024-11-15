package launchy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"accur8.io/godev/a8"
	launchy_proto "accur8.io/godev/launchy/proto"
	"accur8.io/godev/log"
	"github.com/kardianos/osext"
	"github.com/palantir/stacktrace"
	"github.com/spf13/cobra"
)

func parseServicesStatus(services *launchy_proto.ListSystemdServicesResponse, serviceName string) ([]byte, error) {
	if serviceName == "" {
		servicesJson, err := json.Marshal(services)
		if err != nil {
			return nil, stacktrace.Propagate(err, "error marshalling systemctl services status to json")
		}
		return servicesJson, nil
	}
	for _, service := range services.Services {
		if service.Unit == serviceName {
			singleServiceJson, err := json.Marshal(service)
			if err != nil {
				return nil, stacktrace.Propagate(err, "error marshalling single systemctl service status")
			}
			return singleServiceJson, nil
		}
	}
	return nil, nil
}

func scrubSystemCtlOutput(output []byte) string {
	lines := strings.Split(string(output), "\n")
	var cleanedLines []string
	for _, line := range lines {
		if len(line) >= 1 {
			runes := []rune(line)
			firstRune := runes[0]
			if firstRune == '●' || firstRune == '*' {
				line = string(runes[1:])
			}
		}
		cleanedLines = append(cleanedLines, strings.TrimSpace(line))
	}
	return strings.Join(cleanedLines, "\n")

}

func InstallAgent(cmd *cobra.Command, args []string, globalFlags *CmdGlobalFlagsS) {

	serviceName := "minion.service"

	execPath, err := osext.Executable()
	if err != nil {
		log.Error("error getting executable path %v", err)
		return
	}
	execDir := filepath.Dir(execPath)

	content := fmt.Sprintf(`
[Unit]
Description=minion agent runner

[Service]
Type=simple
WorkingDirectory=%s
StandardOutput=journal
ExecStart=%s --trace agent

[Install]
WantedBy=default.target
`, execDir, execPath)

	content = strings.TrimLeft(content, "\n")

	userHome, err := os.UserHomeDir()
	if err != nil {
		panic(stacktrace.Propagate(err, "error getting user home dir"))
	}

	systemdUserDir := filepath.Join(userHome, ".config/systemd/user")

	exists, err := a8.DirExists(systemdUserDir)
	if err != nil {
		panic(stacktrace.Propagate(err, "error checking for systemd user dir %s", systemdUserDir))
	}
	if !exists {
		err = os.RemoveAll(systemdUserDir)
		if err != nil {
			panic(stacktrace.Propagate(err, "error removing systemd user dir %s", systemdUserDir))
		}
	}
	err = os.MkdirAll(systemdUserDir, 0755)
	if err != nil {
		panic(stacktrace.Propagate(err, "error creating systemd user dir %s", systemdUserDir))
	}

	unitFile := filepath.Join(systemdUserDir, serviceName)

	err = os.WriteFile(unitFile, []byte(content), 0644)
	if err != nil {
		panic(stacktrace.Propagate(err, "error writing unit file %s", unitFile))
	}

	output, err := Command("systemctl", "--user", "daemon-reload").CombinedOutput()
	if err != nil {
		panic(stacktrace.Propagate(err, "error running systemctl daemon-reload"))
	}
	if log.IsTraceEnabled {
		log.Debug("daemon-reload output %v", string(output))
	}

	_, err = Command("systemctl", "--user", "enable", serviceName).CombinedOutput()
	if err != nil {
		panic(stacktrace.Propagate(err, "error running systemctl enable"))
	}
	if log.IsTraceEnabled {
		log.Debug("systemctl enable output %v", string(output))
	}

	_, err = Command("systemctl", "--user", "start", serviceName).CombinedOutput()
	if err != nil {
		panic(stacktrace.Propagate(err, "error running systemctl start"))
	}
	if log.IsTraceEnabled {
		log.Debug("systemctl start output %v", string(output))
	}

}
