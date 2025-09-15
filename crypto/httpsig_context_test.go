package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSignatureContextAccessors(t *testing.T) {
	// Generate test keys
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	// Create request
	req, err := http.NewRequest("POST", "http://example.com/test", strings.NewReader("test body"))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	// Prepare for streaming to get SignatureContext
	sigCtx, err := PrepareHTTPRequestForStreaming(privateKey, req)
	if err != nil {
		t.Fatalf("Failed to prepare streaming: %v", err)
	}

	// Test that we can access timestamp and nonce via getter methods
	timestamp := sigCtx.Timestamp()
	nonce := sigCtx.Nonce()

	// Verify timestamp is valid RFC3339Nano format
	_, err = time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		t.Fatalf("Invalid timestamp format: %v", err)
	}

	// Verify nonce is not empty
	if nonce == "" {
		t.Fatal("Nonce should not be empty")
	}

	// Verify they return consistent values
	if sigCtx.Timestamp() != timestamp {
		t.Fatal("Timestamp() should return consistent values")
	}
	if sigCtx.Nonce() != nonce {
		t.Fatal("Nonce() should return consistent values")
	}

	t.Logf("✓ SignatureContext accessors work correctly")
	t.Logf("  Timestamp: %s", timestamp)
	t.Logf("  Nonce: %s", nonce)
}
