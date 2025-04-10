package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/agentuity/go-common/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateOTLPBearerTokenWithExpirationInPast(t *testing.T) {
	sharedSecret := "test-secret"
	expiration := time.Now().Add(-time.Hour) // 1 hour in the past

	token, err := GenerateOTLPBearerTokenWithExpiration(sharedSecret, expiration)
	assert.Error(t, err)
	assert.Empty(t, token)
	assert.Contains(t, err.Error(), "expiration time is in the past")
}

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
