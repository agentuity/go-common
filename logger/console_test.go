package logger

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func captureOutput(f func()) string {
	prev := log.Writer()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(prev)
	f()
	return strings.TrimSpace(buf.String())
}

func TestConsoleLogger(t *testing.T) {

	logger := NewConsoleLogger().(*consoleLogger)

	tests := []struct {
		level            LogLevel
		shouldContain    []string
		shouldNotContain []string
	}{
		{
			level:            LevelTrace,
			shouldContain:    []string{"TRACE", "DEBUG", "INFO", "WARN", "ERROR"},
			shouldNotContain: []string{},
		},
		{
			level:            LevelDebug,
			shouldContain:    []string{"DEBUG", "INFO", "WARN", "ERROR"},
			shouldNotContain: []string{"TRACE"},
		},
		{
			level:            LevelInfo,
			shouldContain:    []string{"INFO", "WARN", "ERROR"},
			shouldNotContain: []string{"TRACE", "DEBUG"},
		},
		{
			level:            LevelWarn,
			shouldContain:    []string{"WARN", "ERROR"},
			shouldNotContain: []string{"TRACE", "DEBUG", "INFO"},
		},
		{
			level:            LevelError,
			shouldContain:    []string{"ERROR"},
			shouldNotContain: []string{"TRACE", "DEBUG", "INFO", "WARN"},
		},
	}

	for _, tt := range tests {
		logger.SetLogLevel(tt.level)
		output := captureOutput(func() {
			logger.Trace("This is a trace message")
			logger.Debug("This is a debug message")
			logger.Info("This is an info message")
			logger.Warn("This is a warn message")
			logger.Error("This is an error message")
		})
		for _, shouldContain := range tt.shouldContain {
			assert.Contains(t, output, shouldContain)
		}
		for _, shouldNotContain := range tt.shouldNotContain {
			assert.NotContains(t, output, shouldNotContain)
		}
	}
}

func TestConsoleLoggerWithEnvLevel(t *testing.T) {

	tests := []struct {
		level            string
		shouldContain    []string
		shouldNotContain []string
	}{
		{
			level:            "TRACE",
			shouldContain:    []string{"TRACE", "DEBUG", "INFO", "WARN", "ERROR"},
			shouldNotContain: []string{},
		},
		{
			level:            "DEBUG",
			shouldContain:    []string{"DEBUG", "INFO", "WARN", "ERROR"},
			shouldNotContain: []string{"TRACE"},
		},
		{
			level:            "INFO",
			shouldContain:    []string{"INFO", "WARN", "ERROR"},
			shouldNotContain: []string{"TRACE", "DEBUG"},
		},
		{
			level:            "WARN",
			shouldContain:    []string{"WARN", "ERROR"},
			shouldNotContain: []string{"TRACE", "DEBUG", "INFO"},
		},
		{
			level:            "ERROR",
			shouldContain:    []string{"ERROR"},
			shouldNotContain: []string{"TRACE", "DEBUG", "INFO", "WARN"},
		},
	}

	for _, tt := range tests {
		os.Setenv("AGENTUITY_LOG_LEVEL", tt.level)
		logger := NewConsoleLogger().(*consoleLogger)

		output := captureOutput(func() {
			logger.Trace("This is a trace message")
			logger.Debug("This is a debug message")
			logger.Info("This is an info message")
			logger.Warn("This is a warn message")
			logger.Error("This is an error message")
		})
		for _, shouldContain := range tt.shouldContain {
			assert.Contains(t, output, shouldContain)
		}
		for _, shouldNotContain := range tt.shouldNotContain {
			assert.NotContains(t, output, shouldNotContain)
		}
		os.Unsetenv("AGENTUITY_LOG_LEVEL")
	}
}

func TestConsoleLoggerWithMetadata(t *testing.T) {
	logger := NewConsoleLogger().(*consoleLogger)

	metadata := map[string]interface{}{
		"key1": "value1",
		"key2": "value2",
	}
	logger = logger.With(metadata).(*consoleLogger)

	output := captureOutput(func() {
		logger.Info("This is an info message with metadata")
	})

	assert.Contains(t, output, "This is an info message with metadata")
	assert.Contains(t, output, `"key1":"value1"`)
	assert.Contains(t, output, `"key2":"value2"`)
}

