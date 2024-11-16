package devops

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"accur8.io/godev/a8"
	"accur8.io/godev/log"
	"github.com/gurkankaymak/hocon"
	"github.com/palantir/stacktrace"
)

/*
TODO ??? remove local and remote staging folders

TODO ??? git commit for each run
  - each app has a last-deploy-results.json file
  - stores the version deployed (and other info)
*/
type DeployInfo struct {
	DomainName string
	Version    string
	AppInfo    *AppInfo
}

type AppInfo struct {
	Name                string
	Dir                 string
	Server              string
	User                string
	StagingDir          string
	ApplicationDotHocon *ApplicationDotHocon
}

type DeployState struct {
	AppConfigsDir       string
	StagingRootDir      string
	AppDirs             []*AppInfo
	AppDirsByDomainName map[string]*AppInfo
}

type Launcher struct {
	Kind string `json:"kind"`
}

type ApplicationDotHocon struct {
	Install    *InstallDescriptor `json:"install"`
	Launcher   *Launcher          `json:"launcher"`
	ListenPort *int               `json:"listenPort"`
	DomainName string             `json:"domainName"`
}

func ParseDeployInfo(rawArg string) (*DeployInfo, error) {
	if rawArg == "" {
		return nil, fmt.Errorf("empty argument")
	}
	parts := strings.SplitN(rawArg, ":", 2)
	var deployInfo DeployInfo
	deployInfo.DomainName = parts[0]
	if len(parts) == 2 {
		deployInfo.Version = parts[1]
	}
	return &deployInfo, nil
}

func Deploy(args []string) error {

	deploys := make([]*DeployInfo, 0, len(args))

	for _, arg := range args {
		pa, err := ParseDeployInfo(arg)
		if err != nil {
			return err
		}
		deploys = append(deploys, pa)
	}

	state := &DeployState{}

	appConfigsDir, err := findAppConfigsDir()
	if err != nil {
		return err
	}

	state.AppConfigsDir = appConfigsDir
	state.StagingRootDir = filepath.Join(state.AppConfigsDir, ".staging")

	appDirs, err := loadAppDirs(appConfigsDir)
	if err != nil {
		return err
	}

	state.AppDirs = appDirs
	state.AppDirsByDomainName = make(map[string]*AppInfo)
	for _, ad := range appDirs {
		domainName := ad.ApplicationDotHocon.DomainName
		if domainName != "" {
			state.AppDirsByDomainName[domainName] = ad
		}
	}

	errors := []string{}
	for _, d := range deploys {
		appInfo, exists := state.AppDirsByDomainName[d.DomainName]
		if exists {
			d.AppInfo = appInfo
		} else {
			errors = append(errors, fmt.Sprintf("app not found: %s", d.DomainName))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors: %s", strings.Join(errors, ", "))
	}

	errors = []string{}
	for _, d := range deploys {
		err := DeployApp(state, d.AppInfo, d)
		if err != nil {
			errors = append(errors, fmt.Sprintf("error deploying app %s", d.DomainName))
		}

	}
	if len(errors) > 0 {
		return fmt.Errorf("errors: %s", strings.Join(errors, ", "))
	}

	return nil

}

func findAppConfigsDir() (string, error) {
	return "/Users/glen/code/accur8/server-app-configs", nil
}

func loadAppDirs(rootDir string) ([]*AppInfo, error) {
	rootDir, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, err
	}

	apps := []*AppInfo{}

	rootEntries, err := os.ReadDir(rootDir)
	if err != nil {
		return nil, err
	}
	for _, e0 := range rootEntries {
		if e0.IsDir() {
			server := e0.Name()
			serverDir := filepath.Join(rootDir, server)

			if !a8.FileExists(filepath.Join(serverDir, "server.hocon")) {
				continue
			}

			userEntries, err := os.ReadDir(serverDir)
			if err != nil {
				return nil, err
			}
			for _, e1 := range userEntries {
				if e1.IsDir() {
					user := e1.Name()
					userDir := filepath.Join(serverDir, user)
					appEntries, err := os.ReadDir(userDir)
					if err != nil {
						return nil, err
					}
					for _, e2 := range appEntries {
						if e2.IsDir() {
							app := e2.Name()
							appDir := filepath.Join(userDir, app)
							appInfo := &AppInfo{Name: app, Dir: appDir, Server: server, User: user}
							appDotHocon, err := loadApplicationDotHocon(appDir)
							if err != nil {
								log.Warn("error loading %v: %v", appDir, err)
								continue
							}
							appInfo.ApplicationDotHocon = appDotHocon
							apps = append(apps, appInfo)

						}
					}
				}
			}
		}
	}
	return apps, nil

}

