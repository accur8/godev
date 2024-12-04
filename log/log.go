package log

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gookit/color"
	"github.com/petermattis/goid"
	"gopkg.in/natefinch/lumberjack.v2"
)

var IsTraceEnabled bool
var IsErrorEnabled bool
var IsLoggingEnabled bool

// ConsoleMode = true means we log to stdout OR stderr for warn and above
// ConsoleMode = false means we log to stdout AND stderr for warn and above
var ConsoleMode bool = true

func init() {
	IsTraceEnabled = true
	IsLoggingEnabled = true
}

var errorWriter *lumberjack.Logger
var detailWriter *lumberjack.Logger

var shortTimestampFixes = []string{
	"0Z",
	"00Z",
	"000Z",
	"0000Z",
	"00000Z",
	"000000Z",
}

const shortTimestampFixesLen = 6

func InitFileLogging(logDir string, appName string) {
	logDir, _ = filepath.Abs(logDir)
	detailsFile := filepath.Join(logDir, appName+"-details.log")
	errorsFile := filepath.Join(logDir, appName+"-errors.log")
	detailsFile, _ = filepath.Abs(detailsFile)
	errorsFile, _ = filepath.Abs(errorsFile)
	os.MkdirAll(logDir, 0700)
	errorWriter = &lumberjack.Logger{
		Filename:   errorsFile,
		MaxSize:    15, // megabytes
		MaxAge:     14, //days
		Compress:   true,
		MaxBackups: 10,
		LocalTime:  false,
	}
	detailWriter = &lumberjack.Logger{
		Filename:   detailsFile,
		MaxSize:    50, // megabytes
		MaxAge:     14, //days
		Compress:   true,
		MaxBackups: 10,
		LocalTime:  false,
	}
	Debug("logging to files: %s %s", detailsFile, errorsFile)
}

func EnableTraceLogging() {
	IsTraceEnabled = true
	IsLoggingEnabled = true
}

func ErrorLoggingOnly() {
	IsTraceEnabled = false
	IsLoggingEnabled = false
	IsErrorEnabled = true
}

type logLevel struct {
	name  string
	index int
}

var (
	_TRACE = &logLevel{"trace", 0}
	_DEBUG = &logLevel{"debug", 1}
	_INFO  = &logLevel{"info ", 2}
	_WARN  = &logLevel{"warn ", 3}
	_ERROR = &logLevel{"error", 4}
)

func Trace(format string, args ...any) {
	if IsTraceEnabled {
		logit(_TRACE, format, args...)
	}
}

func Debug(format string, args ...any) {
	logit(_DEBUG, format, args...)
}

func Info(format string, args ...any) {
	logit(_INFO, format, args...)
}

func Warn(format string, args ...any) {
	logit(_WARN, format, args...)
}

func Error(format string, args ...any) {
	logit(_ERROR, format, args...)
}

var logTimeStampFormat = "2006-01-02T15:04:05.999999Z07:00"

func logit(level *logLevel, format string, args ...any) {

	if IsLoggingEnabled || IsTraceEnabled || (level.index >= _WARN.index && IsErrorEnabled) {

		logMessage := fmt.Sprintf(format, args...)

		// TIME | LEVEL | GOID | MESSAGE
		nowStr := time.Now().UTC().Format(logTimeStampFormat) // "2006-01-02T15:04:05.666Z"

		zeroCount := 27 - len(nowStr)
		if zeroCount >= 0 {
			if zeroCount > shortTimestampFixesLen {
				zeroCount = shortTimestampFixesLen
			}
			replace := shortTimestampFixes[zeroCount]
			nowStr = strings.ReplaceAll(nowStr, "Z", replace)
		}

		goid := goid.Get()

		levelName := level.name
		if ConsoleMode {
			var levelColor color.Color256
			switch level.index {
			case _TRACE.index:
				levelColor = color.C256(33)
			case _DEBUG.index:
				levelColor = color.C256(51)
			case _INFO.index:
				levelColor = color.C256(46)
			case _WARN.index:
				levelColor = color.C256(214)
			case _ERROR.index:
				levelColor = color.C256(196)

			}
			levelName = levelColor.Sprintf(level.name)
		}

		message := fmt.Sprintf("%s | %s | %d | %s", nowStr, levelName, goid, logMessage)

		if level.index >= _WARN.index {
			fmt.Fprintln(os.Stderr, message)
			if errorWriter != nil {
				errorWriter.Write([]byte(message + "\n"))
			}
		}
		if !ConsoleMode || level.index < _WARN.index {
			fmt.Println(message)
		}
		if detailWriter != nil {
			detailWriter.Write([]byte(message + "\n"))
		}

	}

}
