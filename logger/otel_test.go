package logger

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/noop"
)

func TestOtelLoggerWithMergesMetadata(t *testing.T) {
	noopLogger := noop.NewLoggerProvider().Logger("test")

	logger := &otelLogger{
		metadata:   make(map[string]log.Value),
		logLevel:   LevelTrace,
		otelLogger: noopLogger,
		context:    context.Background(),
	}

	baseLogger := logger.With(map[string]interface{}{
		"base_key": "base_value",
		"shared":   "from_base",
	}).(*otelLogger)

	extendedLogger := baseLogger.With(map[string]interface{}{
		"extra_key": "extra_value",
		"shared":    "from_extended",
	}).(*otelLogger)

	assert.Equal(t, 3, len(extendedLogger.metadata))
	assert.Equal(t, "base_value", extendedLogger.metadata["base_key"].AsString())
	assert.Equal(t, "extra_value", extendedLogger.metadata["extra_key"].AsString())
	assert.Equal(t, "from_extended", extendedLogger.metadata["shared"].AsString())
}
