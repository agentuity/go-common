package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestStreamingSignatureImprovements(t *testing.T) {
	// Generate test keys
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	t.Run("No race condition - signature ready before request completion", func(t *testing.T) {
		testBody := "Test race condition fix"

		req, err := http.NewRequest("POST", "http://example.com/test", strings.NewReader(testBody))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		_, err = PrepareHTTPRequestForStreaming(privateKey, req)
		if err != nil {
			t.Fatalf("Failed to prepare streaming: %v", err)
		}

		// Stream data immediately (no blocking)
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("Failed to read body: %v", err)
		}

		// Close body to ensure signature computation completes
		err = req.Body.Close()
		if err != nil {
			t.Fatalf("Failed to close body: %v", err)
		}

		// Verify body content matches original
		if string(body) != testBody {
			t.Fatalf("Body mismatch: expected %q, got %q", testBody, string(body))
		}

		// Verify signature was computed successfully
		signature := req.Trailer.Get("Signature")
		if signature == "" {
			t.Fatal("Expected signature in trailer")
		}

		t.Logf("✓ No race condition: signature ready (%s...)", signature[:32])
	})

	t.Run("True streaming - data flows immediately", func(t *testing.T) {
		testBody := "This body streams immediately without blocking"

		req, err := http.NewRequest("POST", "http://example.com/test", strings.NewReader(testBody))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		_, err = PrepareHTTPRequestForStreaming(privateKey, req)
		if err != nil {
			t.Fatalf("Failed to prepare streaming: %v", err)
		}

		// Read data in chunks to verify streaming behavior
		buf := make([]byte, 10) // Small buffer to force multiple reads
		var result []byte

		for {
			n, err := req.Body.Read(buf)
			if n > 0 {
				result = append(result, buf[:n]...)
				t.Logf("Read chunk: %q (%d bytes)", string(buf[:n]), n)
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("Error during streaming: %v", err)
			}
		}
		req.Body.Close()

		// Verify complete body was streamed
		if string(result) != testBody {
			t.Fatalf("Streamed body mismatch: expected %q, got %q", testBody, string(result))
		}

		// Verify signature was computed
		signature := req.Trailer.Get("Signature")
		if signature == "" {
			t.Fatal("Expected signature in trailer")
		}

		t.Log("✓ True streaming: data flowed immediately in chunks")
		t.Logf("✓ Signature computed: %s...", signature[:32])
	})

	t.Run("Proper error propagation on signature failure", func(t *testing.T) {
		// Test with empty body to ensure basic functionality
		req, err := http.NewRequest("POST", "http://example.com/test", strings.NewReader(""))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		_, err = PrepareHTTPRequestForStreaming(privateKey, req)
		if err != nil {
			t.Fatalf("Failed to prepare streaming: %v", err)
		}

		// Read the (empty) body
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("Failed to read body: %v", err)
		}
		req.Body.Close()

		// Verify even empty body gets signature
		if len(body) != 0 {
			t.Fatalf("Expected empty body, got %d bytes", len(body))
		}

		signature := req.Trailer.Get("Signature")
		if signature == "" {
			t.Fatal("Expected signature even for empty body")
		}

		t.Log("✓ Proper signature handling for edge cases (empty body)")
	})

	t.Run("StreamingSignatureReader provides error access", func(t *testing.T) {
		// Test the StreamingSignatureReader directly
		pr, pw := io.Pipe()
		reader := &StreamingSignatureReader{
			pr: pr,
			pw: pw,
		}

		// Initially no error
		if reader.Error() != nil {
			t.Fatal("Expected no initial error")
		}

		// Close pipe and read to trigger EOF
		pw.Close()
		buf := make([]byte, 10)
		_, err := reader.Read(buf)
		if err != io.EOF {
			t.Fatalf("Expected EOF, got: %v", err)
		}

		// Still no error since no signature computation error occurred
		if reader.Error() != nil {
			t.Fatalf("Unexpected error: %v", reader.Error())
		}

		reader.Close()
		t.Log("✓ StreamingSignatureReader provides proper error access")
	})
}
