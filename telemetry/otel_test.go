package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/agentuity/go-common/logger"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func getTestValues() (string, string, string) {
	serviceName := os.Getenv("TEST_SERVICE_NAME")
	if serviceName == "" {
		serviceName = "test-service"
	}

	secretValue := os.Getenv("TEST_SECRET_VALUE")
	if secretValue == "" {
		secretValue = "placeholder-for-testing"
	}

	apiKey := os.Getenv("TEST_API_KEY")
	if apiKey == "" {
		apiKey = "placeholder-for-testing"
	}

	return serviceName, secretValue, apiKey
}

func TestNew(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx := context.Background()
	serviceName, secretValue, _ := getTestValues()

	ctx2, log, shutdown, err := New(ctx, serviceName, secretValue, server.URL, nil)
	require.NoError(t, err)
	require.NotNil(t, ctx2)
	require.NotNil(t, log)
	require.NotNil(t, shutdown)

	shutdown()

	consoleLogger := logger.NewTestLogger()
	ctx3, log2, shutdown2, err := New(ctx, serviceName, secretValue, server.URL, consoleLogger)
	require.NoError(t, err)
	require.NotNil(t, ctx3)
	require.NotNil(t, log2)
	require.NotNil(t, shutdown2)

	shutdown2()
}

func TestNewWithAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx := context.Background()
	serviceName, _, apiKey := getTestValues()

	ctx2, log, shutdown, err := NewWithAPIKey(ctx, serviceName, server.URL, apiKey, nil)
	require.NoError(t, err)
	require.NotNil(t, ctx2)
	require.NotNil(t, log)
	require.NotNil(t, shutdown)

	shutdown()

	consoleLogger := logger.NewTestLogger()
	ctx3, log2, shutdown2, err := NewWithAPIKey(ctx, serviceName, server.URL, apiKey, consoleLogger)
	require.NoError(t, err)
	require.NotNil(t, ctx3)
	require.NotNil(t, log2)
	require.NotNil(t, shutdown2)

	shutdown2()
}

func TestNewWithInvalidURL(t *testing.T) {
	ctx := context.Background()
	invalidURL := "://invalid-url"
	serviceName, secretValue, _ := getTestValues()

	ctx2, log, shutdown, err := New(ctx, serviceName, secretValue, invalidURL, nil)
	assert.Error(t, err)
	assert.Nil(t, ctx2)
	assert.Nil(t, log)
	assert.Nil(t, shutdown)
	assert.Contains(t, err.Error(), "error parsing oltpServerURL")
}

func TestShutdownTelemetryComponentUsesIndependentTimeouts(t *testing.T) {
	const timeout = 25 * time.Millisecond

	firstStarted := make(chan struct{})
	firstDone := make(chan struct{})
	shutdownTelemetryComponent(timeout, func(ctx context.Context) error {
		close(firstStarted)
		<-ctx.Done()
		close(firstDone)
		return ctx.Err()
	})

	select {
	case <-firstStarted:
	default:
		t.Fatal("expected first shutdown function to start")
	}
	select {
	case <-firstDone:
	default:
		t.Fatal("expected first shutdown function to stop at its own timeout")
	}

	secondCalled := false
	shutdownTelemetryComponent(timeout, func(ctx context.Context) error {
		secondCalled = true
		return nil
	})
	if !secondCalled {
		t.Fatal("expected later shutdown functions to get their own timeout budget")
	}
}

func TestTelemetrySendsLogsTracesAndMetrics(t *testing.T) {
	var mu sync.Mutex
	hits := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmp := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx, log, shutdown, err := NewWithAPIKey(
		ctx,
		"test-service",
		server.URL,
		"api-key",
		nil,
		WithLogBatchPath(filepath.Join(tmp, "logs.db")),
		WithMetricBatchPath(filepath.Join(tmp, "metrics.db")),
		WithTraceBatchPath(filepath.Join(tmp, "traces.db")),
		WithLogBatchIdleTimeout(10*time.Millisecond),
	)
	var shutdownOnce sync.Once
	t.Cleanup(func() {
		if shutdown != nil {
			shutdownOnce.Do(shutdown)
		}
	})
	require.NoError(t, err)
	require.NotNil(t, ctx)
	require.NotNil(t, log)

	log.Info("hello")

	tracer := otel.Tracer("telemetry-test")
	spanCtx, span := tracer.Start(ctx, "span")
	span.End()

	counter, err := otel.Meter("telemetry-test").Int64Counter("test.counter")
	require.NoError(t, err)
	counter.Add(spanCtx, 1, metric.WithAttributes())

	shutdownOnce.Do(shutdown)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return hits["/v1/logs"] > 0 && hits["/v1/traces"] > 0 && hits["/v1/metrics"] > 0
	}, 2*time.Second, 10*time.Millisecond)
}

