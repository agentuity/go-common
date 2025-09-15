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

func TestRequestTrailerSignature(t *testing.T) {
	// Generate test keys
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}
	publicKey := &privateKey.PublicKey

	testBody := "Test body for request trailer signature"

	// Step 1: Prepare request for streaming signature
	req, err := http.NewRequest("POST", "http://example.com/api", strings.NewReader(testBody))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	err = PrepareHTTPRequestForStreaming(privateKey, req)
	if err != nil {
		t.Fatalf("Failed to prepare streaming: %v", err)
	}

	// Verify request trailer is announced correctly
	if req.Trailer == nil {
		t.Error("Expected req.Trailer to be initialized")
	}
	if _, exists := req.Trailer["Signature"]; !exists {
		t.Error("Expected 'Signature' to be announced in req.Trailer")
	}

	// Step 2: Stream the body (simulates HTTP transport)
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("Failed to read body: %v", err)
	}
	req.Body.Close()

	// Step 3: Signature is automatically set when body is closed
	// (The bodyHashingReader.Close() method sets the signature in req.Trailer)

	// Verify signature is in request trailer (set automatically by Close())
	signature := req.Trailer.Get("Signature")
	if signature == "" {
		t.Fatal("Expected signature to be set in request trailer")
	}

	t.Logf("✓ Request trailer signature created: %s", signature[:32]+"...")

	// Step 4: Server-side verification using decoupled function
	timestamp, _ := time.Parse(time.RFC3339Nano, req.Header.Get(HeaderSignatureTimestamp))

	err = VerifyHTTPRequestSignatureWithBody(
		publicKey,
		req,
		strings.NewReader(string(body)),
		timestamp,
		req.Header.Get(HeaderSignatureNonce),
		nil,
	)
	if err != nil {
		t.Fatalf("Failed to verify request signature: %v", err)
	}

	// Step 5: Test tampering detection
	err = VerifyHTTPRequestSignatureWithBody(
		publicKey,
		req,
		strings.NewReader("Tampered body"),
		timestamp,
		req.Header.Get(HeaderSignatureNonce),
		nil,
	)

	if err == nil {
		t.Fatal("Expected verification to fail with tampered body")
	}

	t.Log("✓ Request trailer signature test passed!")
	t.Log("  - Request trailer announcement ✓")
	t.Log("  - Body streaming and hashing ✓")
	t.Log("  - Automatic signature setting on Close() ✓")
	t.Log("  - Signature in request trailer ✓")
	t.Log("  - Server-side verification ✓")
	t.Log("  - Decoupled verification ✓")
	t.Log("  - Tampering detection ✓")
}

func TestServerReceivesRequestTrailerSignature(t *testing.T) {
	// Generate test keys
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}
	publicKey := &privateKey.PublicKey

	testBody := "Server request trailer validation test"
	var serverValidationSuccessful bool
	var receivedTrailerSignature string

	// Create server that validates request trailer signatures
	mux := http.NewServeMux()
	mux.HandleFunc("/api/validate", func(w http.ResponseWriter, r *http.Request) {
		t.Logf("Server received %s request", r.Method)
		t.Logf("Server protocol: %s", r.Proto)
		t.Logf("Request headers: %v", r.Header)
		t.Logf("Request trailers before body read: %v", r.Trailer)

		// CRITICAL: Must read the full request body before trailers are available
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}
		r.Body.Close() // Ensure body is fully consumed

		t.Logf("Server received body: %s (%d bytes)", string(body), len(body))
		t.Logf("Request trailers after body read: %v", r.Trailer)

		// NOW we can check for signature in request trailer
		signature := r.Trailer.Get("Signature")
		if signature == "" {
			http.Error(w, "Missing signature in request trailer", http.StatusUnauthorized)
			return
		}

		receivedTrailerSignature = signature
		t.Logf("Server found signature in request trailer: %s", signature[:32]+"...")

		// Extract signature metadata from headers
		timestampStr := r.Header.Get("X-Signature-Timestamp")
		nonce := r.Header.Get("X-Signature-Nonce")

		// Parse timestamp for verification
		timestamp, err := time.Parse(time.RFC3339Nano, timestampStr)
		if err != nil {
			http.Error(w, "Invalid timestamp", http.StatusBadRequest)
			return
		}

		// Verify the signature using decoupled verification
		err = VerifyHTTPRequestSignatureWithBody(
			publicKey,
			r,
			strings.NewReader(string(body)), // Server's copy of the body
			timestamp,
			nonce,
			nil, // no nonce check
		)

		if err != nil {
			t.Logf("Server signature verification failed: %v", err)
			http.Error(w, fmt.Sprintf("Signature verification failed: %v", err), http.StatusUnauthorized)
			return
		}

		t.Log("✓ Server successfully verified request signature!")
		serverValidationSuccessful = true

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Request signature verified successfully"))
	})

	// Start test server
	server := httptest.NewServer(mux)
	defer server.Close()

	// Create signed request
	req, err := http.NewRequest("POST", server.URL+"/api/validate", strings.NewReader(testBody))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	// Prepare for streaming signature
	err = PrepareHTTPRequestForStreaming(privateKey, req)
	if err != nil {
		t.Fatalf("Failed to prepare streaming: %v", err)
	}

	// Send request to server - let the transport handle body streaming and signature computation
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

	t.Logf("Server response: %s", string(responseBody))
	t.Logf("Response status: %d", resp.StatusCode)

	// After transport completes, verify signature was set in trailer
	signature := req.Trailer.Get("Signature")
	if signature == "" {
		t.Fatal("Expected signature in request trailer after transport completion")
	}
	t.Logf("✓ Client signature in trailer: %s", signature[:32]+"...")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", resp.StatusCode)
	}

	if !serverValidationSuccessful {
		t.Fatal("Server validation was not successful")
	}

	if receivedTrailerSignature == "" {
		t.Fatal("Server did not receive trailer signature")
	}

	t.Log("✓ End-to-end request trailer signature test passed!")
	t.Log("  - Client announces request trailer ✓")
	t.Log("  - Client sets signature in request trailer on Close() ✓")
	t.Log("  - Server receives request trailer signature ✓")
	t.Log("  - Server verifies signature from trailer ✓")
	t.Log("  - Complete request integrity validation ✓")
}
