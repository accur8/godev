package launchy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"

	"accur8.io/godev/a8"
	"accur8.io/godev/log"
	"github.com/palantir/stacktrace"
)

// {
// 	"kind": "jvm_cli",
// 	"mainClass": "a8.codegen.Codegen",
// 	"organization": "io.accur8",
// 	"artifact": "a8-codegen_2.13",
// 	"branch": "master",
// 	"repo": "repo"
// }

type NixBuildDescriptionRequest struct {
	Name          string   `json:"name"`
	MainClass     string   `json:"mainClass"`
	Organization  string   `json:"organization"`
	Artifact      string   `json:"artifact"`
	Version       string   `json:"version"`
	JavaVersion   string   `json:"javaVersion,omitempty"`
	JvmArgs       []string `json:"jvmArgs,omitempty"`
	Args          []string `json:"args,omitempty"`
	Repo          string   `json:"repo,omitempty"`
	Branch        string   `json:"branch"`
	WebappExplode *bool    `json:"webappExplode,omitempty"`
}

type LauncherJson struct {
	Name         string `json:"name"`
	MainClass    string `json:"mainClass"`
	Organization string `json:"organization"`
	Artifact     string `json:"artifact"`
	Branch       string `json:"branch"`
	Repo         string `json:"repo"`
	JavaVersion  string `json:"javaVersion"`
}

type InstalledApp struct {
	InstalledExecPath string
	Version           string
}

type JavaLauncherState struct {
	Name                        string
	LaunchArgs                  *LaunchArgs
	LauncherJson                *LauncherJson
	InstalledApp                *InstalledApp
	NixBuildDescriptionRequest  *NixBuildDescriptionRequest
	NixBuildDescriptionResponse *NixBuildDescriptionResponse
	RepoProperties              map[string]string
	CmdJavaProcessFlags         *CmdJavaProcessFlagsS
	Repository                  *Repository
}

type NixBuildDescriptionResponse struct {
	Files              []FileContents     `json:"files"`
	ResolvedVersion    string             `json:"resolvedVersion"`
	ResolutionResponse ResolutionResponse `json:"resolutionResponse"`
}

type FileContents struct {
	Filename string `json:"filename"`
	Contents string `json:"contents"`
}

type ResolutionResponse struct {
	Request   NixBuildDescriptionRequest `json:"request"`
	Version   string                     `json:"version"`
	RepoURL   string                     `json:"repoUrl"`
	Artifacts []ArtifactResponse         `json:"artifacts"`
}

type ArtifactResponse struct {
	URL          string `json:"url"` // Assuming Uri can be represented as a string
	Organization string `json:"organization"`
	Module       string `json:"module"`
	Version      string `json:"version"`
	Extension    string `json:"extension"`
}

type Repository struct {
	Prefix      string
	RawRepoUrl  string
	ResolvedUrl string
	RootUrl     string
	User        string
	Password    string
}

/*
launchy java-run --launcherJson=foo.json -- program args

	TODO options
		--launcherjson | --launcher-json

	TODO new standard parms
		--version
		--help

	standard parms
		--trace
		--logging
		--category
		--extraData
*/
func PrepareJavaProcess(action int, initialLaunchArgs *LaunchArgs, cmdJavaProcessFlags *CmdJavaProcessFlagsS) (*LaunchArgs, error) {

	state := &JavaLauncherState{
		LaunchArgs:          initialLaunchArgs,
		CmdJavaProcessFlags: cmdJavaProcessFlags,
	}

	// 0 - read repo.properties
	//    a - create a default one if it doesn't already exist
	//    b - use sensible defaults
	// 1 - read launcher json
	//    a - find launcher json
	//       i  - command line parm first
	//       ii - follow symlink second
	// 2 - check if already installed
	// 3 - if not installed install it
	// 	  a - figure out which versions we are going to install
	//    b - call /api/nixBuildDescription to download the builder
	//    c - run the builder
	//    d - updated the cache
	// 4 - return the args

	props, err := readRepoDotProps()
	if err != nil {
		return nil, err
	}
	state.RepoProperties = props

	launcherJson, err := readLauncherJson(state)
	if err != nil {
		return nil, err
	}
	state.LauncherJson = launcherJson

	installedApp, err := loadFromCache(state)
	if err != nil {
		return nil, err
	}

	if installedApp == nil {
		installedApp, err = installApp(state)
		if err != nil {
			return nil, err
		}
	}
	state.InstalledApp = installedApp

	return updateLaunchArgs(installedApp, initialLaunchArgs), nil
}

