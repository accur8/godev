package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"

	"accur8.io/godev/a8"
	"accur8.io/godev/log"
	"github.com/palantir/stacktrace"
	"github.com/spf13/cobra"
)

/*
TODO ??? handle data directories
TODO ??? handle data files
TODO ??? .a8-install directory with build dir and backups dir
*/
type InstallDescriptor struct {
	Name               string `json:"name"`
	Organization       string `json:"organization"`
	Artifact           string `json:"artifact"`
	Version            string `json:"version"`
	InstallDir         string `json:"installDir"`
	WebappExplode      *bool  `json:"webappExplode"`
	MainClass          string `json:"mainClass"`
	Branch             string `json:"branch"`
	Repo               string `json:"repo"`
	JavaRuntimeVersion string `json:"javaRuntimeVersion"`
	BackupDir          string `json:"backupDir"`
}

// Create a new root command
var rootCmd *cobra.Command
var trace = false

func main() {

	// Create a new root command
	rootCmd = &cobra.Command{
		Use:   "a8-install",
		Short: "a8 app installer",
		Run: func(cmd *cobra.Command, args []string) {
			if trace {
				log.EnableTraceLogging()
			} else {
				log.ErrorLoggingOnly()
			}
			err := run(args)
			if err != nil {
				log.Error("%v", err)
				os.Exit(1)
			}
		},
	}

	// Add the --trace flag to the root command
	rootCmd.PersistentFlags().BoolVar(&trace, "trace", false, "Enable trace logging")

	// Execute the root command
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	os.Exit(0)

}

func run(commandLineArgs []string) error {

	if len(commandLineArgs) != 1 {
		return stacktrace.NewError("Usage: a8-install [--trace] <install-spec-dir>")
	}

	state := &InstallerState{
		CommandLineArgs: commandLineArgs,
	}

	installSpecDir := commandLineArgs[0]
	installSpecDirAbs, err := filepath.Abs(installSpecDir)
	if err != nil {
		return stacktrace.Propagate(err, "Error resolving source directory %v", installSpecDir)
	}
	state.InstallSpecDir = installSpecDirAbs

	a8InstallJsonFile := filepath.Join(installSpecDirAbs, "install-descriptor.json")
	a8InstallJsonBytes, err := a8.ReadFileE(a8InstallJsonFile)
	if err != nil {
		return stacktrace.Propagate(err, "Error reading %v", a8InstallJsonFile)
	}

	var args InstallDescriptor
	err = json.Unmarshal(a8InstallJsonBytes, &args)
	if err != nil {
		return stacktrace.Propagate(err, "Error unmarshaling JSON:")
	}

	err = validateArgs(&args)
	if err != nil {
		return stacktrace.Propagate(err, "Error validating arguments")
	}
	state.InstallDescriptor = &args

	err = InstallJavaProcess(state)
	if err != nil {
		return stacktrace.Propagate(err, "Error installing Java process")
	}

	return nil

}

func validateArgs(args *InstallDescriptor) error {

	errors := make([]error, 0)
	appendError := func(msg string) {
		errors = append(errors, stacktrace.NewError(msg))
	}

	if args.Name == "" {
		appendError("Name is required")
	}
	if args.Organization == "" {
		appendError("Organization is required")
	}
	if args.Artifact == "" {
		appendError("Artifact is required")
	}
	if args.InstallDir == "" {
		appendError("InstallDir is required")
	}
	if args.MainClass == "" {
		appendError("MainClass is required")
	}

	if args.Version == "" || args.Version == "latest" {
		if args.Branch == "" {
			appendError("Branch is required if version is latest")
		}
	}

	if len(errors) > 0 {
		return stacktrace.NewError("Validation errors: %v", errors)
	} else {
		return nil
	}

}

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

type InstallerState struct {
	InstallSpecDir              string
	InstallDescriptor           *InstallDescriptor
	NixBuildDescriptionRequest  *NixBuildDescriptionRequest
	NixBuildDescriptionResponse *NixBuildDescriptionResponse
	RepoProperties              map[string]string
	Repository                  *Repository
	Directories                 *ResolvedDirectories
	CommandLineArgs             []string
}

