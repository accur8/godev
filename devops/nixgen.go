package devops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"accur8.io/godev/a8"
	"accur8.io/godev/log"
)

type File struct {
	Path    string
	Content string
}

func NixGen(subCommandArgs *SubCommandArgs) error {

	devopsConfig := subCommandArgs.Config

	if devopsConfig.ProxmoxHostsDir == "" || devopsConfig.ServerAppConfigsDir == "" {
		return fmt.Errorf("proxmoxHostsDir and serverAppConfigsDir must be set in ~/.a8/devops.json or findable in ~ ~/code or ~/code/accur8")
	}

	root, err := loadRoot(devopsConfig.ServerAppConfigsDir)
	if err != nil {
		return err
	}
	files := []*File{}
	for _, app := range root.Apps() {
		files = append(files, GenerateContent(app)...)
	}
	nixgenRoot := filepath.Join(devopsConfig.ProxmoxHostsDir, "nixgen")
	if a8.DirectoryExists(nixgenRoot) {
		log.Trace("clearing nixgen root %v", nixgenRoot)
		err := os.RemoveAll(nixgenRoot)
		if err != nil {
			return err
		}
	}

	for _, file := range files {
		path := filepath.Join(nixgenRoot, file.Path)
		dir := filepath.Dir(path)
		if !a8.DirectoryExists(dir) {
			err := os.MkdirAll(dir, 0755)
			if err != nil {
				return err
			}
		}
		log.Trace("nixgen writing file %v", path)
		err := a8.WriteFile(path, []byte(file.Content))
		if err != nil {
			return err
		}
	}
	return nil
}

func GenerateContent(app *App) []*File {
	files := []*File{}
	if app.ApplicationDotHocon.ListenPort != nil {
		files = append(files, CaddyConfig(app))
	}

	if app.ApplicationDotHocon.CleanUp != nil && app.ApplicationDotHocon.CleanUp.Kind == "systemd" {
		files = append(files, CleanUpSystemdServiceConfig(app))
		files = append(files, CleanUpSystemdTimerConfig(app))
	}
	files = append(files, SupervisorConfig(app))
	return files
}

func CaddyConfig(app *App) *File {
	var content string
	if app.ApplicationDotHocon.CaddyConfig == "" {
		if app.ApplicationDotHocon.ListenPort == nil {
			return nil
		}
		virtualHostList := strings.Join(app.ApplicationDotHocon.DomainNames, " ")
		listenPort := *app.ApplicationDotHocon.ListenPort
		content = strings.TrimLeft(fmt.Sprintf(`
%v {
  encode gzip
  reverse_proxy %v:%v
}		
		`, virtualHostList, app.User.Server.CaddyName(), listenPort), "\n ")
	} else {
		content = app.ApplicationDotHocon.CaddyConfig
	}
	return &File{
		Path:    fmt.Sprintf("caddy/%s/%s.caddy", app.User.Server.Name, app.Name),
		Content: content,
	}
}

func SupervisorConfig(app *App) *File {

	content := strings.TrimLeft(fmt.Sprintf(`
[program:%v]

command = %v

directory = %v

autostart       = true
autorestart     = true
startretries    = 0
startsecs       = 5
redirect_stderr = true
user            = %v

	`, app.Name, app.ExecPath(), app.InstallDir(), app.User.Name), "\n ")

	return &File{
		Path:    fmt.Sprintf("supervisor/%s/%s.conf", app.User.Server.Name, app.Name),
		Content: content,
	}
}

func CleanUpSystemdServiceConfig(app *App) *File {

	content := strings.TrimLeft(fmt.Sprintf(`
[Unit]
Description=Executes daily-cleanup-%v script

[Service]
ExecStart=/run/current-system/sw/bin/daily-cleanup %v/application.hocon
	`, app.Name, app.InstallDir()), "\n ")

	return &File{
		Path:    fmt.Sprintf("systemd/%s/daily-cleanup-%s.service", app.User.Server.Name, app.Name),
		Content: content,
	}
}

func CleanUpSystemdTimerConfig(app *App) *File {

	content := strings.TrimLeft(fmt.Sprintf(`
[Unit]
Description=Timer for daily-cleanup-%v service

[Timer]
OnCalendar=%v
Unit=daily-cleanup-%v.service

	`, app.Name, app.ApplicationDotHocon.CleanUp.Timer, app.Name), "\n ")

	return &File{
		Path:    fmt.Sprintf("systemd/%s/daily-cleanup-%s.timer", app.User.Server.Name, app.Name),
		Content: content,
	}
}
