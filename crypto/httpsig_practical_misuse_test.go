package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPracticalMisuseScenario(t *testing.T) {
	// Generate test keys
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}
	publicKey := &privateKey.PublicKey

	testBody := "Practical misuse protection test"

	// Create and prepare a properly signed request
	req, err := http.NewRequest("POST", "http://example.com/test", strings.NewReader(testBody))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	err = PrepareHTTPRequestForStreaming(privateKey, req)
	if err != nil {
		t.Fatalf("Failed to prepare streaming: %v", err)
	}

	// Stream the body to compute signature
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("Failed to read body: %v", err)
	}
	req.Body.Close()

	// Verify signature was set
	signature := req.Trailer.Get("Signature")
	if signature == "" {
		t.Fatal("Expected signature in trailer")
	}

	// Get the correct values from headers and context
	headerTimestamp := req.Header.Get(HeaderSignatureTimestamp)
	contextNonce := req.Header.Get(HeaderSignatureNonce)
	contextTimestamp, _ := time.Parse(time.RFC3339Nano, headerTimestamp)

	t.Logf("Header timestamp: %s", headerTimestamp)
	t.Logf("Header nonce: %s", contextNonce)

	// Test common programming mistakes

	t.Run("Using wrong timestamp source", func(t *testing.T) {
		// Mistake: using current time instead of the timestamp from when signature was created
		wrongTimestamp := time.Now() // This will be different from the original

		err := VerifyHTTPRequestSignatureWithBody(
			publicKey,
			req,
			strings.NewReader(string(body)),
			wrongTimestamp,
			contextNonce,
			nil,
		)

		if err == nil {
			t.Fatal("Expected error due to timestamp mismatch")
		}
		if !strings.Contains(err.Error(), "timestamp mismatch") {
			t.Fatalf("Expected timestamp mismatch error, got: %v", err)
		}
		t.Logf("✓ Caught programming mistake: %v", err)
	})

	t.Run("Using wrong nonce source", func(t *testing.T) {
		// Mistake: generating a new nonce instead of using the one from the request
		wrongNonce := "newly-generated-nonce"

		err := VerifyHTTPRequestSignatureWithBody(
			publicKey,
			req,
			strings.NewReader(string(body)),
			contextTimestamp,
			wrongNonce,
			nil,
		)

		if err == nil {
			t.Fatal("Expected error due to nonce mismatch")
		}
		if !strings.Contains(err.Error(), "nonce mismatch") {
			t.Fatalf("Expected nonce mismatch error, got: %v", err)
		}
		t.Logf("✓ Caught programming mistake: %v", err)
	})

	t.Run("Correct usage with values from context", func(t *testing.T) {
		// Correct: using values extracted from SignatureContext
		err := VerifyHTTPRequestSignatureWithBody(
			publicKey,
			req,
			strings.NewReader(string(body)),
			contextTimestamp,
			contextNonce,
			nil,
		)

		if err != nil {
			t.Fatalf("Should succeed with correct values from context: %v", err)
		}
		t.Log("✓ Verification succeeded with correct parameters from context")
	})

	t.Run("Correct usage with values parsed from headers", func(t *testing.T) {
		err = VerifyHTTPRequestStreaming(
			publicKey,
			req,
			strings.NewReader(string(body)),
			nil,
		)

		if err != nil {
			t.Fatalf("Should succeed with correct values from headers: %v", err)
		}
		t.Log("✓ Verification succeeded with correct parameters from headers")
	})
}
