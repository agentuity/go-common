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

func TestVerifyHTTPRequestSignatureWithBodyMisuseProtection(t *testing.T) {
	// Generate test keys
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}
	publicKey := &privateKey.PublicKey

	testBody := "Test body for misuse protection"

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

	ts := req.Header.Get(HeaderSignatureTimestamp)

	// Get the correct timestamp and nonce from context
	correctTimestamp, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t.Fatalf("Failed to parse timestamp: %v", err)
	}
	correctNonce := req.Header.Get(HeaderSignatureNonce)

	testCases := []struct {
		name          string
		useTimestamp  time.Time
		useNonce      string
		expectError   bool
		errorContains string
		description   string
	}{
		{
			name:         "Correct parameters matching headers",
			useTimestamp: correctTimestamp,
			useNonce:     correctNonce,
			expectError:  false,
			description:  "Should succeed when parameters match headers",
		},
		{
			name:          "Wrong timestamp parameter",
			useTimestamp:  correctTimestamp.Add(time.Hour), // Different timestamp
			useNonce:      correctNonce,
			expectError:   true,
			errorContains: "timestamp mismatch between header",
			description:   "Should fail when timestamp parameter doesn't match header",
		},
		{
			name:          "Wrong nonce parameter",
			useTimestamp:  correctTimestamp,
			useNonce:      "wrong-nonce-value",
			expectError:   true,
			errorContains: "nonce mismatch between header",
			description:   "Should fail when nonce parameter doesn't match header",
		},
		{
			name:          "Both parameters wrong",
			useTimestamp:  correctTimestamp.Add(time.Hour),
			useNonce:      "wrong-nonce-value",
			expectError:   true,
			errorContains: "timestamp mismatch between header", // Should fail on first check
			description:   "Should fail when both parameters are wrong",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Attempt verification with test case parameters
			err := VerifyHTTPRequestSignatureWithBody(
				publicKey,
				req,
				strings.NewReader(string(body)),
				tc.useTimestamp,
				tc.useNonce,
				nil,
			)

			if tc.expectError {
				if err == nil {
					t.Fatalf("Expected error (%s), but got none", tc.description)
				}
				if !strings.Contains(err.Error(), tc.errorContains) {
					t.Fatalf("Expected error containing %q, got: %v", tc.errorContains, err)
				}
				t.Logf("✓ Got expected error: %v", err)
			} else {
				if err != nil {
					t.Fatalf("Unexpected error (%s): %v", tc.description, err)
				}
				t.Logf("✓ %s", tc.description)
			}
		})
	}
}

func TestVerifyHTTPRequestSignatureWithBodyNoHeaders(t *testing.T) {
	// Generate test keys
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}
	publicKey := &privateKey.PublicKey

	testBody := "Test body for no headers scenario"

	// Create request without signature headers (simulating a manually constructed request)
	req, err := http.NewRequest("POST", "http://example.com/test", strings.NewReader(testBody))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	// Manually set a trailer without the signature metadata headers
	req.Trailer = make(http.Header)
	req.Trailer.Set("Signature", "dummy-signature-for-test")

	// Try to verify with arbitrary timestamp and nonce
	timestamp := time.Now()
	nonce := "test-nonce"

	// This should NOT fail due to header mismatch since headers are missing
	// (though it will fail later during actual signature verification)
	err = VerifyHTTPRequestSignatureWithBody(
		publicKey,
		req,
		strings.NewReader(testBody),
		timestamp,
		nonce,
		nil,
	)

	// Should not get a mismatch error since headers are empty
	if err != nil && strings.Contains(err.Error(), "mismatch between header") {
		t.Fatalf("Should not get header mismatch error when headers are missing, got: %v", err)
	}

	// Should get some other error (like signature verification failure)
	if err == nil {
		t.Fatal("Expected some error (like signature verification failure), but got none")
	}

	t.Logf("✓ No header mismatch error when signature headers are missing: %v", err)
}

func TestMisuseProtectionWithPartialHeaders(t *testing.T) {
	// Generate test keys
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}
	publicKey := &privateKey.PublicKey

	testBody := "Test body for partial headers"

	// Create request with only timestamp header (missing nonce header)
	req, err := http.NewRequest("POST", "http://example.com/test", strings.NewReader(testBody))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	timestamp := time.Now()
	nonce := "test-nonce"

	// Set only timestamp header, leave nonce header empty
	req.Header.Set("X-Signature-Timestamp", timestamp.Format(time.RFC3339Nano))
	req.Trailer = make(http.Header)
	req.Trailer.Set("Signature", "dummy-signature")

	// Verify with matching timestamp but different nonce
	err = VerifyHTTPRequestSignatureWithBody(
		publicKey,
		req,
		strings.NewReader(testBody),
		timestamp,
		nonce, // This nonce doesn't have corresponding header
		nil,
	)

	// Should NOT get nonce mismatch error since nonce header is missing
	if err != nil && strings.Contains(err.Error(), "nonce mismatch") {
		t.Fatalf("Should not get nonce mismatch error when nonce header is missing, got: %v", err)
	}

	// Should not get timestamp mismatch error since timestamp matches
	if err != nil && strings.Contains(err.Error(), "timestamp mismatch") {
		t.Fatalf("Should not get timestamp mismatch error when timestamp matches, got: %v", err)
	}

	t.Log("✓ No mismatch errors when only partial headers are present and parameters match existing headers")
}
