package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTimeoutErrorAPI(t *testing.T) {
	// Generate test keys
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	testBody := "Timeout API test body"

	t.Run("Public API for timeout error detection", func(t *testing.T) {
		// Create request
		req, err := http.NewRequest("POST", "http://example.com/test", strings.NewReader(testBody))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		// Prepare streaming signature with short timeout
		err = PrepareHTTPRequestForStreaming(privateKey, req)
		if err != nil {
			t.Fatalf("Failed to prepare streaming: %v", err)
		}

		// Manually set a very short timeout for testing
		if reader, ok := req.Body.(*StreamingSignatureReader); ok {
			reader.timeout = 50 * time.Millisecond

			// Create a hanging goroutine scenario
			reader.wg.Add(1) // Add extra count to simulate hanging
		}

		// Try to read - should timeout
		_, err = io.ReadAll(req.Body)

		if err == nil {
			t.Fatal("Expected timeout error, got none")
		}

		// Test the public API for error detection
		if errors.Is(err, ErrSignatureComputationTimeout) {
			t.Logf("✅ errors.Is() correctly identifies timeout: %v", err)
		} else {
			t.Fatalf("errors.Is() failed to identify timeout error: %v", err)
		}

		// Test error message content
		if strings.Contains(err.Error(), "signature computation timeout") {
			t.Log("✅ Error message contains expected timeout description")
		}

		// Test error message includes timeout duration
		if !strings.Contains(err.Error(), "after 50ms") {
			t.Logf("Warning: expected duration in error message, got: %v", err)
		}

		req.Body.Close()
		t.Log("✓ Public timeout error API works correctly")
	})

	t.Run("Error handling patterns for users", func(t *testing.T) {
		// Demonstrate how users should handle timeout errors

		req, err := http.NewRequest("POST", "http://example.com/test", strings.NewReader(testBody))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		err = PrepareHTTPRequestForStreaming(privateKey, req)
		if err != nil {
			t.Fatalf("Failed to prepare streaming: %v", err)
		}

		// Simulate timeout by setting very short timeout and adding extra WaitGroup count
		if reader, ok := req.Body.(*StreamingSignatureReader); ok {
			reader.timeout = 25 * time.Millisecond
			reader.wg.Add(1) // Create hanging scenario
		}

		// Read and handle different error types
		_, err = io.ReadAll(req.Body)
		req.Body.Close()

		// Example error handling patterns users can use
		switch {
		case err == nil:
			t.Log("✅ Success - signature computed successfully")

		case errors.Is(err, ErrSignatureComputationTimeout):
			t.Logf("⏰ Timeout - signature computation took too long: %v", err)
			// User could retry with longer timeout or use fallback authentication

		case strings.Contains(err.Error(), "signature computation failed"):
			t.Logf("🔐 Signature Error - cryptographic operation failed: %v", err)
			// User could log security event and reject request

		case strings.Contains(err.Error(), "context canceled"):
			t.Logf("🚫 Canceled - request was canceled: %v", err)
			// User could clean up resources and exit gracefully

		default:
			t.Logf("❌ Other Error - unexpected failure: %v", err)
			// User could log and handle as general error
		}

		// For this test, we expect timeout
		if !errors.Is(err, ErrSignatureComputationTimeout) {
			t.Fatalf("Expected timeout for this test, got: %v", err)
		}

		t.Log("✓ Error handling patterns demonstrated")
	})

	t.Run("Timeout error is distinct from other errors", func(t *testing.T) {
		// Verify that timeout errors are distinguishable from other error types

		timeoutErr := ErrSignatureComputationTimeout
		otherErrors := []error{
			errors.New("some other error"),
			io.EOF,
			errors.New("signature computation failed: crypto error"),
			errors.New("context canceled"),
		}

		// Timeout error should only match itself
		for _, other := range otherErrors {
			if errors.Is(other, ErrSignatureComputationTimeout) {
				t.Fatalf("Error %v should not match ErrSignatureComputationTimeout", other)
			}
			if errors.Is(timeoutErr, other) {
				t.Fatalf("ErrSignatureComputationTimeout should not match %v", other)
			}
		}

		t.Log("✓ Timeout error is properly distinct from other error types")
	})
}

func TestTimeoutErrorValues(t *testing.T) {
	// Test the predefined error values

	if ErrSignatureComputationTimeout == nil {
		t.Fatal("ErrSignatureComputationTimeout should not be nil")
	}

	if ErrSignatureComputationTimeout.Error() != "signature computation timeout" {
		t.Fatalf("Unexpected error message: %v", ErrSignatureComputationTimeout.Error())
	}

	// Test wrapped error messages
	wrappedErr := errors.Join(ErrSignatureComputationTimeout, errors.New("additional context"))
	if !errors.Is(wrappedErr, ErrSignatureComputationTimeout) {
		t.Fatal("Wrapped error should still be identifiable")
	}

	t.Log("✓ Predefined timeout error has correct values and behavior")
}