func TestNewLogForwarderWithAPIKeySendsOnlyLogs(t *testing.T) {
	var mu sync.Mutex
	hits := map[string]int{}
	authHeaders := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		authHeaders[r.URL.Path] = r.Header.Get("Authorization")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmp := t.TempDir()
	ctx := context.Background()
	log, shutdown, err := NewLogForwarderWithAPIKey(
		ctx,
		"container-log-forwarder",
		server.URL,
		"sk_test",
		nil,
		WithLogBatchPath(filepath.Join(tmp, "container-1-logs.db")),
		WithLogBatchIdleTimeout(10*time.Millisecond),
		WithResourceAttributes(
			attribute.String("@agentuity/orgId", "org_123"),
			attribute.String("@agentuity/projectId", "proj_123"),
			attribute.String("@agentuity/deploymentId", "dep_123"),
		),
	)
	require.NoError(t, err)
	require.NotNil(t, log)
	require.NotNil(t, shutdown)

	log.Info("hello from container")
	shutdown()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return hits["/v1/logs"] > 0
	}, 2*time.Second, 10*time.Millisecond)

	mu.Lock()
	assert.Equal(t, "Bearer sk_test", authHeaders["/v1/logs"])
	mu.Unlock()

	require.Never(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return hits["/v1/traces"] > 0 || hits["/v1/metrics"] > 0
	}, 100*time.Millisecond, 10*time.Millisecond)
}

func TestSlowTelemetryExporterDoesNotBlockInstrumentedRedisPing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(250 * time.Millisecond)
		http.Error(w, "tableflip old process still exiting", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	mr := miniredis.RunT(t)

	for _, tt := range []struct {
		name       string
		seedReplay bool
	}{
		{name: "empty queues"},
		{name: "active replay", seedReplay: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			ctx, log, shutdown, err := NewWithAPIKey(
				ctx,
				"test-service",
				server.URL,
				"api-key",
				nil,
				WithTimeout(50*time.Millisecond),
				WithShutdownTimeout(50*time.Millisecond),
				WithLogBatchPath(filepath.Join(tmp, "logs.db")),
				WithMetricBatchPath(filepath.Join(tmp, "metrics.db")),
				WithTraceBatchPath(filepath.Join(tmp, "traces.db")),
				WithLogBatchIdleTimeout(10*time.Millisecond),
			)
			require.NoError(t, err)
			require.NotNil(t, ctx)
			require.NotNil(t, log)
			t.Cleanup(shutdown)

			if tt.seedReplay {
				log.Info("seed log replay")

				spanCtx, span := otel.Tracer("telemetry-redis-repro").Start(ctx, "seed-span")
				span.End()

				counter, err := otel.Meter("telemetry-redis-repro").Int64Counter("seed.counter")
				require.NoError(t, err)
				counter.Add(spanCtx, 1)
			}

			client := redis.NewClient(&redis.Options{
				Addr:         mr.Addr(),
				DialTimeout:  50 * time.Millisecond,
				ReadTimeout:  50 * time.Millisecond,
				WriteTimeout: 50 * time.Millisecond,
				PoolTimeout:  50 * time.Millisecond,
			})
			t.Cleanup(func() {
				require.NoError(t, client.Close())
			})

			require.NoError(t, redisotel.InstrumentTracing(client))
			require.NoError(t, redisotel.InstrumentMetrics(client))

			pingCtx, pingCancel := context.WithTimeout(context.Background(), time.Second)
			defer pingCancel()

			start := time.Now()
			require.NoError(t, client.Ping(pingCtx).Err())
			require.Less(t, time.Since(start), 250*time.Millisecond)
		})
	}
}

type blockingSpanExporter struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (e *blockingSpanExporter) ExportSpans(ctx context.Context, _ []sdktrace.ReadOnlySpan) error {
	e.once.Do(func() {
		close(e.started)
	})
	select {
	case <-e.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *blockingSpanExporter) Shutdown(context.Context) error {
	return nil
}

func TestBatchSpanProcessorDoesNotBlockWhenDurableTraceQueueIsBusy(t *testing.T) {
	exporter := &blockingSpanExporter{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	durable, err := newDurableTraceExporter(context.Background(), exporter, durableTraceConfig{
		path: filepath.Join(t.TempDir(), "traces.db"),
	})
	require.NoError(t, err)

	require.NoError(t, durable.ExportSpans(context.Background(), []sdktrace.ReadOnlySpan{testReadOnlySpan()}))
	require.Eventually(t, func() bool {
		select {
		case <-exporter.started:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)

	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(durable))

	start := time.Now()
	_, span := provider.Tracer("telemetry-test").Start(context.Background(), "span")
	span.End()
	require.Less(t, time.Since(start), 100*time.Millisecond)

	close(exporter.release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, provider.Shutdown(ctx))
}