func DeployApp(state *DeployState, appInfo *AppInfo, deployInfo *DeployInfo) error {

	stagingName := appInfo.Name + "-" + a8.FileSystemCompatibleTimestamp()

	appStagingDir := filepath.Join(state.StagingRootDir, stagingName)

	if a8.DirectoryExists(appStagingDir) {
		err := os.RemoveAll(appStagingDir)
		if err != nil {
			return err
		}
	}
	err := os.MkdirAll(appStagingDir, 0755)
	if err != nil {
		return err
	}

	appInfo.StagingDir = appStagingDir

	// copy config files into staging
	err = RunCommand("cp", "--recursive", "--archive", appInfo.Dir, state.StagingRootDir)
	if err != nil {
		return err
	}

	install := appInfo.ApplicationDotHocon.Install

	remoteAppsDir := "/home/" + appInfo.User + "/apps"

	install.Name = appInfo.Name
	install.Version = deployInfo.Version
	install.BackupDir = remoteAppsDir + ".backup"
	install.InstallDir = remoteAppsDir + "/" + appInfo.Name
	install.IncludeDefaultVmArgs = true
	install.LinkCacheDir = true
	install.LinkLogsDir = true
	install.LinkDataDir = true
	install.LinkTempDir = true

	//    - setup install-descriptor.json
	installDescFile := filepath.Join(appInfo.StagingDir, "install-descriptor.json")
	err = a8.WriteFile(installDescFile, a8.ToPrettyJsonBytes(install))
	if err != nil {
		return err
	}

	remoteStagingDir := "/home/" + appInfo.User + "/.a8-staging/" + stagingName

	//    - rsync copy staging dir to remote user
	sshName := appInfo.User + "@" + appInfo.Server

	err = RunCommand("ssh", sshName, "mkdir", "-p", remoteStagingDir)
	if err != nil {
		return err
	}

	err = RunCommand("rsync", "--recursive", "--links", "--perms", appInfo.StagingDir+"/", sshName+":"+remoteStagingDir+"/")
	if err != nil {
		return err
	}

	err = RunCommand("ssh", sshName, "a8-install", remoteStagingDir)
	if err != nil {
		return err
	}

	// err = RunCommand("ssh", sshName, "rm", "-rf", remoteStagingDir)
	// if err != nil {
	// 	return err
	// }

	return nil

}

func RunCommand(args ...string) error {
	commandStr := strings.Join(args, " ")
	log.Trace("Running command: %s", commandStr)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	err := cmd.Run()
	if err != nil {
		return stacktrace.Propagate(err, "error running command: %s", commandStr)
	}
	if cmd.ProcessState.ExitCode() != 0 {
		return fmt.Errorf("command failed with exit code %v -- %s", cmd.ProcessState.ExitCode(), commandStr)
	}
	return nil
}

func loadApplicationDotHocon(appDir string) (*ApplicationDotHocon, error) {
	filePath := filepath.Join(appDir, "application.hocon")
	if !a8.FileExists(filePath) {
		return nil, fmt.Errorf("file not found: %s", filePath)
	}
	config, err := hocon.ParseResource(filePath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "error parsing hocon file: %s", filePath)
	}
	var appDotHocon ApplicationDotHocon

	getString := func(name string) string {
		s := config.GetString(name)
		return strings.Trim(string(s), `"`)
	}

	fixstr := func(s string) string {
		return strings.Trim(s, `"`)
	}

	listenPort := config.GetInt("listenPort")
	if listenPort != 0 {
		appDotHocon.ListenPort = &listenPort
	}

	domainName := getString("domainName")
	if domainName != "" {
		appDotHocon.DomainName = domainName
	}

	launcher := config.GetConfig("launcher")
	if launcher != nil {
		appDotHocon.Launcher = &Launcher{}
		appDotHocon.Launcher.Kind = launcher.GetString("kind")
	}

	install := config.GetConfig("install")
	if install != nil {
		appDotHocon.Install = &InstallDescriptor{}
		appDotHocon.Install.Name = filepath.Base(appDir)
		appDotHocon.Install.MainClass = fixstr(install.GetString("mainClass"))
		appDotHocon.Install.Args = install.GetStringSlice("args")
		appDotHocon.Install.Artifact = fixstr(install.GetString("artifact"))
		appDotHocon.Install.Organization = fixstr(install.GetString("organization"))
		appDotHocon.Install.Version = fixstr(install.GetString("version"))
		appDotHocon.Install.Branch = fixstr(install.GetString("branch"))
		appDotHocon.Install.JavaRuntimeVersion = fixstr(install.GetString("javaVersion"))
		appDotHocon.Install.Repo = fixstr(install.GetString("repo"))

		wex := install.Get("webappExplode")
		if wex != nil {
			b := install.GetBoolean("webappExplode")
			appDotHocon.Install.WebappExplode = &b
		} else {
			b := true
			appDotHocon.Install.WebappExplode = &b
		}
	}
	log.Trace("loaded app %v: %v", appDir, appDotHocon.DomainName)
	return &appDotHocon, nil
}
