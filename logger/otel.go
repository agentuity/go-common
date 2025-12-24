package logger

import (
	"context"
	"fmt"
	"os"
	"time"

	otelLog "go.opentelemetry.io/otel/log"
)

// otelLogger implements the Logger interface for OpenTelemetry
type otelLogger struct {
	metadata   map[string]otelLog.Value
	logLevel   LogLevel
	otelLogger otelLog.Logger
	context    context.Context
	child      Logger
}

var _ Logger = (*otelLogger)(nil)

// WithContext returns a new logger instance with the given context
func (o *otelLogger) WithContext(ctx context.Context) Logger {
	clone := o.clone()
	clone.context = ctx
	if clone.child != nil {
		clone.child = clone.child.WithContext(ctx)
	}
	return clone
}

// WithPrefix is a no-op for otelLogger
func (o *otelLogger) WithPrefix(prefix string) Logger {
	clone := o.clone()
	if clone.child != nil {
		clone.child = clone.child.WithPrefix(prefix)
	}
	return clone
}

// clone creates a copy of the logger with the given metadata
func (o *otelLogger) clone() *otelLogger {
	metadata := make(map[string]otelLog.Value)
	for k, v := range o.metadata {
		metadata[k] = v
	}
	return &otelLogger{
		metadata:   metadata,
		logLevel:   o.logLevel,
		otelLogger: o.otelLogger,
		child:      o.child,
		context:    o.context,
	}
}

func toLogValue(unknown interface{}) otelLog.Value {
	switch v := unknown.(type) {
	case string:
		return otelLog.StringValue(v)
	case int:
		return otelLog.IntValue(v)
	case int32:
		return otelLog.IntValue(int(v))
	case int64:
		return otelLog.Int64Value(v)
	case bool:
		return otelLog.BoolValue(v)
	case float32:
		return otelLog.Float64Value(float64(v))
	case float64:
		return otelLog.Float64Value(v)
	case []byte:
		return otelLog.BytesValue(v)
	case []interface{}:
		var values []otelLog.Value
		for _, arrayItem := range v {
			values = append(values, toLogValue(arrayItem))
		}
		return otelLog.SliceValue(values...)
	case map[string]interface{}:
		var values []otelLog.KeyValue
		for mapKey, mapUnknownValue := range v {
			mapValue := toLogValue(mapUnknownValue)
			kv := otelLog.KeyValue{
				Key:   mapKey,
				Value: mapValue,
			}
			values = append(values, kv)
		}
		return otelLog.MapValue(values...)
	default:
		return otelLog.StringValue(fmt.Sprintf("%v", v))
	}
}

// With will return a new logger using metadata as the base context
func (o *otelLogger) With(metadata map[string]interface{}) Logger {
	clone := o.clone()
	for k, v := range metadata {
		clone.metadata[k] = toLogValue(v)
	}
	if clone.child != nil {
		clone.child = clone.child.With(metadata)
	}
	return clone
}

// log handles the actual logging to OpenTelemetry
func (o *otelLogger) log(level LogLevel, severity otelLog.Severity, msg string, args ...interface{}) {
	if level < o.logLevel {
		return
	}

	formattedMsg := fmt.Sprintf(msg, args...)

	now := time.Now()

	record := otelLog.Record{}
	record.SetBody(otelLog.StringValue(formattedMsg))
	record.SetSeverity(severity)
	record.SetSeverityText(severity.String())
	record.SetObservedTimestamp(now)
	record.SetTimestamp(now)
	for k, v := range o.metadata {
		record.AddAttributes(otelLog.KeyValue{
			Key:   k,
			Value: v,
		})
	}

	ctx := context.Background()
	if o.context != nil {
		ctx = o.context
	}
	o.otelLogger.Emit(ctx, record)
}

// Trace level logging
func (o *otelLogger) Trace(msg string, args ...interface{}) {
	o.log(LevelTrace, otelLog.SeverityTrace, msg, args...)
	if o.child != nil {
		o.child.Trace(msg, args...)
	}
}

// Debug level logging
func (o *otelLogger) Debug(msg string, args ...interface{}) {
	o.log(LevelDebug, otelLog.SeverityDebug, msg, args...)
	if o.child != nil {
		o.child.Debug(msg, args...)
	}
}

// Info level logging
func (o *otelLogger) Info(msg string, args ...interface{}) {
	o.log(LevelInfo, otelLog.SeverityInfo, msg, args...)
	if o.child != nil {
		o.child.Info(msg, args...)
	}
}

// Warning level logging
func (o *otelLogger) Warn(msg string, args ...interface{}) {
	o.log(LevelWarn, otelLog.SeverityWarn, msg, args...)
	if o.child != nil {
		o.child.Warn(msg, args...)
	}
}

// Error level logging
func (o *otelLogger) Error(msg string, args ...interface{}) {
	o.log(LevelError, otelLog.SeverityError, msg, args...)
	if o.child != nil {
		o.child.Error(msg, args...)
	}
}

// Fatal level logging and exit with code 1
func (o *otelLogger) Fatal(msg string, args ...interface{}) {
	o.log(LevelError, otelLog.SeverityError, msg, args...)
	if o.child != nil {
		o.child.Error(msg, args...)
	}
	os.Exit(1)
}

func (o *otelLogger) Stack(next Logger) Logger {
	clone := o.clone()
	clone.child = next
	return clone
}

func (o *otelLogger) IsLevelEnabled(level LogLevel) bool {
	return level >= o.logLevel
}

func (o *otelLogger) IsTraceEnabled() bool {
	return o.IsLevelEnabled(LevelTrace)
}

func (o *otelLogger) IsDebugEnabled() bool {
	return o.IsLevelEnabled(LevelDebug)
}

func (o *otelLogger) IsInfoEnabled() bool {
	return o.IsLevelEnabled(LevelInfo)
}

func (o *otelLogger) IsWarnEnabled() bool {
	return o.IsLevelEnabled(LevelWarn)
}

func (o *otelLogger) IsErrorEnabled() bool {
	return o.IsLevelEnabled(LevelError)
}

func NewOtelLogger(otelsLogger otelLog.Logger, levels LogLevel) Logger {
	return &otelLogger{
		otelLogger: otelsLogger,
		logLevel:   LevelTrace,
	}
}
