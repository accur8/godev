package devops

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"accur8.io/godev/a8"
	"accur8.io/godev/log"
	"github.com/go-akka/configuration"
	"github.com/palantir/stacktrace"
	"github.com/plus3it/gorecurcopy"
)

type DeployInfo struct {
	DomainName       string
	Version          string
	App              *App
	LocalStagingDir  string
	RemoteStagingDir string
}

type LastDeploy struct {
	Version   string   `json:"Version"`
	Timestamp string   `json:"Timestamp"`
	SshKeys   []string `json:"SshKeys"`
}

type App struct {
	Name                string
	Dir                 string
	User                *User
	ApplicationDotHocon *ApplicationDotHocon
	LastDeploy          *LastDeploy
}

type DeployState struct {
	Root *Root
}

type Launcher struct {
	Kind      string `json:"kind"`
	Timer     *Timer `json:"timer"`
	RawConfig string `json:"rawConfig"`
}

type Timer struct {
	OnCalendar        string `json:"onCalendar"`
	OnBootSec         string `json:"onBootSec"`
	OnUnitActiveSec   string `json:"onUnitActiveSec"`
	OnUnitInactiveSec string `json:"onUnitInactiveSec"`
}

type Root struct {
	Dir        string
	Servers    []*Server
	StagingDir string
}

type Server struct {
	Name        string
	VpnName     string
	Users       []*User
	Dir         string
	Root        *Root
	NameInCaddy string
}

type User struct {
	Name   string
	Apps   []*App
	Dir    string
	Server *Server
}

type ApplicationDotHocon struct {
	Install     *InstallDescriptor `json:"install"`
	Launcher    *Launcher          `json:"launcher"`
	CleanUp     *CleanUp           `json:"cleanUp"`
	ListenPort  *int               `json:"listenPort"`
	DomainNames []string           `json:"domainNames"`
	CaddyConfig string             `json:"caddyConfig"`
}

type CleanUp struct {
	Kind    string         `json:"kind"`
	Restart bool           `json:"restart"`
	Timer   string         `json:"timer"`
	Tasks   []*CleanUpTask `json:"tasks"`
}

type CleanUpTask struct {
	Type string            `json:"type"`
	Args map[string]string `json:"args"`
}

func ParseDeployInfo(rawArg string) (*DeployInfo, error) {
	if rawArg == "" {
		return nil, fmt.Errorf("empty argument")
	}
	parts := strings.SplitN(rawArg, ":", 2)
	var deployInfo DeployInfo
	deployInfo.DomainName = parts[0]

	if len(parts) == 1 || parts[1] == "" {
		deployInfo.Version = "current"
	} else {
		deployInfo.Version = parts[1]
	}

	return &deployInfo, nil
}