func resolveVersionsCacheDir(launcherJson *LauncherJson, version string, mkdir bool) (string, error) {

	path, err := resolveA8Dir()
	if err != nil {
		return "", stacktrace.Propagate(err, "Error resolving a8 directory")
	}

	path = filepath.Join(path, launcherJson.Organization, launcherJson.Artifact, version)

	if mkdir {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			// Create the directory if it doesn't exist
			err := os.MkdirAll(path, 0755)
			if err != nil {
				return "", stacktrace.Propagate(err, "Error creating directory %v", path)
			}
		}
	}

	return path, nil
}

func currentVersionDir(state *JavaLauncherState) (string, error) {
	return resolveVersionsCacheDir(state.LauncherJson, "current", false)
}

func installApp(state *JavaLauncherState) (*InstalledApp, error) {

	repo, err := resolveRepository(state.LauncherJson, state.RepoProperties)
	if err != nil {
		return nil, err
	}
	state.Repository = repo

	// store the build info in the cache
	buildDescription, err := FetchBuildDescription(state)
	if err != nil {
		return nil, err
	}
	state.NixBuildDescriptionResponse = buildDescription

	versionCacheDir, err := resolveVersionsCacheDir(state.LauncherJson, buildDescription.ResolvedVersion, true)
	if err != nil {
		return nil, err
	}

	for _, file := range state.NixBuildDescriptionResponse.Files {
		filePath := filepath.Join(versionCacheDir, file.Filename)
		err := a8.WriteFile(filePath, []byte(file.Contents))
		if err != nil {
			return nil, stacktrace.Propagate(err, "Error writing file %v", filePath)
		}
	}

	cmd := exec.Command("nix-build", "--out-link", "build", "-E", "with import <nixpkgs> {}; (callPackage ./default.nix {})")
	cmd.Dir = versionCacheDir

	{
		path := filepath.Join(versionCacheDir, "build-description.json")
		bytes := a8.ToJsonBytes(state.NixBuildDescriptionResponse)
		err := a8.WriteFile(path, bytes)
		if err != nil {
			log.Warn("Error writing build description to %v -- %v", path, err)
		}
	}

	// Set the command's stdout and stderr to the existing program's stdout and stderr
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Run the command
	err = cmd.Run()
	if err != nil {
		return nil, stacktrace.Propagate(err, "Error executing command in %v -- %v", versionCacheDir, cmd.Args)
	}

	cvd, err := currentVersionDir(state)
	if err != nil {
		return nil, err
	}
	if a8.DirectoryExists(cvd) {
		os.Remove(cvd)
	}

	err = os.Symlink(versionCacheDir, cvd)
	if err != nil {
		return nil, stacktrace.Propagate(err, "Error creating symlink %v -> %v", cvd, versionCacheDir)
	}

	return &InstalledApp{
		InstalledExecPath: filepath.Join(versionCacheDir, "build", "bin", state.Name),
	}, nil

}

func updateLaunchArgs(installedApp *InstalledApp, launchArgs *LaunchArgs) *LaunchArgs {
	originalCommand := launchArgs.Command
	launchArgs.Command = []string{installedApp.InstalledExecPath}
	launchArgs.Command = append(launchArgs.Command, originalCommand...)
	return launchArgs
}

func loadFromCache(state *JavaLauncherState) (*InstalledApp, error) {
	cvd, err := currentVersionDir(state)
	if err != nil {
		return nil, err
	}
	if a8.DirectoryExists(cvd) {
		return &InstalledApp{
			InstalledExecPath: filepath.Join(cvd, "bin", state.Name),
		}, nil
	} else {
		return nil, nil
	}
}

func readLauncherJson(state *JavaLauncherState) (*LauncherJson, error) {

	// TODO ??? check for a .json file next to the exec that was called

	// 1 - read launcher json
	//    a - find launcher json
	//       i  - command line parm first
	//       ii - follow symlink second

	var launcherJsonFilename string
	if state.CmdJavaProcessFlags.LauncherJsonFile == nil {
		exec, err := os.Executable()
		if err != nil {
			return nil, stacktrace.Propagate(err, "Error getting executable path")
		}
		launcherJsonFilename = exec + ".json"
	} else {
		launcherJsonFilename = *state.CmdJavaProcessFlags.LauncherJsonFile
	}

	// filename := state.Name
	launcherJsonFilename, err := filepath.Abs(launcherJsonFilename)
	if err != nil {
		return nil, stacktrace.Propagate(err, "Error getting absolute path for %v", "a8-codegen.launcher.json")
	}

	state.Name = "a8-codegen"

	jsonBytes, err := a8.ReadFileE(launcherJsonFilename)
	if err != nil {
		return nil, stacktrace.Propagate(err, "Error reading file %v", launcherJsonFilename)
	}

	var lancherJson LauncherJson
	err = json.Unmarshal(jsonBytes, &lancherJson)
	if err != nil {
		return nil, stacktrace.Propagate(err, "Error unmarshaling JSON from %v -- %v", launcherJsonFilename, string(jsonBytes))
	}

	return &lancherJson, nil

}

