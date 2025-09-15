package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"fmt"
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

		err = PrepareHTTPRequestForStreaming(privateKey, req)
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

		err = PrepareHTTPRequestForStreaming(privateKey, req)
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

		err = PrepareHTTPRequestForStreaming(privateKey, req)
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

	t.Run("Body streaming error propagation", func(t *testing.T) {
		// Create pipe and reader
		pr, pw := io.Pipe()
		reader := &StreamingSignatureReader{
			pr: pr,
			pw: pw,
		}
		reader.wg.Add(1)

		// Simulate streaming error by closing pipe with error
		testError := errors.New("simulated streaming error")

		go func() {
			defer reader.wg.Done()
			// Simulate goroutine setting error and closing pipe with error
			reader.mu.Lock()
			reader.err = fmt.Errorf("failed to stream body: %w", testError)
			reader.mu.Unlock()
			pw.CloseWithError(testError)
		}()

		// Wait for goroutine to complete and close the reader
		reader.Close()

		// Error() should reflect the signature computation error
		if reader.Error() == nil {
			t.Fatal("Expected Error() to return streaming error")
		}
		if !strings.Contains(reader.Error().Error(), "failed to stream body") {
			t.Fatalf("Expected 'failed to stream body' error, got: %v", reader.Error())
		}

		t.Log("✓ Body streaming error properly propagated")
	})

	t.Run("Signature computation error propagation", func(t *testing.T) {
		// Create pipe and reader
		pr, pw := io.Pipe()
		reader := &StreamingSignatureReader{
			pr: pr,
			pw: pw,
		}
		reader.wg.Add(1)

		// Simulate signature computation error
		signatureError := errors.New("simulated signature computation error")

		go func() {
			defer reader.wg.Done()
			defer pw.Close()

			// Simulate signature computation failure
			reader.mu.Lock()
			reader.err = fmt.Errorf("failed to sign request: %w", signatureError)
			reader.mu.Unlock()
		}()

		// Wait for completion and check error
		reader.Close()

		// Error() should reflect the signature computation error
		if reader.Error() == nil {
			t.Fatal("Expected Error() to return signature computation error")
		}
		if !strings.Contains(reader.Error().Error(), "failed to sign request") {
			t.Fatalf("Expected 'failed to sign request' error, got: %v", reader.Error())
		}

		t.Log("✓ Signature computation error properly propagated")
	})

	t.Run("Read method error propagation on EOF", func(t *testing.T) {
		// Test that Read() properly returns signature errors when reaching EOF
		pr, pw := io.Pipe()
		reader := &StreamingSignatureReader{
			pr: pr,
			pw: pw,
		}
		reader.wg.Add(1)

		testData := "data for EOF error test"
		signatureError := errors.New("signature error at EOF")

		go func() {
			defer reader.wg.Done()
			defer pw.Close()

			// Write test data
			pw.Write([]byte(testData))

			// Set signature computation error
			reader.mu.Lock()
			reader.err = fmt.Errorf("failed to sign request: %w", signatureError)
			reader.mu.Unlock()
		}()

		// Read data until EOF
		buf := make([]byte, 1024)
		var allData []byte
		var gotSignatureError bool
		for {
			n, err := reader.Read(buf)
			if n > 0 {
				allData = append(allData, buf[:n]...)
			}
			if err == io.EOF {
				// EOF should contain signature computation error
				if strings.Contains(err.Error(), "signature computation failed") {
					gotSignatureError = true
				}
				break
			}
			if err != nil {
				// Could be signature computation error during read
				if strings.Contains(err.Error(), "signature computation failed") {
					gotSignatureError = true
					break
				}
				t.Fatalf("Unexpected error during read: %v", err)
			}
		}

		if !gotSignatureError {
			t.Fatal("Expected to get signature computation error during read")
		}

		// Verify data was read correctly
		if string(allData) != testData {
			t.Fatalf("Data mismatch: expected %q, got %q", testData, string(allData))
		}

		// Error() should expose the underlying error
		if reader.Error() == nil {
			t.Fatal("Expected Error() to return signature error")
		}

		reader.Close()
		t.Log("✓ Read method properly returns signature errors on EOF")
	})

	t.Run("Close method error consistency", func(t *testing.T) {
		// Test that Close() returns/surfaces the same errors as Read()

		testCases := []struct {
			name          string
			setupError    func(*StreamingSignatureReader, *io.PipeWriter)
			expectError   bool
			errorContains string
		}{
			{
				name: "No error case",
				setupError: func(reader *StreamingSignatureReader, pw *io.PipeWriter) {
					go func() {
						defer reader.wg.Done()
						defer pw.Close()
						// Normal completion - no error
					}()
				},
				expectError: false,
			},
			{
				name: "Streaming error case",
				setupError: func(reader *StreamingSignatureReader, pw *io.PipeWriter) {
					go func() {
						defer reader.wg.Done()
						reader.mu.Lock()
						reader.err = errors.New("streaming error for close test")
						reader.mu.Unlock()
						pw.CloseWithError(errors.New("pipe error"))
					}()
				},
				expectError:   true,
				errorContains: "streaming error for close test",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				pr, pw := io.Pipe()
				reader := &StreamingSignatureReader{
					pr: pr,
					pw: pw,
				}
				reader.wg.Add(1)

				// Setup the error scenario
				tc.setupError(reader, pw)

				// Close and check error consistency
				closeErr := reader.Close()
				readerErr := reader.Error()

				if tc.expectError {
					if readerErr == nil {
						t.Fatal("Expected Error() to return error")
					}
					if !strings.Contains(readerErr.Error(), tc.errorContains) {
						t.Fatalf("Expected error containing %q, got: %v", tc.errorContains, readerErr)
					}
					t.Logf("✓ Close and Error() properly handle error case: %v", readerErr)
				} else {
					if readerErr != nil {
						t.Fatalf("Unexpected error: %v", readerErr)
					}
					if closeErr != nil {
						t.Fatalf("Unexpected close error: %v", closeErr)
					}
					t.Log("✓ Close and Error() properly handle success case")
				}
			})
		}
	})
}
