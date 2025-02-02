package logger

// NewGCloudLogger returns a new Logger instance which can be used for structured google cloud logging
func NewGCloudLogger() Logger {
	return NewJSONLogger()
}

// NewGCloudLoggerWithSink returns a new Logger instance using a sink and suppressing the console logging
func NewGCloudLoggerWithSink(sink Sink, level LogLevel) Logger {
	return NewJSONLoggerWithSink(sink, level)
}
