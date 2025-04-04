package logger

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetLevelFromEnv(t *testing.T) {
	originalValue := os.Getenv("AGENTUITY_LOG_LEVEL")
	defer os.Setenv("AGENTUITY_LOG_LEVEL", originalValue)

	tests := []struct {
		name          string
		envValue      string
		expectedLevel LogLevel
	}{
		{
			name:          "trace level",
			envValue:      "trace",
			expectedLevel: LevelTrace,
		},
		{
			name:          "debug level",
			envValue:      "debug",
			expectedLevel: LevelDebug,
		},
		{
			name:          "info level",
			envValue:      "info",
			expectedLevel: LevelInfo,
		},
		{
			name:          "warn level",
			envValue:      "warn",
			expectedLevel: LevelWarn,
		},
		{
			name:          "error level",
			envValue:      "error",
			expectedLevel: LevelError,
		},
		{
			name:          "uppercase trace",
			envValue:      "TRACE",
			expectedLevel: LevelTrace,
		},
		{
			name:          "mixed case debug",
			envValue:      "DeBuG",
			expectedLevel: LevelDebug,
		},
		{
			name:          "empty string",
			envValue:      "",
			expectedLevel: LevelDebug, // Default value
		},
		{
			name:          "invalid value",
			envValue:      "invalid",
			expectedLevel: LevelDebug, // Default value
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("AGENTUITY_LOG_LEVEL", tt.envValue)
			
			level := GetLevelFromEnv()
			
			assert.Equal(t, tt.expectedLevel, level)
		})
	}
}

func TestLogLevelConstants(t *testing.T) {
	assert.Equal(t, LogLevel(0), LevelTrace)
	assert.Equal(t, LogLevel(1), LevelDebug)
	assert.Equal(t, LogLevel(2), LevelInfo)
	assert.Equal(t, LogLevel(3), LevelWarn)
	assert.Equal(t, LogLevel(4), LevelError)
	assert.Equal(t, LogLevel(5), LevelNone)
}
