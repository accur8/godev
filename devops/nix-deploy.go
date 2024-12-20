package devops

import (
	"fmt"
	"path/filepath"
	"strings"

	"accur8.io/godev/a8"
)

func NixDeploy(subCommandArgs *SubCommandArgs) error {

	devopsConfig := subCommandArgs.Config

	if devopsConfig.ProxmoxHostsDir == "" {
		return fmt.Errorf("proxmoxHostsDir must be set in ~/.a8/devops.json or findable in ~ ~/code or ~/code/accur8")
	}

	proxmoxHostsRootDir := devopsConfig.ProxmoxHostsDir

	force := false
	for _, arg := range subCommandArgs.RemainingArgs {
		if arg == "--force" {
			force = true
			break
		}
	}

	runNixDeploy := func(serverPath string) error {
		cmdArgs := []string{"./deploy"}
		if force {
			cmdArgs = append(cmdArgs, "--force")
		}

		return RunCommandX(
			&RunCommandArgs{
				Command:    cmdArgs,
				WorkingDir: serverPath,
			},
		)

	}

	for _, arg := range subCommandArgs.RemainingArgs {

		if strings.HasPrefix(arg, "--") {
			continue
		}
		serverName := arg
		serverPath := filepath.Join(proxmoxHostsRootDir, serverName)
		if !a8.DirectoryExists(serverPath) {
			return fmt.Errorf("server %v does not exist in %v", serverName, proxmoxHostsRootDir)
		}

		err := runNixDeploy(serverPath)
		if err != nil {
			return err
		}

	}

	return nil
}
