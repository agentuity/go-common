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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
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