type ResolvedDirectories struct {
	TempInstallDir string
	TempBuildDir   string
	InstallDir     string
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

func InstallJavaProcess(state *InstallerState) error {

	// 0 - read repo.properties
	//    a - create a default one if it doesn't already exist
	//    b - use sensible defaults
	// 1 - install it
	//    a - call /api/nixBuildDescription to download the builder
	//    b - run the builder
	//    c - symlink stuff into the app dir

	resolvedDirectories, err := resolveDirectories(state)
	if err != nil {
		return err
	}
	state.Directories = resolvedDirectories

	reifiedInstallDescriptorJson := a8.ToJson(state.InstallDescriptor)
	err = a8.WriteFile(
		filepath.Join(state.Directories.TempBuildDir, "install-descriptor.json"),
		[]byte(reifiedInstallDescriptorJson),
	)
	if err != nil {
		log.Warn("Ignoring error writing args.json -- %v", err)
	}

	props, err := readRepoDotProps()
	if err != nil {
		return err
	}
	state.RepoProperties = props

	repo, err := resolveRepository(state.InstallDescriptor.Repo, state.RepoProperties)
	if err != nil {
		return err
	}
	state.Repository = repo

	buildDescription, err := FetchBuildDescription(state)
	if err != nil {
		return err
	}
	state.NixBuildDescriptionResponse = buildDescription

	err = runNixBuild(state)
	if err != nil {
		return err
	}

	err = setupInstallDir(state)
	if err != nil {
		return err
	}

	err = backupInstallDir(state)
	if err != nil {
		return err
	}

	log.Debug("Renaming %v to %v", state.Directories.TempInstallDir, state.Directories.InstallDir)
	err = os.Rename(state.Directories.TempInstallDir, state.Directories.InstallDir)
	if err != nil {
		return stacktrace.Propagate(err, "Error renaming directory %v to %v", state.Directories.TempInstallDir, state.Directories.InstallDir)
	}

	log.Debug("Installation completed successfully")

	return nil

}

func resolveDirectories(state *InstallerState) (*ResolvedDirectories, error) {
	installDir := state.InstallDescriptor.InstallDir
	installDir, err := filepath.Abs(installDir)
	if err != nil {
		return nil, stacktrace.Propagate(err, "Error resolving install directory %v", installDir)
	}
	log.Debug("Resolved install directory %v to %v", installDir, installDir)

	installDirParent := filepath.Dir(installDir)

	timestampStr := a8.FileSystemCompatibleTimestamp()

	tempInstallDir := filepath.Join(installDirParent, "build-"+state.InstallDescriptor.Name+"-"+timestampStr)

	tempBuildDir := filepath.Join(tempInstallDir, "nix")

	err = os.MkdirAll(tempBuildDir, 0755)
	if err != nil {
		return nil, stacktrace.Propagate(err, "Error creating tempBuildDir %v", tempBuildDir)
	}

	rd := &ResolvedDirectories{
		TempInstallDir: tempInstallDir,
		TempBuildDir:   tempBuildDir,
		InstallDir:     installDir,
	}

	log.Debug("Resolved directories %v", rd)

	return rd, nil

}

func backupInstallDir(state *InstallerState) error {
	if state.InstallDescriptor.BackupDir == "" {
		log.Debug("No backup directory specified, deleting the directory %v", state.Directories.InstallDir)
		err := os.RemoveAll(state.Directories.InstallDir)
		if err != nil {
			return stacktrace.Propagate(err, "Error deleting directory %v", state.Directories.InstallDir)
		}
		return nil
	} else if a8.DirectoryExists(state.InstallDescriptor.InstallDir) {
		backupRootDir, err := filepath.Abs(state.InstallDescriptor.BackupDir)
		if !a8.DirectoryExists(backupRootDir) {
			err := os.MkdirAll(backupRootDir, 0755)
			if err != nil {
				return stacktrace.Propagate(err, "Error creating root backup directory %v", backupRootDir)
			}
		}
		if err != nil {
			return stacktrace.Propagate(err, "will not backup, Invalid backup directory %v", state.InstallDescriptor.BackupDir)
		}
		backupName := state.InstallDescriptor.Name + "-" + a8.FileSystemCompatibleTimestamp()

		backupDir := filepath.Join(backupRootDir, backupName)

		if !a8.DirectoryExists(backupRootDir) {
			err := os.MkdirAll(backupRootDir, 0755)
			if err != nil {
				return stacktrace.Propagate(err, "Error creating root backup directory %v", backupRootDir)
			}
		}

		log.Debug("Backing up the install directory %v to %v", state.Directories.InstallDir, backupDir)

		err = os.Rename(state.Directories.InstallDir, backupDir)
		if err != nil {
			return stacktrace.Propagate(err, "Error moving directory: %v - %v", state.Directories.InstallDir, backupDir)
		}
		return nil
	} else {
		return nil
	}
}

func setupInstallDir(state *InstallerState) error {

	tempInstallDir := state.Directories.TempInstallDir

	log.Debug("preparing the contents of the temp install dir %v", tempInstallDir)

	errors := make([]error, 0)

	link := func(name string, linkSuffix string) {
		if linkSuffix != "" {
			linkSuffix = name
		}
		err := os.Symlink("nix/build/"+linkSuffix, filepath.Join(tempInstallDir, name))
		if err != nil {
			errors = append(errors, stacktrace.Propagate(err, "Error creating symlink %v", name))
		}
	}

	link("bin", "")
	link("lib", "")

	if state.InstallDescriptor.WebappExplode != nil && *state.InstallDescriptor.WebappExplode {
		link("webapp-composite", "webapp-composite/webapp")
	}

	if len(errors) > 0 {
		return stacktrace.NewError("Error setting up install directory: %v", errors)
	}

	log.Debug("Copying the install spec directory into the installation %v to %v", state.InstallSpecDir, tempInstallDir)
	err := copyDirectory(state.InstallSpecDir, tempInstallDir)
	if err != nil {
		return stacktrace.Propagate(err, "Error copying %v to %v", state.InstallSpecDir, tempInstallDir)
	}

	return nil
}

/*
//   a naive copy directory that should work well enough for our purposes
*/
func copyDirectory(sourceDir string, targetDir string) error {

	if !a8.DirectoryExists(targetDir) {
		os.MkdirAll(targetDir, 0755)
	}
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return stacktrace.Propagate(err, "Error reading directory %v", sourceDir)
	}
	for _, entry := range entries {
		sourceName := filepath.Join(sourceDir, entry.Name())
		targetName := filepath.Join(targetDir, entry.Name())
		if entry.IsDir() {
			err := copyDirectory(sourceName, targetName)
			if err != nil {
				return err
			}
		} else {
			content, err := a8.ReadFileE(sourceName)
			if err != nil {
				return stacktrace.Propagate(err, "Error reading file %v", sourceName)
			}
			err = a8.WriteFile(targetName, content)
			if err != nil {
				return stacktrace.Propagate(err, "Error writing file %v", targetName)
			}
		}
	}
	return nil
}

