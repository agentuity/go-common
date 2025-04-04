package logger

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJSONLogEntryString(t *testing.T) {
	entry := JSONLogEntry{
		Message: "Test message",
	}
	result := entry.String()
	var parsed map[string]interface{}
	err := json.Unmarshal([]byte(result), &parsed)
	assert.NoError(t, err)
	assert.Equal(t, "Test message", parsed["message"])
	assert.Equal(t, "INFO", parsed["severity"]) // Default severity

	entry = JSONLogEntry{
		Message:  "Test message",
		Severity: "DEBUG",
	}
	result = entry.String()
	err = json.Unmarshal([]byte(result), &parsed)
	assert.NoError(t, err)
	assert.Equal(t, "Test message", parsed["message"])
	assert.Equal(t, "DEBUG", parsed["severity"])

	entry = JSONLogEntry{
		Message:  "Test message",
		Severity: "ERROR",
		Metadata: map[string]interface{}{
			"key1": "value1",
			"key2": 42,
		},
	}
	result = entry.String()
	err = json.Unmarshal([]byte(result), &parsed)
	assert.NoError(t, err)
	assert.Equal(t, "Test message", parsed["message"])
	assert.Equal(t, "ERROR", parsed["severity"])
	metadata := parsed["metadata"].(map[string]interface{})
	assert.Equal(t, "value1", metadata["key1"])
	assert.Equal(t, float64(42), metadata["key2"]) // JSON numbers are float64
}


func TestJSONLoggerTokenize(t *testing.T) {
	
	logger := NewJSONLogger().WithPrefix("[component]")
	
	sink := &testSink{}
	
	jsonLog, ok := logger.(SinkLogger)
	assert.True(t, ok)
	jsonLog.SetSink(sink, LevelTrace)
	
	logger.Info("Test message")
	
	var parsed map[string]interface{}
	err := json.Unmarshal(sink.buf, &parsed)
	assert.NoError(t, err)
	
	assert.Equal(t, "component", parsed["component"])
}

func TestJSONLoggerWithPrefix(t *testing.T) {
	logger := NewJSONLogger()
	withPrefix := logger.WithPrefix("test")
	
	sink := &testSink{}
	
	jsonLog, ok := withPrefix.(SinkLogger)
	assert.True(t, ok)
	jsonLog.SetSink(sink, LevelTrace)
	
	withPrefix.Info("Test message")
	
	var parsed map[string]interface{}
	err := json.Unmarshal(sink.buf, &parsed)
	assert.NoError(t, err)
	
	assert.Equal(t, "test", parsed["component"])
	
	withPrefix2 := withPrefix.WithPrefix("another")
	sink.buf = nil
	
	jsonLog2, ok := withPrefix2.(SinkLogger)
	assert.True(t, ok)
	jsonLog2.SetSink(sink, LevelTrace)
	
	withPrefix2.Info("Test message")
	
	err = json.Unmarshal(sink.buf, &parsed)
	assert.NoError(t, err)
	
	assert.Equal(t, "test another", parsed["component"])
	
	withPrefix3 := withPrefix2.WithPrefix("another")
	sink.buf = nil
	
	jsonLog3, ok := withPrefix3.(SinkLogger)
	assert.True(t, ok)
	jsonLog3.SetSink(sink, LevelTrace)
	
	withPrefix3.Info("Test message")
	
	err = json.Unmarshal(sink.buf, &parsed)
	assert.NoError(t, err)
	
	assert.Equal(t, "test another", parsed["component"]) // Should not add duplicate
}

func TestJSONLoggerWith(t *testing.T) {
	logger := NewJSONLogger()
	withTrace := logger.With(map[string]interface{}{
		"trace": "trace-id",
	})
	
	sink := &testSink{}
	
	jsonLog, ok := withTrace.(SinkLogger)
	assert.True(t, ok)
	jsonLog.SetSink(sink, LevelTrace)
	
	withTrace.Info("Test message")
	
	var parsed map[string]interface{}
	err := json.Unmarshal(sink.buf, &parsed)
	assert.NoError(t, err)
	
	assert.Equal(t, "trace-id", parsed["logging.googleapis.com/trace"])
	
	withComponent := logger.With(map[string]interface{}{
		"component": "component-name",
	})
	sink.buf = nil
	
	jsonLog2, ok := withComponent.(SinkLogger)
	assert.True(t, ok)
	jsonLog2.SetSink(sink, LevelTrace)
	
	withComponent.Info("Test message")
	
	err = json.Unmarshal(sink.buf, &parsed)
	assert.NoError(t, err)
	
	assert.Equal(t, "component-name", parsed["component"])
	
	withMetadata := logger.With(map[string]interface{}{
		"key1": "value1",
		"key2": 42,
	})
	sink.buf = nil
	
	jsonLog3, ok := withMetadata.(SinkLogger)
	assert.True(t, ok)
	jsonLog3.SetSink(sink, LevelTrace)
	
	withMetadata.Info("Test message")
	
	err = json.Unmarshal(sink.buf, &parsed)
	assert.NoError(t, err)
	
	metadata := parsed["metadata"].(map[string]interface{})
	assert.Equal(t, "value1", metadata["key1"])
	assert.Equal(t, float64(42), metadata["key2"]) // JSON numbers are float64
}

