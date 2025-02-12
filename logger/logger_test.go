package logger

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type testSink struct {
	buf []byte
}

func (s *testSink) Write(buf []byte) (int, error) {
	s.buf = buf
	return len(buf), nil
}

func TestGCloudLogger(t *testing.T) {
	sink := &testSink{}
	log := NewGCloudLoggerWithSink(sink, LevelTrace)
	jlog := log.(*jsonLogger)
	tv := time.Date(2023, 10, 22, 12, 30, 0, 0, time.UTC)
	jlog.ts = &tv
	log.Trace("Hi")
	assert.Equal(t, `{"timestamp":"2023-10-22T12:30:00Z","message":"Hi","severity":"TRACE"}`, string(sink.buf))
	wlog := log.WithPrefix("[hi]")
	jlog = wlog.(*jsonLogger)
	jlog.ts = &tv
	wlog.Debug("hi")
	assert.Equal(t, `{"timestamp":"2023-10-22T12:30:00Z","message":"hi","severity":"DEBUG","component":"hi"}`, string(sink.buf))
	w2log := wlog.WithPrefix("[bye]")
	jlog = w2log.(*jsonLogger)
	jlog.ts = &tv
	w2log.Debug("hi")
	assert.Equal(t, `{"timestamp":"2023-10-22T12:30:00Z","message":"hi","severity":"DEBUG","component":"hi, bye"}`, string(sink.buf))
}

func TestCombinedLogger(t *testing.T) {
	sink := &testSink{}
	log := NewTestLogger()
	jsonLog := NewJSONLoggerWithSink(sink, LevelTrace)
	tv := time.Date(2023, 10, 22, 12, 30, 0, 0, time.UTC)
	jsonLog.(*jsonLogger).ts = &tv
	combined := NewMultiLogger(log, jsonLog)
	combined.Info("Ayyyyyy")
	assert.Len(t, log.Logs, 1)
	assert.Equal(t, `{"timestamp":"2023-10-22T12:30:00Z","message":"Ayyyyyy","severity":"INFO"}`, string(sink.buf))
}

func TestJSONLogger(t *testing.T) {

	logger := NewJSONLogger().(*jsonLogger)

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

func TestJSONLoggerWithEnvLevel(t *testing.T) {

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
		logger := NewJSONLogger().(*jsonLogger)

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