func runNixBuild(state *InstallerState) error {

	buildDir := state.Directories.TempBuildDir

	for _, file := range state.NixBuildDescriptionResponse.Files {
		filePath := filepath.Join(buildDir, file.Filename)
		err := a8.WriteFile(filePath, []byte(file.Contents))
		if err != nil {
			return stacktrace.Propagate(err, "Error writing file %v", filePath)
		}
	}

	cmdArgs := []string{"nix-build", "--out-link", "build", "-E", "with import <nixpkgs> {}; (callPackage ./default.nix {})"}
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Dir = buildDir

	{
		path := filepath.Join(buildDir, "build-description.json")
		bytes := a8.ToJsonBytes(state.NixBuildDescriptionResponse)
		err := a8.WriteFile(path, bytes)
		if err != nil {
			log.Warn("Error writing build description to %v -- %v", path, err)
		}
	}

	// Set the command's stdout and stderr to the existing program's stdout and stderr
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Debug("Running the nix build -- %v", cmdArgs)

	// Run the command
	err := cmd.Run()
	if err != nil {
		return stacktrace.Propagate(err, "Error executing command in %v -- %v", buildDir, cmd.Args)
	}

	return nil
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
		log.Debug("creating a8 directory since it does not exist -- %v", dirPath)
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
	log.Debug("Reading repo properties from %v", repoDotPropsPath)
	// Check if the path exists
	if _, err := os.Stat(repoDotPropsPath); os.IsNotExist(err) {
		// Create the file if it doesn't exist
		file, err := os.Create(repoDotPropsPath)
		if err != nil {
			return nil, stacktrace.Propagate(err, "Error creating file "+repoDotPropsPath)
		}
		log.Debug("creating repo properties file with sensible defaults since it does not exist -- %v", repoDotPropsPath)
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

func FetchBuildDescription(state *InstallerState) (*NixBuildDescriptionResponse, error) {

	resolvedVersion := state.InstallDescriptor.Version
	if resolvedVersion == "" {
		resolvedVersion = "latest"
	}

	requestDto := NixBuildDescriptionRequest{
		Name:          state.InstallDescriptor.Name,
		MainClass:     state.InstallDescriptor.MainClass,
		Organization:  state.InstallDescriptor.Organization,
		Artifact:      state.InstallDescriptor.Artifact,
		Version:       resolvedVersion,
		Branch:        state.InstallDescriptor.Branch,
		JavaVersion:   state.InstallDescriptor.JavaRuntimeVersion,
		WebappExplode: state.InstallDescriptor.WebappExplode,
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

func resolveRepository(repoPrefix string, repoProperties map[string]string) (*Repository, error) {

	var repo Repository

	if repoPrefix == "" {
		repoPrefix = "repo"
	}

	repo.Prefix = repoPrefix
	repo.RawRepoUrl = repoProperties[repoPrefix+"_url"]
	repo.User = repoProperties[repoPrefix+"_user"]
	repo.Password = repoProperties[repoPrefix+"_password"]

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