func Deploy(subCommandArgs *SubCommandArgs) error {

	args := subCommandArgs.RemainingArgs

	deploys := make([]*DeployInfo, 0, len(args))

	for _, arg := range args {
		pa, err := ParseDeployInfo(arg)
		if err != nil {
			return err
		}
		deploys = append(deploys, pa)
	}

	state := &DeployState{}

	root, err := loadRoot(subCommandArgs.Config.ServerAppConfigsDir)
	if err != nil {
		return err
	}
	state.Root = root

	errors := []string{}
	for _, d := range deploys {
		app := root.FindApp(d.DomainName)
		if app == nil {
			errors = append(errors, fmt.Sprintf("app not found: %s", d.DomainName))
		} else {
			d.App = app
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors: %s", strings.Join(errors, ", "))
	}

	errors = []string{}
	for _, d := range deploys {
		err := DeployApp(state, d)
		if err != nil {
			errors = append(errors, fmt.Sprintf("error deploying app %s: %s", d.DomainName, err.Error()))
		}

	}
	if len(errors) > 0 {
		return fmt.Errorf("errors: %s", strings.Join(errors, ", "))
	}

	log.Trace("committing successful deploy to server-app-configs git repo")
	commitMsg := "deployed " + strings.Join(subCommandArgs.RemainingArgs, " ")
	err = RunCommand("git", "-C", subCommandArgs.Config.ServerAppConfigsDir, "commit", "-am", commitMsg)
	if err != nil {
		return err
	}

	return nil

}

func isCurrentVersionLabel(version string) bool {
	var currentVersionLabels = []string{"current", ""}

	for _, label := range currentVersionLabels {
		if version == label {
			return true
		}
	}
	return false
}

func DeployApp(state *DeployState, deployInfo *DeployInfo) error {

	user := deployInfo.App.User

	stagingName := deployInfo.App.Name + "-" + a8.FileSystemCompatibleTimestamp()

	localStagingDir := filepath.Join(state.Root.StagingDir, stagingName)

	if a8.DirectoryExists(localStagingDir) {
		err := os.RemoveAll(localStagingDir)
		if err != nil {
			return err
		}
	}
	err := os.MkdirAll(localStagingDir, 0755)
	if err != nil {
		return err
	}

	deployInfo.LocalStagingDir = localStagingDir

	err = gorecurcopy.CopyDirectory(deployInfo.App.Dir, localStagingDir)
	if err != nil {
		return err
	}

	stagedLastDeployFile := filepath.Join(localStagingDir, "last-deploy.json")
	if a8.FileExists(stagedLastDeployFile) {
		os.Remove(stagedLastDeployFile)
	}

	appInfo := deployInfo.App
	install := appInfo.ApplicationDotHocon.Install

	deployInfo.RemoteStagingDir = filepath.Join(appInfo.User.HomeDir(), "apps")

	// Settle explicit version
	if isCurrentVersionLabel(deployInfo.Version) {
		if appInfo.LastDeploy == nil {
			err = fmt.Errorf("last-deploy.json needed in order to fetch current version")
			return err
		}

		deployInfo.Version = appInfo.LastDeploy.Version
	}

	install.Name = appInfo.Name
	install.Version = deployInfo.Version
	install.BackupDir = user.BackupsDir()
	install.InstallDir = filepath.Join(user.AppsDir(), appInfo.Name)
	install.IncludeDefaultVmArgs = true
	install.LinkCacheDir = true
	install.LinkLogsDir = true
	install.LinkDataDir = true
	install.LinkTempDir = true

	//    - setup install-descriptor.json
	installDescFile := filepath.Join(deployInfo.LocalStagingDir, "install-descriptor.json")
	err = a8.WriteFile(installDescFile, a8.ToPrettyJsonBytes(install))
	if err != nil {
		return err
	}

	deployInfo.RemoteStagingDir = filepath.Join(appInfo.User.HomeDir(), ".a8-staging", stagingName)

	//    - rsync copy staging dir to remote user
	sshName := appInfo.User.SshName()

	err = RunCommand("ssh", sshName, "--", "mkdir", "-p", deployInfo.RemoteStagingDir)
	if err != nil {
		return err
	}

	err = RunCommand("rsync", "--recursive", "--links", "--perms", deployInfo.LocalStagingDir+"/", sshName+":"+deployInfo.RemoteStagingDir+"/")
	if err != nil {
		return err
	}

	args := []string{"ssh", sshName, "--", "a8"}
	if log.IsTraceEnabled {
		args = append(args, "--trace")
	}
	args = append(args, "local-install", deployInfo.RemoteStagingDir)

	err = RunCommand(args...)
	if err != nil {
		return err
	}

	log.Trace("sucessfully deployed %v", deployInfo.DomainName)

	lastDeploy := &LastDeploy{
		Version:   deployInfo.Version,
		Timestamp: time.Now().UTC().String(),
		SshKeys:   FindSshKeyPublicKeys(),
	}
	lastDeployFile := filepath.Join(deployInfo.App.Dir, "last-deploy.json")
	err = a8.WriteFile(lastDeployFile, a8.ToPrettyJsonBytes(lastDeploy))
	if err != nil {
		log.Warn("error writing last-deploy.json file %v: %v", lastDeployFile, err)
	} else {
		log.Trace("wrote last-deploy.json file %v", lastDeployFile)
	}

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
	log.Trace("command completed with exit code %v -- %s", cmd.ProcessState.ExitCode(), commandStr)
	if cmd.ProcessState.ExitCode() != 0 {
		return fmt.Errorf("command failed with exit code %v -- %s", cmd.ProcessState.ExitCode(), commandStr)
	}
	return nil
}

func LoadLastDeployJson(appDir string) (*LastDeploy, error) {
	filePath := filepath.Join(appDir, "last-deploy.json")

	if !a8.FileExists(filePath) {
		return nil, nil
	}

	var lastDeploy LastDeploy

	jsonBytes := a8.ReadFile(filePath)
	err := json.Unmarshal(jsonBytes, &lastDeploy)

	if err != nil {
		err = fmt.Errorf("error loading last-deploy.json file: %s", filePath)
		return nil, err
	}

	return &lastDeploy, err
}

func loadApplicationDotHocon(appDir string) (*ApplicationDotHocon, error) {
	filePath := filepath.Join(appDir, "application.hocon")
	config, err := loadHoconFile(filePath)

	if err != nil {
		return nil, stacktrace.Propagate(err, "error parsing hocon file: %s", filePath)
	}
	var appDotHocon ApplicationDotHocon

	listenPort := config.GetInt32("listenPort")
	if listenPort != 0 {
		t := int(listenPort)
		appDotHocon.ListenPort = &t
	}

	domainNames := []string{}
	{
		dn := config.GetString("domainName")
		if dn != "" {
			domainNames = append(domainNames, dn)
		}
		dnames := config.GetStringList("domainNames")
		domainNames = append(domainNames, dnames...)
	}

	appDotHocon.DomainNames = domainNames
	appDotHocon.CaddyConfig = config.GetString("caddyConfig")

	launcher := config.GetConfig("launcher")
	if launcher != nil {
		appDotHocon.Launcher = &Launcher{}
		appDotHocon.Launcher.Kind = launcher.GetString("kind")
		appDotHocon.Launcher.RawConfig = launcher.GetString("rawConfig")

		timer := launcher.GetConfig("timer")
		if timer != nil {
			appDotHocon.Launcher.Timer = &Timer{
				OnBootSec:         timer.GetString("onBootSec"),
				OnCalendar:        timer.GetString("onCalendar"),
				OnUnitActiveSec:   timer.GetString("onUnitActiveSec"),
				OnUnitInactiveSec: timer.GetString("onUnitInactiveSec"),
			}
		}

	}

	install := config.GetConfig("install")
	if install != nil {
		appDotHocon.Install = &InstallDescriptor{}
		appDotHocon.Install.Name = filepath.Base(appDir)
		appDotHocon.Install.MainClass = install.GetString("mainClass")
		appDotHocon.Install.Args = install.GetStringList("args")
		appDotHocon.Install.JvmArgs = install.GetStringList("jvmArgs")
		appDotHocon.Install.Artifact = install.GetString("artifact")
		appDotHocon.Install.Organization = install.GetString("organization")
		appDotHocon.Install.Version = install.GetString("version")
		appDotHocon.Install.Branch = install.GetString("branch")
		appDotHocon.Install.JavaRuntimeVersion = install.GetString("javaVersion")
		appDotHocon.Install.Repo = install.GetString("repo")

		wex := install.GetNode("webappExplode")
		if wex != nil {
			b := install.GetBoolean("webappExplode")
			appDotHocon.Install.WebappExplode = &b
		} else {
			b := true
			appDotHocon.Install.WebappExplode = &b
		}
	}

	cleanUp := config.GetConfig("cleanUp")

	if cleanUp != nil {
		appDotHocon.CleanUp = &CleanUp{}
		appDotHocon.CleanUp.Kind = cleanUp.GetString("kind")
		appDotHocon.CleanUp.Timer = cleanUp.GetString("timer")
		appDotHocon.CleanUp.Restart = cleanUp.GetBoolean("restart")
		// The rest of CleanUp Config is not needed yet in nixgen, to be added later
	}

	log.Trace("loaded app %v: %v", appDir, appDotHocon.DomainName())
	return &appDotHocon, nil

}

func loadHoconFile(filePath string) (config *configuration.Config, err error) {
	defer func() {
		// recover from panic if one occurred. Set err to nil otherwise.
		r := recover()
		if r != nil {
			err = fmt.Errorf("error parsing hocon %v -- %v", filePath, r)
		}
	}()
	if !a8.FileExists(filePath) {
		err = fmt.Errorf("file not found: %s", filePath)
		return
	}
	config = configuration.LoadConfig(filePath)
	if config == nil {
		err = fmt.Errorf("error loading hocon file: %s", filePath)
		return
	}
	return
}

func loadRoot(rootDir string) (*Root, error) {
	if !a8.DirectoryExists(rootDir) {
		return nil, nil
	}
	root := &Root{
		Dir:        rootDir,
		Servers:    []*Server{},
		StagingDir: filepath.Join(rootDir, ".staging"),
	}

	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		serverDir := filepath.Join(rootDir, e.Name())
		server, err := loadServer(serverDir, root)
		if err != nil {
			log.Warn("error loading server %v: %v", serverDir, err)
			continue
		}
		if server != nil {
			root.Servers = append(root.Servers, server)
		}
	}

	return root, nil
}

