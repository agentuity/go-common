package logger

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel/log"
)

// otelLogger implements the Logger interface for OpenTelemetry
type otelLogger struct {
	prefixes   []string
	metadata   map[string]log.Value
	logLevel   LogLevel
	otelLogger log.Logger
}

var _ Logger = (*otelLogger)(nil)

// WithPrefix will return a new logger with a prefix prepended to the message
func (o *otelLogger) WithPrefix(prefix string) Logger {
	// help!

	return o
}

// clone creates a copy of the logger with the given metadata
func (o *otelLogger) clone(kv map[string]log.Value) *otelLogger {
	prefixes := make([]string, 0)
	prefixes = append(prefixes, o.prefixes...)
	return &otelLogger{
		prefixes:   prefixes,
		metadata:   kv,
		logLevel:   o.logLevel,
		otelLogger: o.otelLogger,
	}
}

func toLogValue(unknown interface{}) log.Value {
	switch v := unknown.(type) {
	case string:
		return log.StringValue(v)
	case int:
		return log.IntValue(v)
	case bool:
		return log.BoolValue(v)
	case float64:
		return log.Float64Value(v)
	case []byte:
		return log.BytesValue(v)
	case []interface{}:
		var values []log.Value
		for _, arrayItem := range v {
			values = append(values, toLogValue(arrayItem))
		}
		return log.SliceValue(values...)
	case map[string]interface{}:
		var values []log.KeyValue
		for mapKey, mapUnknownValue := range v {
			mapValue := toLogValue(mapUnknownValue)
			kv := log.KeyValue{
				Key:   mapKey,
				Value: mapValue,
			}
			values = append(values, kv)
		}
		return log.MapValue(values...)
	default:
		return log.StringValue(fmt.Sprintf("%v", v))
	}
}

// With will return a new logger using metadata as the base context
func (o *otelLogger) With(metadata map[string]interface{}) Logger {
	kv := make(map[string]log.Value)
	if o.metadata != nil {
		for k, v := range o.metadata {
			kv[k] = v
		}
	}
	for k, v := range metadata {
		kv[k] = toLogValue(v)
	}
	if len(kv) == 0 {
		kv = nil
	}
	return o.clone(kv)
}

// log handles the actual logging to OpenTelemetry
func (o *otelLogger) log(level LogLevel, severity log.Severity, msg string, args ...interface{}) {
	if level < o.logLevel {
		return
	}

	formattedMsg := fmt.Sprintf(msg, args...)

	// Add prefixes if any
	if len(o.prefixes) > 0 {
		formattedMsg = strings.Join(o.prefixes, " ") + " " + formattedMsg
	}

	now := time.Now()

	record := log.Record{}
	record.SetBody(log.StringValue(formattedMsg))
	record.SetSeverity(severity)
	record.SetSeverityText(severity.String())
	record.SetObservedTimestamp(now)
	record.SetTimestamp(now)
	for k, v := range o.metadata {
		record.AddAttributes(log.KeyValue{
			Key:   k,
			Value: v,
		})
	}

	o.otelLogger.Emit(context.Background(), record)
}

// Trace level logging
func (o *otelLogger) Trace(msg string, args ...interface{}) {
	o.log(LevelTrace, log.SeverityTrace, msg, args...)
}

// Debug level logging
func (o *otelLogger) Debug(msg string, args ...interface{}) {
	o.log(LevelDebug, log.SeverityDebug, msg, args...)
}

// Info level logging
func (o *otelLogger) Info(msg string, args ...interface{}) {
	o.log(LevelInfo, log.SeverityInfo, msg, args...)
}

// Warning level logging
func (o *otelLogger) Warn(msg string, args ...interface{}) {
	o.log(LevelWarn, log.SeverityWarn, msg, args...)
}

// Error level logging
func (o *otelLogger) Error(msg string, args ...interface{}) {
	o.log(LevelError, log.SeverityError, msg, args...)
}

// Fatal level logging and exit with code 1
func (o *otelLogger) Fatal(msg string, args ...interface{}) {
	o.log(LevelError, log.SeverityError, msg, args...)
	os.Exit(1)
}

func NewOtelLogger(otelsLogger log.Logger, levels LogLevel) Logger {

	return &otelLogger{
		otelLogger: otelsLogger,
		logLevel:   LevelTrace,
	}
}
