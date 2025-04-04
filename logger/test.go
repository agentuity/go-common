package logger

import (
	"context"
	"os"
)

type TestLogEntry struct {
	Severity  string
	Message   string
	Arguments []interface{}
}

type TestLogger struct {
	metadata map[string]interface{}
	Logs     []TestLogEntry
	child    Logger
}

var _ Logger = (*TestLogger)(nil)

func (c *TestLogger) WithSink(sink Sink, level LogLevel) Logger {
	return c
}

func (c *TestLogger) WithContext(ctx context.Context) Logger {
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
	child := c.child
	if child != nil {
		child = child.With(metadata)
	}
	return &TestLogger{metadata: kv, Logs: c.Logs, child: child}
}

func (c *TestLogger) Log(level string, msg string, args ...interface{}) {
	c.Logs = append(c.Logs, TestLogEntry{level, msg, args})
}

func (c *TestLogger) Trace(msg string, args ...interface{}) {
	c.Log("TRACE", msg, args...)
	if c.child != nil {
		c.child.Trace(msg, args...)
	}
}

func (c *TestLogger) Debug(msg string, args ...interface{}) {
	c.Log("DEBUG", msg, args...)
	if c.child != nil {
		c.child.Debug(msg, args...)
	}
}

func (c *TestLogger) Info(msg string, args ...interface{}) {
	c.Log("INFO", msg, args...)
	if c.child != nil {
		c.child.Info(msg, args...)
	}
}

func (c *TestLogger) Warn(msg string, args ...interface{}) {
	c.Log("WARNING", msg, args...)
	if c.child != nil {
		c.child.Warn(msg, args...)
	}
}

func (c *TestLogger) Error(msg string, args ...interface{}) {
	c.Log("ERROR", msg, args...)
	if c.child != nil {
		c.child.Error(msg, args...)
	}
}

func (c *TestLogger) Fatal(msg string, args ...interface{}) {
	c.Log("FATAL", msg, args...)
	if c.child != nil {
		c.child.Fatal(msg, args...)
	}
	os.Exit(1)
}

func (c *TestLogger) Stack(next Logger) Logger {
	return &TestLogger{c.metadata, c.Logs, next}
}

// NewTestLogger returns a new Logger instance useful for testing
func NewTestLogger() *TestLogger {
	return &TestLogger{
		Logs: make([]TestLogEntry, 0),
	}
}
