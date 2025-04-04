package logger

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithKV(t *testing.T) {
	t.Run("string value", func(t *testing.T) {
		testLogger := NewTestLogger()
		key := "testKey"
		value := "testValue"

		loggerWithKV := WithKV(testLogger, key, value)
		assert.NotNil(t, loggerWithKV)

		kvLogger, ok := loggerWithKV.(*TestLogger)
		assert.True(t, ok)
		assert.Equal(t, value, kvLogger.metadata[key])

		kvLogger.Info("Test message")

		assert.Equal(t, 1, len(kvLogger.Logs))
		assert.Equal(t, "INFO", kvLogger.Logs[0].Severity)
		assert.Equal(t, "Test message", kvLogger.Logs[0].Message)
	})

	t.Run("integer value", func(t *testing.T) {
		testLogger := NewTestLogger()
		key := "intKey"
		value := 42

		loggerWithKV := WithKV(testLogger, key, value)
		assert.NotNil(t, loggerWithKV)

		kvLogger, ok := loggerWithKV.(*TestLogger)
		assert.True(t, ok)
		assert.Equal(t, value, kvLogger.metadata[key])

		kvLogger.Info("Integer value")

		assert.Equal(t, 1, len(kvLogger.Logs))
		assert.Equal(t, "INFO", kvLogger.Logs[0].Severity)
		assert.Equal(t, "Integer value", kvLogger.Logs[0].Message)
	})

	t.Run("boolean value", func(t *testing.T) {
		testLogger := NewTestLogger()
		key := "boolKey"
		value := true

		loggerWithKV := WithKV(testLogger, key, value)
		assert.NotNil(t, loggerWithKV)

		kvLogger, ok := loggerWithKV.(*TestLogger)
		assert.True(t, ok)
		assert.Equal(t, value, kvLogger.metadata[key])

		kvLogger.Info("Boolean value")

		assert.Equal(t, 1, len(kvLogger.Logs))
		assert.Equal(t, "INFO", kvLogger.Logs[0].Severity)
		assert.Equal(t, "Boolean value", kvLogger.Logs[0].Message)
	})

	t.Run("multiple key-value pairs", func(t *testing.T) {
		testLogger := NewTestLogger()

		multiLogger := WithKV(WithKV(testLogger, "key1", "value1"), "key2", "value2")
		assert.NotNil(t, multiLogger)

		kvLogger, ok := multiLogger.(*TestLogger)
		assert.True(t, ok)
		assert.Equal(t, "value1", kvLogger.metadata["key1"])
		assert.Equal(t, "value2", kvLogger.metadata["key2"])

		kvLogger.Info("Multiple keys")

		assert.Equal(t, 1, len(kvLogger.Logs))
		assert.Equal(t, "INFO", kvLogger.Logs[0].Severity)
		assert.Equal(t, "Multiple keys", kvLogger.Logs[0].Message)
	})
}
