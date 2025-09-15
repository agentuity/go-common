package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPrepareHTTPRequestForStreamingIntegration(t *testing.T) {
	// Generate test keys
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}
	publicKey := &privateKey.PublicKey

	testCases := []struct {
		name string
		body string
	}{
		{"Small body", "Hello, World!"},
		{"Longer body", "This is a longer test message with some content to hash and verify."},
		{"Empty body", ""},
		{"Multi-line body", "Line 1\nLine 2\nLine 3"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create request
			req, err := http.NewRequest("POST", "http://example.com/test", strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			// Prepare for streaming
			sigCtx, err := PrepareHTTPRequestForStreaming(privateKey, req)
			if err != nil {
				t.Fatalf("Failed to prepare streaming: %v", err)
			}

			// Verify setup
			if req.Trailer == nil {
				t.Fatal("Request trailer not initialized")
			}
			if req.Header.Get("Trailer") != "Signature" {
				t.Fatal("Trailer header should announce Signature")
			}
			if req.ContentLength != -1 {
				t.Fatal("Content length should be -1")
			}

			// Stream the body (this should trigger signature computation)
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("Failed to read body: %v", err)
			}
			req.Body.Close()

			// Verify body content
			if string(body) != tc.body {
				t.Fatalf("Body mismatch: expected %q, got %q", tc.body, string(body))
			}

			// Verify signature was set in trailer
			signature := req.Trailer.Get("Signature")
			if signature == "" {
				t.Fatal("Signature not set in trailer")
			}

			t.Logf("Signature created: %s...", signature[:min(32, len(signature))])

			// Verify signature using our verification function
			timestamp, err := time.Parse(time.RFC3339Nano, sigCtx.Timestamp())
			if err != nil {
				t.Fatalf("Failed to parse timestamp: %v", err)
			}

			err = VerifyHTTPRequestSignatureWithBody(
				publicKey,
				req,
				strings.NewReader(tc.body),
				timestamp,
				sigCtx.Nonce(),
				nil,
			)
			if err != nil {
				t.Fatalf("Signature verification failed: %v", err)
			}

			t.Log("✓ Signature verification successful")
		})
	}
}

func TestTrailerBasedHTTPSignatureEndToEnd(t *testing.T) {
	// Generate test keys
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}
	publicKey := &privateKey.PublicKey

	testBody := "End-to-end trailer signature test"
	var serverReceivedTrailer bool
	var serverVerificationSuccess bool

	// Create test server that verifies trailer signatures
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("Server received request: %s %s", r.Method, r.URL.Path)
		t.Logf("Transfer-Encoding: %v", r.TransferEncoding)
		t.Logf("Content-Length: %d", r.ContentLength)
		t.Logf("Trailer header announced: %v", r.Header.Get("Trailer"))
		t.Logf("Initial Trailer: %v", r.Trailer)

		// Read the body completely - this is required for trailers to be available
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Logf("Error reading body: %v", err)
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}
		r.Body.Close()

		t.Logf("Body received: %q (%d bytes)", string(body), len(body))
		t.Logf("Trailers after body read: %v", r.Trailer)

		// Check for signature in trailer
		signature := r.Trailer.Get("Signature")
		if signature == "" {
			t.Log("No signature found in trailer")
			http.Error(w, "No signature in trailer", http.StatusUnauthorized)
			return
		}

		serverReceivedTrailer = true
		t.Logf("✓ Server received signature trailer: %s...", signature[:min(32, len(signature))])

		// Extract metadata from headers
		timestampStr := r.Header.Get("X-Signature-Timestamp")
		nonce := r.Header.Get("X-Signature-Nonce")

		timestamp, err := time.Parse(time.RFC3339Nano, timestampStr)
		if err != nil {
			t.Logf("Invalid timestamp: %v", err)
			http.Error(w, "Invalid timestamp", http.StatusBadRequest)
			return
		}

		// Verify signature
		err = VerifyHTTPRequestSignatureWithBody(
			publicKey,
			r,
			strings.NewReader(string(body)),
			timestamp,
			nonce,
			nil,
		)

		if err != nil {
			t.Logf("Signature verification failed: %v", err)
			http.Error(w, fmt.Sprintf("Signature verification failed: %v", err), http.StatusUnauthorized)
			return
		}

		serverVerificationSuccess = true
		t.Log("✅ Server successfully verified signature!")

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Signature verified"))
	}))
	defer server.Close()

	// Create and prepare client request
	req, err := http.NewRequest("POST", server.URL+"/test", strings.NewReader(testBody))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	_, err = PrepareHTTPRequestForStreaming(privateKey, req)
	if err != nil {
		t.Fatalf("Failed to prepare streaming: %v", err)
	}

	t.Logf("Client request prepared with trailers: %v", req.Trailer)

	// Send request to server - transport handles streaming and signature computation
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read response
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	t.Logf("Response status: %d", resp.StatusCode)
	t.Logf("Response body: %s", string(responseBody))

	// Verify success
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", resp.StatusCode)
	}

	if !serverReceivedTrailer {
		t.Fatal("Server did not receive trailer")
	}

	if !serverVerificationSuccess {
		t.Fatal("Server signature verification failed")
	}

	t.Log("🎉 End-to-end trailer signature test PASSED!")
	t.Log("  ✅ Request prepared with trailers")
	t.Log("  ✅ Signature computed during streaming")
	t.Log("  ✅ Server received trailer")
	t.Log("  ✅ Server verified signature successfully")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
