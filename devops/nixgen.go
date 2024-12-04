package devops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"accur8.io/godev/a8"
	"accur8.io/godev/log"
	"github.com/palantir/stacktrace"
)

type File struct {
	Path    string
	Content string
}

func NixGen(subCommandArgs *SubCommandArgs) error {

	// log.Trace("trombone titicaca tamborine")
	// log.Debug("dastardly dudes destroying detroit")
	// log.Info("in incognito ignomious intelligence")
	// log.Warn("wet watery whimpering")
	// log.Error("screaming silently")
	// os.Exit(1)

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
	{
		cc := CaddyConfig(app)
		if cc != nil {
			files = append(files, cc)
		}
	}
	{
		svc, err := SupervisorConfig(app)
		if err != nil {
			log.Error("failed to generate supervisor config for app %v -- %v", app.Name, err)
		} else if svc != nil {
			files = append(files, svc)
		}
	}
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
  encode zstd gzip
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

func SupervisorConfig(app *App) (*File, error) {

	if app.ApplicationDotHocon.Launcher.Kind == "supervisor" {

		type SupervisorConfig struct {
			AppName   string
			Command   string
			Directory string
			User      string
		}

		var templateContent string

		if app.ApplicationDotHocon.Launcher.RawConfig == "" {
			templateContent = strings.TrimLeft(`
[program:{{.AppName}}]

command = {{.Command}}

directory = {{.Directory}}

autostart       = true
autorestart     = true
startretries    = 0
startsecs       = 5
redirect_stderr = true
user            = {{.User}}
`, "\n\t ")
		} else {
			templateContent = app.ApplicationDotHocon.Launcher.RawConfig
		}

		content, err := a8.TemplatedString(
			&a8.TemplateRequest{
				Name:    "supervisor-config",
				Content: templateContent,
				Data: SupervisorConfig{
					AppName:   app.Name,
					Command:   app.ExecPath(),
					Directory: app.InstallDir(),
					User:      app.User.Name,
				},
			},
		)
		if err != nil {
			return nil, stacktrace.Propagate(err, "failed to execute supervisor template")
		}

		return &File{
			Path:    fmt.Sprintf("supervisor/%s/%s.conf", app.User.Server.Name, app.Name),
			Content: content,
		}, nil
	} else {
		return nil, nil
	}
}
