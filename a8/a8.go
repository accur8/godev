package a8

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"text/template"
	"time"

	"accur8.io/godev/log"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/google/uuid"
	"github.com/mitchellh/go-homedir"
	"github.com/palantir/stacktrace"
)

type Uid = string

type utcTimestampImpl struct {
	utcEpochMillis int64
}

type UtcTimestamp interface {
	InEpochhMillis() int64
}

func (utc utcTimestampImpl) InEpochhMillis() int64 {
	return utc.utcEpochMillis
}

type AppFn = func(context.Context) error

func RandomUid() Uid {
	uuid0, err := uuid.NewRandom()
	if err != nil {
		panic(fmt.Sprintf("panic unable to create uuid's - %v", err))
	}
	return strings.ReplaceAll(uuid0.String(), "-", "")
}

type SyncMapS[K comparable, V any] struct {
	m sync.Map
}

type SyncMap[K comparable, V any] interface {
	Delete(key K)
	Load(key K) (value V, ok bool)
	LoadAndDelete(key K) (value V, loaded bool)
	LoadOrStore(key K, value V) (actual V, loaded bool)
	Range(f func(key K, value V) bool)
	Store(key K, value V)
	Values() []V
}

func NewSyncMap[K comparable, V any]() SyncMap[K, V] {
	return new(SyncMapS[K, V])
}

func (m *SyncMapS[K, V]) Values() []V {
	values := []V{}
	m.m.Range(func(key, value any) bool {
		values = append(values, value.(V))
		return true
	})
	return values
}

func (m *SyncMapS[K, V]) Delete(key K) { m.m.Delete(key) }
func (m *SyncMapS[K, V]) Load(key K) (value V, ok bool) {
	v, ok := m.m.Load(key)
	if !ok {
		return value, ok
	}
	return v.(V), ok
}
func (m *SyncMapS[K, V]) LoadAndDelete(key K) (value V, loaded bool) {
	v, loaded := m.m.LoadAndDelete(key)
	if !loaded {
		return value, loaded
	}
	return v.(V), loaded
}
func (m *SyncMapS[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool) {
	a, loaded := m.m.LoadOrStore(key, value)
	return a.(V), loaded
}
func (m *SyncMapS[K, V]) Range(f func(key K, value V) bool) {
	m.m.Range(func(key, value any) bool { return f(key.(K), value.(V)) })
}
func (m *SyncMapS[K, V]) Store(key K, value V) { m.m.Store(key, value) }

func Map[T, U any](ts []T, f func(T) U) []U {
	us := make([]U, len(ts))
	for i := range ts {
		us[i] = f(ts[i])
	}
	return us
}

// lops panics
func SubmitGoRoutine(context string, thunk func() error) {

	defer func() {
		if err := recover(); err != nil {
			log.Error("go routine %v ended with panic %v", context, err)
		}
	}()

	go func() {
		err := thunk()
		if err != nil {
			log.Error("go routine %v ended with error %v", context, err)
		} else {
			log.Debug("go routine %v ended normally without error", context)
		}
	}()
}

func ToJsonBytesE[A any](a A) ([]byte, error) {
	bytes, err := json.Marshal(a)
	if err != nil {
		return nil, stacktrace.Propagate(err, "unable to marshal to json - %+v", a)
	}
	return bytes, nil
}

func ToPrettyJsonBytes[A any](a A) []byte {
	bytes, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return nil
	}
	return bytes
}

func ToJsonBytes[A interface{}](a *A) []byte {
	bytes, err := json.Marshal(a)
	if err != nil {
		return nil
	}
	return bytes
}

func ToProtoJson[A proto.Message](a A) string {
	bytes, err := protojson.Marshal(a)
	if err != nil {
		return fmt.Sprintf("unable to marshal to json - %+v", a)
	}
	return string(bytes)
}

func ToProtoJsonE[A proto.Message](a A) ([]byte, error) {
	bytes, err := protojson.Marshal(a)
	if err != nil {
		return nil, stacktrace.Propagate(err, "unable to marshal to json - %+v", a)
	}
	return bytes, nil
}

