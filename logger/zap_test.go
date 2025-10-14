package logger

import (
	"testing"

	"go.uber.org/zap"
)

func TestToZap(t *testing.T) {
	baseLogger := NewTestLogger()
	zapLogger := ToZap(baseLogger)

	if zapLogger == nil {
		t.Fatal("expected non-nil zap logger")
	}

	zapLogger.Info("test message", zap.String("key", "value"))
	zapLogger.Debug("debug message", zap.Int("count", 42))
	zapLogger.Warn("warning message")
	zapLogger.Error("error message", zap.Bool("flag", true))
}

func TestZapBridgeWith(t *testing.T) {
	baseLogger := NewTestLogger()
	zapLogger := ToZap(baseLogger)

	childLogger := zapLogger.With(zap.String("component", "test"))
	childLogger.Info("child logger message")
}
