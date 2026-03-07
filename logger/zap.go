package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type zapBridge struct {
	logger Logger
}

func (z *zapBridge) Enabled(level zapcore.Level) bool {
	return true
}

func (z *zapBridge) With(fields []zapcore.Field) zapcore.Core {
	metadata := make(map[string]interface{})
	encoder := zapcore.NewMapObjectEncoder()
	for _, field := range fields {
		field.AddTo(encoder)
	}
	for key, value := range encoder.Fields {
		metadata[key] = value
	}
	return &zapBridge{logger: z.logger.With(metadata)}
}

func (z *zapBridge) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	return ce.AddCore(entry, z)
}

func (z *zapBridge) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	metadata := make(map[string]interface{})
	encoder := zapcore.NewMapObjectEncoder()
	for _, field := range fields {
		field.AddTo(encoder)
	}
	for key, value := range encoder.Fields {
		metadata[key] = value
	}

	logger := z.logger
	if len(metadata) > 0 {
		logger = logger.With(metadata)
	}

	switch entry.Level {
	case zapcore.DebugLevel:
		logger.Debug("%s", entry.Message)
	case zapcore.InfoLevel:
		logger.Info("%s", entry.Message)
	case zapcore.WarnLevel:
		logger.Warn("%s", entry.Message)
	case zapcore.ErrorLevel:
		logger.Error("%s", entry.Message)
	case zapcore.DPanicLevel:
		logger.Error("%s", entry.Message)
	case zapcore.PanicLevel:
		logger.Error("%s", entry.Message)
		panic(entry.Message)
	case zapcore.FatalLevel:
		logger.Fatal("%s", entry.Message)
	default:
		logger.Trace("%s", entry.Message)
	}

	return nil
}

func (z *zapBridge) Sync() error {
	return nil
}

// ToZap returns a zap.Logger instance that will output to the provided logger
func ToZap(logger Logger) *zap.Logger {
	core := &zapBridge{logger: logger}
	return zap.New(core)
}
