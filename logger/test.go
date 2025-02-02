package logger

import "os"

type TestLogEntry struct {
	Severity  string
	Message   string
	Arguments []interface{}
}

type TestLogger struct {
	metadata map[string]interface{}
	Logs     []TestLogEntry
}

var _ Logger = (*TestLogger)(nil)

func (c *TestLogger) WithSink(sink Sink, level LogLevel) Logger {
	return c
}

// WithPrefix will return a new logger with a prefix prepended to the message
func (c *TestLogger) WithPrefix(prefix string) Logger {
	return c
}

func (c *TestLogger) With(metadata map[string]interface{}) Logger {
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
	return &TestLogger{kv, c.Logs}
}

func (c *TestLogger) Log(level string, msg string, args ...interface{}) {
	c.Logs = append(c.Logs, TestLogEntry{level, msg, args})
}

func (c *TestLogger) Trace(msg string, args ...interface{}) {
	c.Log("TRACE", msg, args...)
}

func (c *TestLogger) Debug(msg string, args ...interface{}) {
	c.Log("DEBUG", msg, args...)
}

func (c *TestLogger) Info(msg string, args ...interface{}) {
	c.Log("INFO", msg, args...)
}

func (c *TestLogger) Warn(msg string, args ...interface{}) {
	c.Log("WARNING", msg, args...)
}

func (c *TestLogger) Error(msg string, args ...interface{}) {
	c.Log("ERROR", msg, args...)
}

func (c *TestLogger) Fatal(msg string, args ...interface{}) {
	c.Log("FATAL", msg, args...)
	os.Exit(1)
}

// NewTestLogger returns a new Logger instance useful for testing
func NewTestLogger() *TestLogger {
	return &TestLogger{
		Logs: make([]TestLogEntry, 0),
	}
}
