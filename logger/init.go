package logger

import (
	"context"
	"io"
	"os"
	"regexp"
	"strings"
)

// LogLevel defines the level of logging
type LogLevel int

const (
	LevelTrace LogLevel = iota
	LevelDebug
	LevelInfo
	LevelWarn
	LevelError
	LevelNone
)

// GetLevelFromEnv will look at the environment var `AGENTUITY_LOG_LEVEL` and convert it into the appropriate LogLevel
func GetLevelFromEnv() LogLevel {
	s := os.Getenv("AGENTUITY_LOG_LEVEL")
	switch strings.ToLower(s) { // Convert the string to lowercase to make it case-insensitive
	case "trace":
		return LevelTrace
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelDebug // Return an unknown or default value for invalid strings
	}
}

type Sink io.Writer

// Logger is an interface for logging
type Logger interface {
	// With will return a new logger using metadata as the base context
	With(metadata map[string]interface{}) Logger
	// WithPrefix will return a new logger with a prefix prepended to the message
	WithPrefix(prefix string) Logger
	// WithContext will return a new logger with the given context
	WithContext(ctx context.Context) Logger
	// Trace level logging
	Trace(msg string, args ...interface{})
	// Debug level logging
	Debug(msg string, args ...interface{})
	// Info level logging
	Info(msg string, args ...interface{})
	// Warning level logging
	Warn(msg string, args ...interface{})
	// Error level logging
	Error(msg string, args ...interface{})
	// Fatal level logging and exit with code 1
	Fatal(msg string, args ...interface{})
	// Stack will return a new logger that logs to the given logger as well as the current logger
	Stack(next Logger) Logger
	// IsLevelEnabled returns true if the given log level is enabled
	IsLevelEnabled(level LogLevel) bool
	// IsTraceEnabled returns true if trace level logging is enabled
	IsTraceEnabled() bool
	// IsDebugEnabled returns true if debug level logging is enabled
	IsDebugEnabled() bool
	// IsInfoEnabled returns true if info level logging is enabled
	IsInfoEnabled() bool
	// IsWarnEnabled returns true if warn level logging is enabled
	IsWarnEnabled() bool
	// IsErrorEnabled returns true if error level logging is enabled
	IsErrorEnabled() bool
}

type SinkLogger interface {
	Logger
	// SetSink will set the sink, and level to sink
	SetSink(sink Sink, level LogLevel)
}

var ansiColorStripper = regexp.MustCompile("\x1b\\[[0-9;]*[mK]")