func resolveA8Dir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", stacktrace.Propagate(err, "Error getting home directory")
	}

	// Define the directory path
	dirPath := filepath.Join(homeDir, ".a8")

	// Check if the directory exists
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		// Create the directory if it doesn't exist
		err := os.Mkdir(dirPath, 0755)
		if err != nil {
			return "", stacktrace.Propagate(err, "Error creating directory %v", dirPath)
		}
	}
	return dirPath, nil
}

func readRepoDotProps() (map[string]string, error) {
	// 0 - read repo.properties
	//    a - create a default one if it doesn't already exist
	//    b - use sensible defaults
	// Get the user's home directory

	// Define the directory path
	dirPath, err := resolveA8Dir()
	if err != nil {
		return nil, stacktrace.Propagate(err, "Error resolving a8 directory")
	}

	repoDotPropsPath := filepath.Join(dirPath, "repo.properties")
	// Check if the path exists
	if _, err := os.Stat(repoDotPropsPath); os.IsNotExist(err) {
		// Create the file if it doesn't exist
		file, err := os.Create(repoDotPropsPath)
		if err != nil {
			return nil, stacktrace.Propagate(err, "Error creating file "+repoDotPropsPath)
		}
		file.Write([]byte(`
repo_url      = https://locus.accur8.net/repos/all
repo_realm    = Accur8 Repo
# repo_user     = my_repo_user
# repo_password = my_repo_password
		`))
		err = file.Close()
		if err != nil {
			return nil, stacktrace.Propagate(err, "Error closing file "+repoDotPropsPath)
		}
	}

	return a8.ReadPropertiesFile(repoDotPropsPath)

}

func FetchBuildDescription(state *JavaLauncherState) (*NixBuildDescriptionResponse, error) {

	resolvedVersion := ""
	if state.CmdJavaProcessFlags.Version == nil || *state.CmdJavaProcessFlags.Version == "" {
		resolvedVersion = "latest"
	} else {
		resolvedVersion = *state.CmdJavaProcessFlags.Version
	}

	requestDto := NixBuildDescriptionRequest{
		Name:         state.Name,
		MainClass:    state.LauncherJson.MainClass,
		Organization: state.LauncherJson.Organization,
		Artifact:     state.LauncherJson.Artifact,
		Version:      resolvedVersion,
		Branch:       state.LauncherJson.Branch,
		JavaVersion:  state.LauncherJson.JavaVersion,
	}

	apiUrl := state.Repository.RootUrl + "/api/nixBuildDescription"

	// Convert the struct to JSON
	requestBodyBytes, err := json.Marshal(requestDto)
	if err != nil {
		return nil, stacktrace.Propagate(err, "Error marshalling JSON:")
	}

	log.Trace("Fetching build description for %v", string(requestBodyBytes))

	// Create a new POST request
	req, err := http.NewRequest("POST", apiUrl, bytes.NewBuffer(requestBodyBytes))
	if err != nil {
		return nil, stacktrace.Propagate(err, "Error creating request:")
	}

	// Set the appropriate headers
	req.Header.Set("Content-Type", "application/json")

	// Perform the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, stacktrace.Propagate(err, "Error making request:")
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, stacktrace.Propagate(err, "Error reading response body:")
	}

	// Unmarshal the response body into a BuildDescriptionResponse object
	var response NixBuildDescriptionResponse
	err = json.Unmarshal(bodyBytes, &response)
	if err != nil {
		return nil, stacktrace.Propagate(err, "Error unmarshalling JSON: -- %v", string(bodyBytes))
	}

	state.NixBuildDescriptionRequest = &requestDto

	return &response, nil

}

func resolveRepository(launcherJson *LauncherJson, repoProperties map[string]string) (*Repository, error) {

	var repo Repository

	prefix := launcherJson.Repo
	if prefix == "" {
		prefix = "repo"
	}

	repo.Prefix = prefix
	repo.RawRepoUrl = repoProperties[prefix+"_url"]
	repo.User = repoProperties[prefix+"_user"]
	repo.Password = repoProperties[prefix+"_password"]

	parsedUrl, err := url.Parse(repo.RawRepoUrl)
	if err != nil {
		return nil, stacktrace.Propagate(err, "Error parsing URL %v", repo.RawRepoUrl)
	}

	resolvedRepoUrl := *parsedUrl

	if repo.User != "" {
		resolvedRepoUrl.User = url.UserPassword(repo.User, repo.Password)
	}

	repo.ResolvedUrl = resolvedRepoUrl.String()
	repo.RootUrl = a8.RootUrl(repo.ResolvedUrl)

	return &repo, nil
}
