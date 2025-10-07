package logger

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/embedded"
	"go.opentelemetry.io/otel/log/noop"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
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

	// Verify baseLogger.metadata stayed unchanged (immutability)
	assert.Equal(t, 2, len(baseLogger.metadata))
	assert.Equal(t, "base_value", baseLogger.metadata["base_key"].AsString())
	assert.Equal(t, "from_base", baseLogger.metadata["shared"].AsString())

	assert.Equal(t, 3, len(extendedLogger.metadata))
	assert.Equal(t, "base_value", extendedLogger.metadata["base_key"].AsString())
	assert.Equal(t, "extra_value", extendedLogger.metadata["extra_key"].AsString())
	assert.Equal(t, "from_extended", extendedLogger.metadata["shared"].AsString())
}

type mockOtelLogger struct {
	embedded.Logger
	mu       sync.Mutex
	contexts []context.Context
}

func (m *mockOtelLogger) Emit(ctx context.Context, record log.Record) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.contexts = append(m.contexts, ctx)
}

func (m *mockOtelLogger) Enabled(ctx context.Context, param log.EnabledParameters) bool {
	return true
}

func (m *mockOtelLogger) getContexts() []context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]context.Context{}, m.contexts...)
}

func TestOtelLoggerWithContextPreservesSpanContext(t *testing.T) {
	mockLogger := &mockOtelLogger{}

	tp := sdktrace.NewTracerProvider()
	tracer := tp.Tracer("test-tracer")

	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	logger := &otelLogger{
		metadata:   make(map[string]log.Value),
		logLevel:   LevelTrace,
		otelLogger: mockLogger,
		context:    ctx,
	}

	logger.Info("test message")

	contexts := mockLogger.getContexts()
	require.Len(t, contexts, 1)

	spanContext := trace.SpanContextFromContext(contexts[0])
	assert.True(t, spanContext.IsValid())
	assert.Equal(t, span.SpanContext().TraceID(), spanContext.TraceID())
	assert.Equal(t, span.SpanContext().SpanID(), spanContext.SpanID())
}

func TestOtelLoggerWithContextMultipleTimes(t *testing.T) {
	mockLogger := &mockOtelLogger{}

	tp := sdktrace.NewTracerProvider()
	tracer := tp.Tracer("test-tracer")

	ctx1, span1 := tracer.Start(context.Background(), "span-1")
	defer span1.End()

	ctx2, span2 := tracer.Start(ctx1, "span-2")
	defer span2.End()

	ctx3, span3 := tracer.Start(ctx2, "span-3")
	defer span3.End()

	baseLogger := &otelLogger{
		metadata:   make(map[string]log.Value),
		logLevel:   LevelTrace,
		otelLogger: mockLogger,
		context:    ctx1,
	}

	logger2 := baseLogger.WithContext(ctx2)
	logger3 := logger2.WithContext(ctx3)

	logger3.Info("test message")

	contexts := mockLogger.getContexts()
	require.Len(t, contexts, 1)

	spanContext := trace.SpanContextFromContext(contexts[0])
	assert.True(t, spanContext.IsValid())
	assert.Equal(t, span3.SpanContext().TraceID(), spanContext.TraceID())
	assert.Equal(t, span3.SpanContext().SpanID(), spanContext.SpanID())
}

func TestOtelLoggerWithContextChainedLoggers(t *testing.T) {
	mockLogger1 := &mockOtelLogger{}
	mockLogger2 := &mockOtelLogger{}

	tp := sdktrace.NewTracerProvider()
	tracer := tp.Tracer("test-tracer")

	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	childLogger := &otelLogger{
		metadata:   make(map[string]log.Value),
		logLevel:   LevelTrace,
		otelLogger: mockLogger2,
		context:    context.Background(),
	}

	parentLogger := &otelLogger{
		metadata:   make(map[string]log.Value),
		logLevel:   LevelTrace,
		otelLogger: mockLogger1,
		context:    context.Background(),
		child:      childLogger,
	}

	loggerWithCtx := parentLogger.WithContext(ctx)
	loggerWithCtx.Info("test message")

	contexts1 := mockLogger1.getContexts()
	require.Len(t, contexts1, 1)
	spanContext1 := trace.SpanContextFromContext(contexts1[0])
	assert.True(t, spanContext1.IsValid())
	assert.Equal(t, span.SpanContext().TraceID(), spanContext1.TraceID())

	contexts2 := mockLogger2.getContexts()
	require.Len(t, contexts2, 1)
	spanContext2 := trace.SpanContextFromContext(contexts2[0])
	assert.True(t, spanContext2.IsValid())
	assert.Equal(t, span.SpanContext().TraceID(), spanContext2.TraceID())
}

func TestOtelLoggerWithContextPreservesOriginalLogger(t *testing.T) {
	mockLogger := &mockOtelLogger{}

	tp := sdktrace.NewTracerProvider()
	tracer := tp.Tracer("test-tracer")

	ctx1, span1 := tracer.Start(context.Background(), "span-1")
	defer span1.End()

	ctx2, span2 := tracer.Start(ctx1, "span-2")
	defer span2.End()

	baseLogger := &otelLogger{
		metadata:   make(map[string]log.Value),
		logLevel:   LevelTrace,
		otelLogger: mockLogger,
		context:    ctx1,
	}

	newLogger := baseLogger.WithContext(ctx2)

	baseLogger.Info("original context")
	newLogger.Info("new context")

	contexts := mockLogger.getContexts()
	require.Len(t, contexts, 2)

	spanContext1 := trace.SpanContextFromContext(contexts[0])
	assert.Equal(t, span1.SpanContext().SpanID(), spanContext1.SpanID())

	spanContext2 := trace.SpanContextFromContext(contexts[1])
	assert.Equal(t, span2.SpanContext().SpanID(), spanContext2.SpanID())
}

func TestOtelLoggerWithContextAndMetadata(t *testing.T) {
	mockLogger := &mockOtelLogger{}

	tp := sdktrace.NewTracerProvider()
	tracer := tp.Tracer("test-tracer")

	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	baseLogger := &otelLogger{
		metadata:   make(map[string]log.Value),
		logLevel:   LevelTrace,
		otelLogger: mockLogger,
		context:    context.Background(),
	}

	loggerWithMetadata := baseLogger.With(map[string]interface{}{
		"key": "value",
	})

	loggerWithCtx := loggerWithMetadata.WithContext(ctx)
	loggerWithCtx.Info("test message")

	contexts := mockLogger.getContexts()
	require.Len(t, contexts, 1)

	spanContext := trace.SpanContextFromContext(contexts[0])
	assert.True(t, spanContext.IsValid())
	assert.Equal(t, span.SpanContext().TraceID(), spanContext.TraceID())
}
