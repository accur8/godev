package devops

import (
	"encoding/json"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"accur8.io/godev/a8"
	"accur8.io/godev/log"
)

type SubCommandArgs struct {
	Config        *DevopsConfig
	RemainingArgs []string
}

type DevopsConfig struct {
	ProxmoxHostsDir     string `json:"proxmoxHostsDir"`
	ServerAppConfigsDir string `json:"serverAppConfigsDir"`
}

func LoadDefaultConfig() (*DevopsConfig, error) {
	c, e := loadDefaultConfig()
	if c != nil {
		log.Trace("loaded default config -- %v", a8.ToJson(c))
	}
	return c, e
}

func loadDefaultConfig() (*DevopsConfig, error) {

	var config DevopsConfig
	userHome, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	devopsConfigFile := filepath.Join(userHome, "devops.json")
	if a8.FileExists(devopsConfigFile) {
		jsonBytes := a8.ReadFile(devopsConfigFile)
		err := json.Unmarshal(jsonBytes, &config)
		if err != nil {
			log.Warn("Failed to unmarshal devops config file: %s", err)
		} else {
			return &config, nil
		}
	}

	proxmoxHostsDir := findDirs(userHome, "proxmox-hosts")
	serverAppConfigsDir := findDirs(userHome, "server-app-configs")

	return &DevopsConfig{
		ProxmoxHostsDir:     proxmoxHostsDir,
		ServerAppConfigsDir: serverAppConfigsDir,
	}, nil

}

func findDirs(userHome string, name string) string {
	parents := []string{"", "code", "code/accur8"}
	for _, parent := range parents {
		dir := filepath.Join(userHome, parent, name)
		if a8.DirectoryExists(dir) {
			return dir
		}
	}
	return ""
}

func FindSshKeyPublicKeys() []string {
	keys := []string{}
	usr, err := user.Current()
	if err != nil {
		return keys
	}

	find := func(keyName string) {
		sshKeyPath := filepath.Join(usr.HomeDir, ".ssh", keyName)
		if a8.FileExists(sshKeyPath) {
			contents := a8.ReadFile(sshKeyPath)
			keys = append(keys, strings.TrimSpace(string(contents)))
		}
	}

	find("id_rsa.pub")
	find("id_ed25519.pub")

	return keys
}