func loadServer(dir string, root *Root) (*Server, error) {
	if !a8.DirectoryExists(dir) {
		return nil, nil
	}
	serverHoconFile := filepath.Join(dir, "server.hocon")
	if !a8.FileExists(filepath.Join(dir, "server.hocon")) {
		return nil, nil
	}
	config := configuration.LoadConfig(serverHoconFile)
	if config == nil {
		return nil, stacktrace.NewError("error loading server.hocon file: %s", serverHoconFile)
	}

	server := &Server{
		Name:        filepath.Base(dir),
		Dir:         dir,
		VpnName:     config.GetString("vpnName"),
		NameInCaddy: config.GetString("nameInCaddy"),
		Root:        root,
	}

	server.Users = []*User{}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		userDir := filepath.Join(dir, e.Name())
		user, err := loadUser(userDir, server)
		if err != nil {
			log.Warn("error loading user %v: %v", userDir, err)
			continue
		}
		if user != nil {
			server.Users = append(server.Users, user)
		}
	}

	return server, nil
}

func loadUser(userDir string, server *Server) (*User, error) {
	if !a8.DirectoryExists(userDir) {
		return nil, nil
	}
	userHoconFile := filepath.Join(userDir, "user.hocon")
	if !a8.FileExists(userHoconFile) {
		return nil, nil
	}
	config := configuration.LoadConfig(userHoconFile)
	if config == nil {
		return nil, stacktrace.NewError("error loading user.hocon file: %s", userHoconFile)
	}

	user := &User{
		Name:   filepath.Base(userDir),
		Dir:    userDir,
		Server: server,
		Apps:   []*App{},
	}

	entries, err := os.ReadDir(userDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		appDir := filepath.Join(userDir, e.Name())
		app, err := loadApp(appDir, user)
		if err != nil {
			log.Warn("error loading app %v: %v", appDir, err)
			continue
		}
		if app != nil {
			user.Apps = append(user.Apps, app)
		}
	}

	return user, nil
}

