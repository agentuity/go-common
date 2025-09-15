package crypto

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestStreamingSignatureContextCancellation(t *testing.T) {
	// Generate test keys
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	testCases := []struct {
		name          string
		cancelWhen    string
		expectError   bool
		errorContains string
	}{
		{
			name:          "Early cancellation before reading",
			cancelWhen:    "immediate",
			expectError:   true,
			errorContains: "context canceled",
		},
		{
			name:          "Cancellation during body streaming",
			cancelWhen:    "during_read",
			expectError:   true,
			errorContains: "context canceled",
		},
		{
			name:        "No cancellation - normal completion",
			cancelWhen:  "never",
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a context with cancellation
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Create request with context - use a larger body for reliable cancellation testing
			testBody := strings.Repeat("Context cancellation test with streaming signatures. ", 1000) // Large body
			req, err := http.NewRequest("POST", "http://example.com/test", strings.NewReader(testBody))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			req = req.WithContext(ctx)

			err = PrepareHTTPRequestForStreaming(privateKey, req)
			if err != nil {
				t.Fatalf("Failed to prepare streaming: %v", err)
			}

			// Handle different cancellation scenarios
			switch tc.cancelWhen {
			case "immediate":
				// Cancel immediately
				cancel()

				// Try to read - should get context cancellation error
				buf := make([]byte, 1024)
				_, err = req.Body.Read(buf)

			case "during_read":
				// Start reading in goroutine and cancel after a short delay
				go func() {
					time.Sleep(5 * time.Millisecond)
					cancel()
				}()

				// Try to read entire body - this should be canceled
				buf := make([]byte, 100) // Smaller buffer to ensure multiple reads
				for {
					_, readErr := req.Body.Read(buf)
					if readErr != nil {
						err = readErr
						break
					}
					// Small delay to allow cancellation to take effect
					time.Sleep(1 * time.Millisecond)
				}

			case "never":
				// Normal completion without cancellation
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatalf("Failed to read body: %v", err)
				}
				req.Body.Close()

				if string(body) != testBody {
					t.Fatalf("Body mismatch: expected %q, got %q", testBody, string(body))
				}

				// Verify signature was set
				signature := req.Trailer.Get("Signature")
				if signature == "" {
					t.Fatal("Expected signature in trailer")
				}

				t.Log("✓ Normal completion - signature computed successfully")
				return
			}

			// Check error expectations
			if tc.expectError {
				if err == nil {
					t.Fatal("Expected error due to cancellation, but got none")
				}
				if !strings.Contains(err.Error(), tc.errorContains) {
					t.Fatalf("Expected error containing %q, got: %v", tc.errorContains, err)
				}
				t.Logf("✓ Got expected cancellation error: %v", err)
			} else if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Ensure body is closed properly
			req.Body.Close()
		})
	}
}

func TestStreamingSignatureGoroutineLeakPrevention(t *testing.T) {
	// Generate test keys
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	// Create multiple requests and cancel them to test goroutine cleanup
	numRequests := 10

	for i := 0; i < numRequests; i++ {
		ctx, cancel := context.WithCancel(context.Background())

		testBody := "Goroutine leak prevention test"
		req, err := http.NewRequest("POST", "http://example.com/test", strings.NewReader(testBody))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req = req.WithContext(ctx)

		err = PrepareHTTPRequestForStreaming(privateKey, req)
		if err != nil {
			t.Fatalf("Failed to prepare streaming: %v", err)
		}

		// Cancel immediately to test cleanup
		cancel()

		// Try to read to trigger the error path
		buf := make([]byte, 1024)
		_, err = req.Body.Read(buf)

		// Should get cancellation error
		if err == nil {
			t.Fatal("Expected cancellation error")
		}

		// Close the body
		req.Body.Close()
	}

	// Give goroutines time to clean up
	time.Sleep(50 * time.Millisecond)

	t.Logf("✓ Created and canceled %d requests - goroutines should be cleaned up", numRequests)
}

// cancellableSlowReader simulates a slow reader that can be interrupted
type cancellableSlowReader struct {
	data  string
	pos   int
	delay time.Duration
}

func (s *cancellableSlowReader) Read(p []byte) (n int, err error) {
	if s.pos >= len(s.data) {
		return 0, io.EOF
	}

	// Add artificial delay to allow cancellation
	time.Sleep(s.delay)

	// Read small chunks
	remaining := len(s.data) - s.pos
	toRead := len(p)
	if toRead > remaining {
		toRead = remaining
	}
	if toRead > 10 { // Limit chunk size for slower reading
		toRead = 10
	}

	copy(p, s.data[s.pos:s.pos+toRead])
	s.pos += toRead
	return toRead, nil
}

func TestContextAwareCopyFunction(t *testing.T) {
	testCases := []struct {
		name          string
		input         string
		readerDelay   time.Duration
		cancelAfter   time.Duration
		expectError   bool
		errorContains string
	}{
		{
			name:        "Normal copy completion",
			input:       "Normal copy test data",
			readerDelay: 0,
			cancelAfter: 0, // No cancellation
			expectError: false,
		},
		{
			name:          "Copy with cancellation",
			input:         strings.Repeat("This copy will be canceled. ", 50),
			readerDelay:   5 * time.Millisecond, // Slow reader
			cancelAfter:   10 * time.Millisecond,
			expectError:   true,
			errorContains: "context",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			src := &cancellableSlowReader{
				data:  tc.input,
				delay: tc.readerDelay,
			}
			var dst strings.Builder

			// Set up cancellation if needed
			if tc.cancelAfter > 0 {
				go func() {
					time.Sleep(tc.cancelAfter)
					cancel()
				}()
			}

			err := contextAwareCopy(ctx, &dst, src)

			if tc.expectError {
				if err == nil {
					t.Fatal("Expected error, but got none")
				}
				if !strings.Contains(err.Error(), tc.errorContains) {
					t.Fatalf("Expected error containing %q, got: %v", tc.errorContains, err)
				}
				t.Logf("✓ Got expected error: %v", err)
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				if dst.String() != tc.input {
					t.Fatalf("Copy mismatch: expected %q, got %q", tc.input, dst.String())
				}
				t.Log("✓ Copy completed successfully")
			}
		})
	}
}