func ToJson[A any](a A) string {
	bytes, err := ToJsonBytesE(a)
	if err != nil {
		return fmt.Sprintf("unable to marshal to json - %+v", a)
	}
	return string(bytes)
}

func NowUtcTimestamp() UtcTimestamp {
	return utcTimestampImpl{utcEpochMillis: time.Now().UTC().UnixMilli()}
}

func ParseUrl(rawURL string) *url.URL {
	url, err := url.Parse(rawURL)
	if err != nil {
		panic(err)
	}
	return url
}

func UserHomeDir() string {
	uh, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	return uh
}

func GetCwd() string {
	d, e := os.Getwd()
	if e != nil {
		panic(e)
	}
	return d
}

var Units = UnitsS{
	Kilobyte: 1024,
	Megabyte: 1024 * 1024,
}

type UnitsS struct {
	Kilobyte uint
	Megabyte uint
}

func ReadFile(filename string) []byte {
	if FileExists(filename) {
		bytes, _ := os.ReadFile(filename)
		return bytes
	} else {
		return nil
	}
}

func ReadFileE(filename string) ([]byte, error) {
	absname, err := filepath.Abs(filename)
	if err != nil {
		return nil, stacktrace.Propagate(err, "unable to get absolute path for %v", filename)
	}
	return os.ReadFile(absname)
}

func WriteFile(filename string, content []byte) error {
	// Write the content to the file with the given filename
	// using ioutil.WriteFile with default file permissions (0644)
	err := os.WriteFile(filename, content, 0644)
	if err != nil {
		return err
	}
	return nil
}

func PossibleConfigFiles(appName string) []string {

	filename := "/" + appName + ".json"

	possibles := []string{
		GetCwd() + filename,
		UserHomeDir() + "/.config/" + appName + filename,
		"/etc" + filename,
	}
	return possibles

}

func ConfigLookup[A interface{}](appName string) *A {
	a, _ := ConfigLookupE[A](appName)
	return a
}

func ConfigLookupE[A interface{}](appName string) (*A, error) {

	messages := []string{}

	logit := func(format string, a ...any) {
		msg := fmt.Sprintf(format, a...)
		log.Trace(msg)
		messages = append(messages, msg)
	}

	loadConfig := func(filename string) *A {
		logit("attempting to load config from %s", filename)
		configBytes := ReadFile(filename)
		if configBytes != nil {
			logit("loaded config from %s - %s", filename, string(configBytes))
			var a A
			err := json.Unmarshal(configBytes, &a)
			if err != nil {
				logit("json deserialize error reading %s - %v", filename, err)
			} else {
				logit("loaded config from %s - %s", filename, string(configBytes))
				return &a
			}
		}
		return nil
	}

	for _, d := range PossibleConfigFiles(appName) {
		cfg := loadConfig(d)
		if cfg != nil {
			logit("resolved config is %+v", cfg)
			return cfg, nil
		}
	}

	return nil, errors.New(strings.Join(messages, "\n"))

}

func UnwrapErrorChain(err error) []error {
	return unwrapErrorChainImpl(err, []error{})
}

func unwrapErrorChainImpl(err error, arr []error) []error {
	arr = append(arr, err)
	switch x := err.(type) {
	case interface{ Unwrap() error }:
		err = x.Unwrap()
		if err == nil {
			return []error{}
		}
		arr = unwrapErrorChainImpl(err, arr)
	case interface{ Unwrap() []error }:
		for _, err := range x.Unwrap() {
			arr = unwrapErrorChainImpl(err, arr)
		}
	}
	return arr
}

// runs multiple app functions and will wait for them to all
// end cleanly
func RunMultiRoutineApp(appFns ...AppFn) {
	var wg sync.WaitGroup
	wg.Add(len(appFns))

	errorsChan := make(chan error)

	combinedAppFn := func(ctx context.Context) error {
		for _, appFn := range appFns {
			appFn0 := appFn
			go func() {
				defer wg.Done()
				err := appFn0(ctx)
				if err != nil {
					log.Error("app coroutine ended with -- %v", err)
					errorsChan <- err
				}
			}()
		}
		go func() {
			log.Debug("waiting for app coroutines to complete")
			wg.Wait()
			log.Debug("all app coroutines complete")
			close(errorsChan)
		}()

		combinedErrors := ChanToSlice(errorsChan)
		return errors.Join(combinedErrors...)
	}

	RunApp(combinedAppFn)

}

