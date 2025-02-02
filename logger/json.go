package logger

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
)

// JSONLogEntry defines a log entry
// this is modeled after the JSON format expected by Cloud Logging
// https://github.com/GoogleCloudPlatform/golang-samples/blob/08bc985b4973901c09344eabbe9d7d5add7dc656/run/logging-manual/main.go
type JSONLogEntry struct {
	Timestamp time.Time              `json:"timestamp,omitempty"`
	Message   string                 `json:"message"`
	Severity  string                 `json:"severity,omitempty"`
	Trace     string                 `json:"logging.googleapis.com/trace,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	// Logs Explorer allows filtering and display of this as `jsonPayload.component`.
	Component string `json:"component,omitempty"`
	logLevel  LogLevel
}

// String renders an entry structure to the JSON format expected by Cloud Logging.
func (e JSONLogEntry) String() string {
	if e.Severity == "" {
		e.Severity = "INFO"
	}
	out, err := json.Marshal(e)
	if err != nil {
		log.Printf("json.Marshal: %v", err)
	}
	return string(out)
}

type jsonLogger struct {
	metadata     map[string]interface{}
	traceID      string
	component    string
	sink         Sink
	sinkLogLevel LogLevel
	noConsole    bool
	ts           *time.Time // for unit testing
	logLevel     LogLevel
}

var _ Logger = (*jsonLogger)(nil)

func (c *jsonLogger) SetSink(sink Sink, level LogLevel) {
	c.sink = sink
	c.sinkLogLevel = level
}

// WithPrefix will return a new logger with a prefix prepended to the message
func (c *jsonLogger) WithPrefix(prefix string) Logger {
	newlogger := c.With(nil).(*jsonLogger)
	if c.component == "" {
		newlogger.component = prefix
	} else {
		if !strings.Contains(c.component, prefix) {
			newlogger.component = c.component + " " + prefix
		}
	}
	return newlogger
}

func (c *jsonLogger) With(metadata map[string]interface{}) Logger {
	traceID := c.traceID
	component := c.component
	if trace, ok := metadata["trace"].(string); ok {
		traceID = trace
		delete(metadata, "trace")
	}
	if comp, ok := metadata["component"].(string); ok {
		component = comp
		delete(metadata, "component")
	}
	kv := metadata
	if c.metadata != nil {
		kv = make(map[string]interface{})
		for k, v := range c.metadata {
			kv[k] = v
		}
		for k, v := range metadata {
			kv[k] = v
		}
	}
	if len(kv) == 0 {
		kv = nil
	}
	return &jsonLogger{
		metadata:     kv,
		traceID:      traceID,
		component:    component,
		noConsole:    c.noConsole,
		sink:         c.sink,
		sinkLogLevel: c.sinkLogLevel,
		logLevel:     c.logLevel,
	}
}

var bracketRegex = regexp.MustCompile(`\[(.*?)\]`)

func (c *jsonLogger) tokenize(val string) string {
	if bracketRegex.MatchString(val) {
		vals := make([]string, 0)
		for _, token := range bracketRegex.FindAllString(val, -1) {
			vals = append(vals, bracketRegex.ReplaceAllString(token, "$1"))
		}
		return strings.Join(vals, ", ")
	}
	return val
}

func (c *jsonLogger) Log(level LogLevel, severity string, msg string, args ...interface{}) {
	if level < c.logLevel && level < c.sinkLogLevel {
		return
	}
	_msg := msg
	if len(args) > 0 {
		_msg = fmt.Sprintf(msg, args...)
	}
	entry := JSONLogEntry{
		Severity:  severity,
		Message:   _msg,
		Trace:     c.traceID,
		Metadata:  c.metadata,
		Component: c.tokenize(c.component),
		Timestamp: time.Now(),
	}
	if !c.noConsole && level >= c.logLevel {
		log.Println(entry)
	}
	if c.sink != nil && level >= c.sinkLogLevel {
		entry.Message = ansiColorStripper.ReplaceAllString(entry.Message, "")
		if c.ts != nil {
			entry.Timestamp = *c.ts // for testing
		}
		buf, _ := json.Marshal(entry)
		if _, err := c.sink.Write(buf); err != nil {
			log.Printf("sink.Write: %v", err)
		}
	}
}

func (c *jsonLogger) Trace(msg string, args ...interface{}) {
	c.Log(LevelTrace, "TRACE", msg, args...)
}

func (c *jsonLogger) Debug(msg string, args ...interface{}) {
	c.Log(LevelDebug, "DEBUG", msg, args...)
}

func (c *jsonLogger) Info(msg string, args ...interface{}) {
	c.Log(LevelInfo, "INFO", msg, args...)
}

func (c *jsonLogger) Warn(msg string, args ...interface{}) {
	c.Log(LevelWarn, "WARNING", msg, args...)
}

func (c *jsonLogger) Error(msg string, args ...interface{}) {
	c.Log(LevelError, "ERROR", msg, args...)
}

func (c *jsonLogger) Fatal(msg string, args ...interface{}) {
	c.Log(LevelError, "ERROR", msg, args...)
}

func (c *jsonLogger) SetLogLevel(level LogLevel) {
	c.logLevel = level
}

// NewJSONLogger returns a new Logger instance which can be used for structured logging
func NewJSONLogger(levels ...LogLevel) Logger {
	if len(levels) > 0 {
		return &jsonLogger{logLevel: levels[0]}
	}
	level := GetLevelFromEnv()
	return &jsonLogger{logLevel: level}

}

// NewJSONLoggerWithSink returns a new Logger instance using a sink and suppressing the console logging
func NewJSONLoggerWithSink(sink Sink, level LogLevel) SinkLogger {
	return &jsonLogger{noConsole: true, sink: sink, sinkLogLevel: level}
}