func TestConsoleLoggerSinkTraceLevel(t *testing.T) {
	logger := NewConsoleLogger().(*consoleLogger)
	logger.SetLogLevel(LevelTrace)

	output := captureOutput(func() {
		logger.Trace("This is a trace message")
	})
	assert.Contains(t, output, "This is a trace message")

	output = captureOutput(func() {
		logger.Debug("This is a debug message")
	})
	assert.Contains(t, output, "This is a debug message")

	output = captureOutput(func() {
		logger.Info("This is an info message")
	})
	assert.Contains(t, output, "This is an info message")

	output = captureOutput(func() {
		logger.Warn("This is a warn message")
	})
	assert.Contains(t, output, "This is a warn message")

	output = captureOutput(func() {
		logger.Error("This is an error message")
	})
	assert.Contains(t, output, "This is an error message")
}

func TestConsoleLoggerSinkDebugLevel(t *testing.T) {
	logger := NewConsoleLogger().(*consoleLogger)
	logger.SetLogLevel(LevelDebug)

	output := captureOutput(func() {
		logger.Trace("This trace message should not be printed")
	})
	assert.NotContains(t, output, "This trace message should not be printed")

	output = captureOutput(func() {
		logger.Debug("This is a debug message")
	})
	assert.Contains(t, output, "This is a debug message")

	output = captureOutput(func() {
		logger.Info("This is an info message")
	})
	assert.Contains(t, output, "This is an info message")

	output = captureOutput(func() {
		logger.Warn("This is a warn message")
	})
	assert.Contains(t, output, "This is a warn message")

	output = captureOutput(func() {
		logger.Error("This is an error message")
	})
	assert.Contains(t, output, "This is an error message")
}

func TestConsoleLoggerSinkInfoLevel(t *testing.T) {
	logger := NewConsoleLogger().(*consoleLogger)
	logger.SetLogLevel(LevelInfo)

	output := captureOutput(func() {
		logger.Trace("This trace message should not be printed")
	})
	assert.NotContains(t, output, "This trace message should not be printed")

	output = captureOutput(func() {
		logger.Debug("This debug message should not be printed")
	})
	assert.NotContains(t, output, "This debug message should not be printed")

	output = captureOutput(func() {
		logger.Info("This is an info message")
	})
	assert.Contains(t, output, "This is an info message")

	output = captureOutput(func() {
		logger.Warn("This is a warn message")
	})
	assert.Contains(t, output, "This is a warn message")

	output = captureOutput(func() {
		logger.Error("This is an error message")
	})
	assert.Contains(t, output, "This is an error message")
}

func TestConsoleLoggerSinkWarnLevel(t *testing.T) {
	logger := NewConsoleLogger().(*consoleLogger)
	logger.SetLogLevel(LevelWarn)

	output := captureOutput(func() {
		logger.Trace("This trace message should not be printed")
	})
	assert.NotContains(t, output, "This trace message should not be printed")

	output = captureOutput(func() {
		logger.Debug("This debug message should not be printed")
	})
	assert.NotContains(t, output, "This debug message should not be printed")

	output = captureOutput(func() {
		logger.Info("This info message should not be printed")
	})
	assert.NotContains(t, output, "This info message should not be printed")

	output = captureOutput(func() {
		logger.Warn("This is a warn message")
	})
	assert.Contains(t, output, "This is a warn message")

	output = captureOutput(func() {
		logger.Error("This is an error message")
	})
	assert.Contains(t, output, "This is an error message")
}

func TestConsoleLoggerSinkErrorLevel(t *testing.T) {
	logger := NewConsoleLogger().(*consoleLogger)
	logger.SetLogLevel(LevelError)

	output := captureOutput(func() {
		logger.Trace("This trace message should not be printed")
	})
	assert.NotContains(t, output, "This trace message should not be printed")

	output = captureOutput(func() {
		logger.Debug("This debug message should not be printed")
	})
	assert.NotContains(t, output, "This debug message should not be printed")

	output = captureOutput(func() {
		logger.Info("This info message should not be printed")
	})
	assert.NotContains(t, output, "This info message should not be printed")

	output = captureOutput(func() {
		logger.Warn("This warn message should not be printed")
	})
	assert.NotContains(t, output, "This warn message should not be printed")

	output = captureOutput(func() {
		logger.Error("This is an error message")
	})
	assert.Contains(t, output, "This is an error message")
}