func RunApp(appFn func(ctx context.Context) error) {

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	exec, err := os.Executable()
	if err != nil {
		exec = "unknown"
	}
	log.Debug("starting app %s %v", exec, os.Args)

	ctx, cancelCtxFn := context.WithCancel(context.Background())

	completion := make(chan error)
	complete := false

	go func() {
		err := appFn(ctx)
		complete = true
		if err != nil {
			err = stacktrace.Propagate(err, "main app go routine ended with an error")
			log.Error(err.Error())
			completion <- err
		} else {
			completion <- nil
		}
	}()

	cancel := func(source string) {
		log.Debug("cancelling main app ctx because %s", source)
		cancelCtxFn()
	}

	var returnErr error
	select {
	case sig := <-shutdown:
		cancel(fmt.Sprintf("received %v signal cancelling ctx", sig))
	case e := <-completion:
		if e != nil {
			returnErr = stacktrace.Propagate(e, "main app ended with an error")
			cancel(returnErr.Error())
		} else {
			cancel("main app go routine completed without an error")
		}
	}

	if !complete {
		log.Debug("waiting for main app go routine to complete")
		<-completion
	}
	log.Debug("main app completed")

}

func ChanToSlice[A interface{}](ch chan A) []A {
	s := make([]A, 0)
	for i := range ch {
		s = append(s, i)
	}
	return s
}

func StrPtr(s string) *string {
	return &s
}

type _AppS struct {
	WaitGroup              sync.WaitGroup
	SubProcesses           SyncMap[string, SubProcess]
	ParentContext          ContextI
	ShutdownLoggingTimeout time.Duration
	Shutdown               chan os.Signal
	Executable_            string
	Name_                  string
}

var _GlobalApp _AppS

func GlobalApp() App {
	return &_GlobalApp
}

func init() {

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	exec, err := os.Executable()
	if err != nil {
		panic(err)
	}
	// log.Debug("starting app %s %v", exec, os.Args)

	delegate, delegateCancelFn := context.WithCancel(context.Background())
	ctx := &ContextS{
		delegate: delegate,
		cancelFn: func() {
			log.Debug("cancelling GlobalApp context")
			delegateCancelFn()
		},
	}

	_GlobalApp = _AppS{
		SubProcesses:           NewSyncMap[string, SubProcess](),
		ParentContext:          ctx,
		Shutdown:               shutdown,
		ShutdownLoggingTimeout: 5 * time.Second,
		Executable_:            exec,
		Name_:                  filepath.Base(exec),
	}

}

type App interface {
	SubmitSubProcess(id string, run func(ctx ContextI) error) SubProcess
	WaitForCompletion()
	Context() ContextI
	Name() string
	Executable() string
}

type SubProcess interface {
	Id() string
	Run(ctx ContextI) error
	Done() <-chan struct{}
	Error() error
	Completed() bool
	Context() ContextI
}

type SubProcessS struct {
	id        string
	uuid      string
	run       func(ctx ContextI) error
	ctx       ContextI
	err       error
	completed bool
}

type ContextS struct {
	delegate context.Context
	cancelFn context.CancelFunc
}

type ContextI interface {
	context.Context

	SubProcessContext(subProcess SubProcess) ContextI
	ChildContext() ContextI

	Trace(format string, args ...any)
	Debug(format string, args ...any)
	Info(format string, args ...any)
	Warn(format string, args ...any)
	Error(format string, args ...any)

	Cancel()
}

func (app *_AppS) Context() ContextI {
	return app.ParentContext
}