func loadApp(appDir string, user *User) (*App, error) {
	if !a8.FileExists(filepath.Join(appDir, "application.hocon")) {
		return nil, nil
	}
	appDotHocon, err := loadApplicationDotHocon(appDir)
	lastDeploy, err := LoadLastDeployJson(appDir)

	if err != nil {
		return nil, err
	}

	app := &App{
		Name:                filepath.Base(appDir),
		Dir:                 appDir,
		User:                user,
		ApplicationDotHocon: appDotHocon,
		LastDeploy:          lastDeploy,
	}

	return app, nil

}

func (root *Root) Apps() []*App {
	apps := []*App{}
	for _, server := range root.Servers {
		for _, user := range server.Users {
			apps = append(apps, user.Apps...)
		}
	}
	return apps
}

func (root *Root) FindApp(domainName string) *App {
	for _, server := range root.Servers {
		for _, user := range server.Users {
			for _, app := range user.Apps {
				if app.ApplicationDotHocon.IsNamed(domainName) {
					return app
				}
			}
		}
	}
	return nil
}

func (user *User) HomeDir() string {
	return filepath.Join("/home/", user.Name)
}

func (user *User) AppsDir() string {
	return filepath.Join(user.HomeDir(), "apps")
}

func (user *User) SshName() string {
	return user.Name + "@" + user.Server.SshName()
}

func (user *User) BackupsDir() string {
	return filepath.Join(user.AppsDir(), ".backups")
}

func (server *Server) SshName() string {
	if server.VpnName != "" {
		return server.VpnName
	} else {
		return server.Name
	}
}

func (app *App) ExecPath() string {
	return filepath.Join(app.InstallDir(), "bin", app.Name)
}

func (Server *Server) CaddyName() string {
	if Server.NameInCaddy != "" {
		return Server.NameInCaddy
	} else {
		return Server.VpnName
	}
}

func (app *App) InstallDir() string {
	return filepath.Join(app.User.AppsDir(), app.Name)
}

func (adh *ApplicationDotHocon) DomainName() string {
	if len(adh.DomainNames) > 0 {
		return adh.DomainNames[0]
	}
	return ""
}

func (adh *ApplicationDotHocon) IsNamed(domainName string) bool {
	for _, dn := range adh.DomainNames {
		if dn == domainName {
			return true
		}
	}
	return false
}
