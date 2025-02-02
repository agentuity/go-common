package logger

func WithKV(logger Logger, key string, value any) Logger {
	return logger.With(map[string]interface{}{key: value})
}
