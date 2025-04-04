package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agentuity/go-common/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

const (
	mockSharedSecret = "MOCK_SHARED_SECRET_FOR_TESTING"
	mockAPIKey       = "MOCK_API_KEY_FOR_TESTING"
	mockServiceName  = "mock-service"
)

func TestGenerateOTLPBearerTokenError(t *testing.T) {
	pastTime := time.Now().Add(-1 * time.Hour)
	token, err := GenerateOTLPBearerTokenWithExpiration(mockSharedSecret, pastTime)
	assert.Error(t, err)
	assert.Empty(t, token)
	assert.Contains(t, err.Error(), "expiration time is in the past")
}

func TestNew(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx := context.Background()

	ctx2, log, shutdown, err := New(ctx, mockServiceName, mockSharedSecret, server.URL, nil)
	require.NoError(t, err)
	require.NotNil(t, ctx2)
	require.NotNil(t, log)
	require.NotNil(t, shutdown)

	shutdown()

	consoleLogger := logger.NewTestLogger()
	ctx3, log2, shutdown2, err := New(ctx, mockServiceName, mockSharedSecret, server.URL, consoleLogger)
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

	ctx2, log, shutdown, err := NewWithAPIKey(ctx, mockServiceName, server.URL, mockAPIKey, nil)
	require.NoError(t, err)
	require.NotNil(t, ctx2)
	require.NotNil(t, log)
	require.NotNil(t, shutdown)

	shutdown()

	consoleLogger := logger.NewTestLogger()
	ctx3, log2, shutdown2, err := NewWithAPIKey(ctx, mockServiceName, server.URL, mockAPIKey, consoleLogger)
	require.NoError(t, err)
	require.NotNil(t, ctx3)
	require.NotNil(t, log2)
	require.NotNil(t, shutdown2)

	shutdown2()
}

func TestStartSpan(t *testing.T) {
	ctx := context.Background()
	log := logger.NewTestLogger()
	tracer := trace.NewNoopTracerProvider().Tracer("test")

	ctx2, log2, span := StartSpan(ctx, log, tracer, "test-span")
	require.NotNil(t, ctx2)
	require.NotNil(t, log2)
	require.NotNil(t, span)

	span.End()
}

func TestNewWithInvalidURL(t *testing.T) {
	ctx := context.Background()
	invalidURL := "://invalid-url"

	ctx2, log, shutdown, err := New(ctx, mockServiceName, mockSharedSecret, invalidURL, nil)
	assert.Error(t, err)
	assert.Nil(t, ctx2)
	assert.Nil(t, log)
	assert.Nil(t, shutdown)
	assert.Contains(t, err.Error(), "error parsing oltpServerURL")
}
