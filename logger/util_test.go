package logger

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithKV(t *testing.T) {
	testLogger := NewTestLogger()
	
	key := "testKey"
	value := "testValue"
	
	loggerWithKV := WithKV(testLogger, key, value)
	
	assert.NotNil(t, loggerWithKV)
	
	loggerWithKV.Info("Test message")
	
	assert.Len(t, testLogger.Logs, 1)
	assert.Equal(t, "INFO", testLogger.Logs[0].Severity)
	assert.Equal(t, "Test message", testLogger.Logs[0].Message)
	
	intValue := 42
	loggerWithInt := WithKV(testLogger, "intKey", intValue)
	loggerWithInt.Info("Integer value")
	
	boolValue := true
	loggerWithBool := WithKV(testLogger, "boolKey", boolValue)
	loggerWithBool.Info("Boolean value")
	
	multiLogger := WithKV(WithKV(testLogger, "key1", "value1"), "key2", "value2")
	multiLogger.Info("Multiple keys")
}
