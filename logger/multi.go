package logger

type muxLogger struct {
	loggers []Logger
}

func (m *muxLogger) With(metadata map[string]interface{}) Logger {
	var newLoggers []Logger
	for _, l := range m.loggers {
		newLoggers = append(newLoggers, l.With(metadata))
	}
	return NewMultiLogger(newLoggers...)
}

func (m *muxLogger) WithPrefix(prefix string) Logger {
	var newLoggers []Logger
	for _, l := range m.loggers {
		newLoggers = append(newLoggers, l.WithPrefix(prefix))
	}
	return NewMultiLogger(newLoggers...)
}

func (m *muxLogger) Trace(msg string, args ...interface{}) {
	m.each(func(l Logger) { l.Trace(msg, args...) })
}

func (m *muxLogger) Debug(msg string, args ...interface{}) {
	m.each(func(l Logger) { l.Debug(msg, args...) })
}

func (m *muxLogger) Info(msg string, args ...interface{}) {
	m.each(func(l Logger) { l.Info(msg, args...) })
}

func (m *muxLogger) Warn(msg string, args ...interface{}) {
	m.each(func(l Logger) { l.Warn(msg, args...) })
}

func (m *muxLogger) Error(msg string, args ...interface{}) {
	m.each(func(l Logger) { l.Error(msg, args...) })
}

func (m *muxLogger) Fatal(msg string, args ...interface{}) {
	m.each(func(l Logger) { l.Fatal(msg, args...) })
}

func (m *muxLogger) each(f func(Logger)) {
	for _, l := range m.loggers {
		f(l)
	}
}

func NewMultiLogger(loggers ...Logger) Logger {
	return &muxLogger{
		loggers: loggers,
	}
}