func TestJSONLoggerLog(t *testing.T) {
	sink := &testSink{}
	
	logger := NewJSONLoggerWithSink(sink, LevelDebug)
	
	logger.Info("Test message")
	
	var parsed map[string]interface{}
	err := json.Unmarshal(sink.buf, &parsed)
	assert.NoError(t, err)
	assert.Equal(t, "Test message", parsed["message"])
	assert.Equal(t, "INFO", parsed["severity"])
	
	sink.buf = nil
	logger.Warn("Test %s %d", "message", 42)
	
	err = json.Unmarshal(sink.buf, &parsed)
	assert.NoError(t, err)
	assert.Equal(t, "Test message 42", parsed["message"])
	assert.Equal(t, "WARNING", parsed["severity"])
	
	sink.buf = nil
	loggerWithMetadata := logger.With(map[string]interface{}{
		"key1": "value1",
		"key2": 42,
	})
	loggerWithMetadata.Error("Test message")
	
	err = json.Unmarshal(sink.buf, &parsed)
	assert.NoError(t, err)
	assert.Equal(t, "Test message", parsed["message"])
	assert.Equal(t, "ERROR", parsed["severity"])
	metadata := parsed["metadata"].(map[string]interface{})
	assert.Equal(t, "value1", metadata["key1"])
	assert.Equal(t, float64(42), metadata["key2"])
	
	sink.buf = nil
	loggerWithComponent := logger.WithPrefix("test-component")
	loggerWithComponent.Debug("Test message")
	
	err = json.Unmarshal(sink.buf, &parsed)
	assert.NoError(t, err)
	assert.Equal(t, "Test message", parsed["message"])
	assert.Equal(t, "DEBUG", parsed["severity"])
	assert.Equal(t, "test-component", parsed["component"])
	
	sink.buf = nil
	loggerWithTrace := logger.With(map[string]interface{}{
		"trace": "trace-id",
	})
	loggerWithTrace.Trace("Test message")
	
	err = json.Unmarshal(sink.buf, &parsed)
	assert.NoError(t, err)
	assert.Equal(t, "Test message", parsed["message"])
	assert.Equal(t, "TRACE", parsed["severity"])
	assert.Equal(t, "trace-id", parsed["logging.googleapis.com/trace"])
}

func TestJSONLoggerLogLevelFiltering(t *testing.T) {
	sink := &testSink{}
	
	logger := NewJSONLoggerWithSink(sink, LevelInfo)
	
	logger.Debug("Debug message")
	assert.Nil(t, sink.buf, "Debug message should be filtered out")
	
	logger.Info("Info message")
	assert.NotNil(t, sink.buf, "Info message should be logged")
	sink.buf = nil
	
	logger.Error("Error message")
	assert.NotNil(t, sink.buf, "Error message should be logged")
}

func TestNewJSONLogger(t *testing.T) {
	originalValue := os.Getenv("AGENTUITY_LOG_LEVEL")
	defer os.Setenv("AGENTUITY_LOG_LEVEL", originalValue)

	os.Setenv("AGENTUITY_LOG_LEVEL", "")
	logger := NewJSONLogger()
	assert.NotNil(t, logger)
	
	logger = NewJSONLogger(LevelError)
	assert.NotNil(t, logger)
	
	sink := &testSink{}
	jsonLog, ok := logger.(SinkLogger)
	assert.True(t, ok)
	jsonLog.SetSink(sink, LevelTrace)
	
	logger.Debug("Debug message")
	assert.Nil(t, sink.buf, "Debug message should be filtered out")
	
	logger.Error("Error message")
	assert.NotNil(t, sink.buf, "Error message should be logged")
	
	os.Setenv("AGENTUITY_LOG_LEVEL", "warn")
	logger = NewJSONLogger()
	assert.NotNil(t, logger)
	
	sink = &testSink{}
	jsonLog, ok = logger.(SinkLogger)
	assert.True(t, ok)
	jsonLog.SetSink(sink, LevelTrace)
	
	logger.Debug("Debug message")
	assert.Nil(t, sink.buf, "Debug message should be filtered out")
	
	logger.Warn("Warn message")
	assert.NotNil(t, sink.buf, "Warn message should be logged")
}

func TestNewJSONLoggerWithSink(t *testing.T) {
	sink := &testSink{}
	logger := NewJSONLoggerWithSink(sink, LevelDebug)
	assert.NotNil(t, logger)
	
	logger.Info("Test message")
	
	var parsed map[string]interface{}
	err := json.Unmarshal(sink.buf, &parsed)
	assert.NoError(t, err)
	assert.Equal(t, "Test message", parsed["message"])
	assert.Equal(t, "INFO", parsed["severity"])
}