func (app *_AppS) SubmitSubProcess(id string, run func(ctx ContextI) error) SubProcess {

	subProcess := &SubProcessS{
		uuid: uuid.New().String(),
		id:   id,
		run:  run,
	}

	subProcess.ctx = app.ParentContext.SubProcessContext(subProcess)

	app.SubProcesses.Store(subProcess.uuid, subProcess)

	go func() {

		defer func() {
			if err := recover(); err != nil {
				log.Error("%v", err)
				subProcess.err = stacktrace.NewError("SubProcess %v ended with panic %v", id, err)
				log.Error(subProcess.err.Error())
			}
			subProcess.completed = true
		}()

		app.WaitGroup.Add(1)
		defer app.WaitGroup.Done()

		log.Debug("starting SubProcess %v", id)

		subProcess.err = subProcess.Run(subProcess.ctx)
		subProcess.completed = true
		if subProcess.err != nil {
			log.Error("SubProcess %v ended with error %v", id, subProcess.err)
		} else {
			log.Debug("SubProcess %v ended normally", id)
		}

	}()
	return subProcess
}

func (app *_AppS) Executable() string {
	return app.Executable_
}

func (app *_AppS) Name() string {
	return app.Name_
}

func (app *_AppS) WaitForCompletion() {

	select {
	case signal := <-app.Shutdown:
		log.Debug("received shutdown signal %v", signal)
		app.ParentContext.Cancel()
	case <-app.ParentContext.Done():
		log.Debug("ParentContext.Done() completed")
	}

	go func() {
		timeout := app.ShutdownLoggingTimeout
		if timeout == 0 {
			timeout = 5 * time.Second
		}
		for {
			time.Sleep(timeout)
			app.SubProcesses.Range(func(_ string, subProcess SubProcess) bool {
				if !subProcess.Completed() {
					log.Debug("sub process %s is still running", subProcess.Id())
				}
				return true
			})
		}
	}()

	app.WaitGroup.Wait()

}

func (subProcess *SubProcessS) Id() string {
	return subProcess.id
}

func (subProcess *SubProcessS) Run(ctx ContextI) error {
	subProcess.ctx = ctx
	return subProcess.run(ctx)
}

func (subProcess *SubProcessS) Done() <-chan struct{} {
	return subProcess.ctx.Done()
}

func (subProcess *SubProcessS) Error() error {
	return subProcess.err
}

func (subProcess *SubProcessS) Completed() bool {
	return subProcess.completed
}

func (cw *ContextS) Trace(format string, args ...any) {
	log.Trace(format, args...)
}

func (cw *ContextS) Debug(format string, args ...any) {
	log.Debug(format, args...)
}

func (cw *ContextS) Info(format string, args ...any) {
	log.Info(format, args...)
}

func (cw *ContextS) Warn(format string, args ...any) {
	log.Warn(format, args...)
}

func (cw *ContextS) Error(format string, args ...any) {
	log.Error(format, args...)
}

func (cw *ContextS) SubProcessContext(subProcess SubProcess) ContextI {
	return cw.ChildContext()
}

func (cw *ContextS) ChildContext() ContextI {
	ctx, cancelFn := context.WithCancel(cw.delegate)
	return &ContextS{
		delegate: ctx,
		cancelFn: cancelFn,
	}
}

func (cw *ContextS) Deadline() (deadline time.Time, ok bool) {
	return cw.delegate.Deadline()
}

func (cw *ContextS) Done() <-chan struct{} {
	return cw.delegate.Done()
}

func (cw *ContextS) Err() error {
	return cw.delegate.Err()
}

func (cw *ContextS) Value(key any) any {
	return cw.delegate.Value(key)
}

func (cw *ContextS) Cancel() {
	cw.cancelFn()
}

func (sp *SubProcessS) Context() ContextI {
	return sp.ctx
}

func IsValidJson(str string) bool {
	var js json.RawMessage
	return json.Unmarshal([]byte(str), &js) == nil
}

func NewTrue() *bool {
	b := true
	return &b
}

func NewFalse() *bool {
	b := false
	return &b
}

func IsLinux() bool {
	// return true
	os := runtime.GOOS
	return os == "linux"
}

func PathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func DirExists(path string) (bool, error) {
	s, err := os.Stat(path)
	if err == nil {
		return s.IsDir(), nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func ValidateDirectory(dir string) (string, error) {
	dirPath, err := homedir.Expand(dir)
	if err != nil {
		return "", stacktrace.Propagate(err, "error expanding home directory: %v\n", dir)
	}
	info, err := os.Stat(dirPath)
	if os.IsNotExist(err) {
		return "", stacktrace.Propagate(err, "Directory does not exist: %v\n", dirPath)
	}
	if err != nil {
		return "", stacktrace.Propagate(err, "Directory error: %v\n", dirPath)

	}
	if !info.IsDir() {
		return "", stacktrace.NewError("Directory is a file, not a directory: %#v\n", dirPath)
	}
	return dirPath, nil
}

func ConcatMultipleSlices[T any](slices [][]T) []T {
	var totalLen int

	for _, s := range slices {
		totalLen += len(s)
	}

	result := make([]T, totalLen)

	var i int

	for _, s := range slices {
		i += copy(result[i:], s)
	}

	return result
}

func RemoveDupes[T comparable](sliceList []T) []T {
	allKeys := make(map[T]bool)
	list := []T{}
	for _, item := range sliceList {
		if _, value := allKeys[item]; !value {
			allKeys[item] = true
			list = append(list, item)
		}
	}
	return list
}

func NewContext() ContextI {
	delegate, cancelFn := context.WithCancel(context.Background())
	return &ContextS{
		delegate: delegate,
		cancelFn: cancelFn,
	}
}

func RunPar[A any, B any](items []A, runnerFn func(A) B) []B {
	numWorkers := len(items)

	var wg sync.WaitGroup
	resultsCh := make(chan B, numWorkers)

	// Launch coroutines (goroutines)
	for _, item := range items {
		item0 := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			resultsCh <- runnerFn(item0)
		}()
	}

	// Wait for all goroutines to finish
	wg.Wait()
	close(resultsCh)

	// Collect results
	results := []B{}
	for result := range resultsCh {
		results = append(results, result)
	}
	return results
}

func ReadPropertiesFile(filename string) (map[string]string, error) {
	// Open the .properties file
	file, err := os.Open(filename)
	if err != nil {
		return nil, stacktrace.Propagate(err, "Error opening file: %v", filename)
	}
	defer file.Close()

	// Create a map to store key-value pairs
	properties := make(map[string]string)

	// Read the file line by line
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		// Trim spaces and skip empty lines or comments
		line = strings.TrimSpace(line)
		if len(line) == 0 || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}

		// Split the line at the first '=' or ':'
		var key, value string
		if strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			key, value = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		} else if strings.Contains(line, ":") {
			parts := strings.SplitN(line, ":", 2)
			key, value = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		} else {
			continue // Skip lines without a separator
		}

		// Store in the map
		properties[key] = value
	}

	return properties, nil

}

// Function to normalize a URL to its root
func RootUrl(rawURL string) string {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	port := parsedURL.Port()
	if port != "" {
		port = ":" + port
	}

	// Construct the root URL using only the scheme and host
	rootURL := fmt.Sprintf("%s://%s%s", parsedURL.Scheme, parsedURL.Host, port)
	return rootURL
}

func DirectoryExists(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return info.IsDir()
}

func FileExists(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func ResolveSymlinkChain(path string) ([]string, error) {
	chain := []string{}
	currentPath := path

	for {
		absPath, err := filepath.Abs(currentPath)
		if err != nil {
			return nil, stacktrace.Propagate(err, "unable to get absolute path for %v", currentPath)
		}
		chain = append(chain, absPath)
		currentPath, err = os.Readlink(currentPath)
		if err != nil {
			if os.IsNotExist(err) || os.IsPermission(err) {
				// We reached a point where currentPath is not a symlink
				break
			}
			return nil, err
		}
	}

	return chain, nil
}

func FileSystemCompatibleTimestamp() string {
	t := time.Now()
	timestampStr := t.Format("20060102-150405")
	return timestampStr
}

type TemplateRequest struct {
	Name    string
	Content string
	Data    interface{}
}

func TemplatedString(req *TemplateRequest) (string, error) {
	if req.Name == "" {
		req.Name = "template"
	}
	content := req.Content
	data := req.Data
	tmpl, err := template.New(req.Name).Parse(req.Content)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to parse template -- %v", content)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to execute template -- %v -- %v", content, data)
	}

	return buf.String(), nil
}
