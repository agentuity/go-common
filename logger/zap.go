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
	for _, field := range fields {
		metadata[field.Key] = field.Interface
	}
	return &zapBridge{logger: z.logger.With(metadata)}
}

func (z *zapBridge) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	return ce.AddCore(entry, z)
}

func (z *zapBridge) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	args := make([]interface{}, 0, len(fields)*2)
	for _, field := range fields {
		args = append(args, field.Key, field.Interface)
	}

	switch entry.Level {
	case zapcore.DebugLevel:
		z.logger.Debug(entry.Message, args...)
	case zapcore.InfoLevel:
		z.logger.Info(entry.Message, args...)
	case zapcore.WarnLevel:
		z.logger.Warn(entry.Message, args...)
	case zapcore.ErrorLevel, zapcore.DPanicLevel, zapcore.PanicLevel, zapcore.FatalLevel:
		z.logger.Error(entry.Message, args...)
	default:
		z.logger.Trace(entry.Message, args...)
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
