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

// Global benchmark variables to avoid initialization overhead
var (
	benchmarkPrivateKey *ecdsa.PrivateKey
	benchmarkPublicKey  *ecdsa.PublicKey
)

func init() {
	// Pre-generate keys for benchmarks to avoid key generation overhead
	var err error
	benchmarkPrivateKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic("Failed to generate benchmark keys: " + err.Error())
	}
	benchmarkPublicKey = &benchmarkPrivateKey.PublicKey
}

func BenchmarkPrepareHTTPRequestForStreaming(b *testing.B) {
	// Use Option A: http.NoBody to avoid creating writer and goroutine

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req, err := http.NewRequest("POST", "http://example.com/api", http.NoBody)
		if err != nil {
			b.Fatal(err)
		}

		_, err = PrepareHTTPRequestForStreaming(benchmarkPrivateKey, req)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStreamingSignatureSmallBody(b *testing.B) {
	testBody := "Small benchmark body for streaming signature"

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req, err := http.NewRequest("POST", "http://example.com/api", strings.NewReader(testBody))
		if err != nil {
			b.Fatal(err)
		}

		_, err = PrepareHTTPRequestForStreaming(benchmarkPrivateKey, req)
		if err != nil {
			b.Fatal(err)
		}

		// Stream the body to trigger signature computation
		_, err = io.ReadAll(req.Body)
		if err != nil {
			b.Fatal(err)
		}
		req.Body.Close()
	}
}

func BenchmarkStreamingSignatureMediumBody(b *testing.B) {
	// 10KB body
	testBody := strings.Repeat("Medium benchmark body for streaming signature testing. ", 200)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req, err := http.NewRequest("POST", "http://example.com/api", strings.NewReader(testBody))
		if err != nil {
			b.Fatal(err)
		}

		_, err = PrepareHTTPRequestForStreaming(benchmarkPrivateKey, req)
		if err != nil {
			b.Fatal(err)
		}

		// Stream the body to trigger signature computation
		_, err = io.ReadAll(req.Body)
		if err != nil {
			b.Fatal(err)
		}
		req.Body.Close()
	}
}

func BenchmarkStreamingSignatureLargeBody(b *testing.B) {
	// 1MB body
	testBody := strings.Repeat("Large benchmark body for streaming signature performance testing. ", 16384)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req, err := http.NewRequest("POST", "http://example.com/api", strings.NewReader(testBody))
		if err != nil {
			b.Fatal(err)
		}

		_, err = PrepareHTTPRequestForStreaming(benchmarkPrivateKey, req)
		if err != nil {
			b.Fatal(err)
		}

		// Stream the body to trigger signature computation
		_, err = io.ReadAll(req.Body)
		if err != nil {
			b.Fatal(err)
		}
		req.Body.Close()
	}
}

func BenchmarkStreamingSignatureVsNonStreaming(b *testing.B) {
	testBody := "Comparison benchmark body for streaming vs non-streaming signatures"
	testBodyBytes := []byte(testBody)

	b.Run("Streaming", func(b *testing.B) {
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			req, err := http.NewRequest("POST", "http://example.com/api", strings.NewReader(testBody))
			if err != nil {
				b.Fatal(err)
			}

			_, err = PrepareHTTPRequestForStreaming(benchmarkPrivateKey, req)
			if err != nil {
				b.Fatal(err)
			}

			_, err = io.ReadAll(req.Body)
			if err != nil {
				b.Fatal(err)
			}
			req.Body.Close()
		}
	})

	b.Run("NonStreaming", func(b *testing.B) {
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			req, err := http.NewRequest("POST", "http://example.com/api", strings.NewReader(testBody))
			if err != nil {
				b.Fatal(err)
			}

			err = SignHTTPRequest(benchmarkPrivateKey, req, testBodyBytes)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkStreamingSignatureVerification(b *testing.B) {
	testBody := "Verification benchmark body for streaming signature"

	// Pre-create a signed request
	req, err := http.NewRequest("POST", "http://example.com/api", strings.NewReader(testBody))
	if err != nil {
		b.Fatal(err)
	}

	sigCtx, err := PrepareHTTPRequestForStreaming(benchmarkPrivateKey, req)
	if err != nil {
		b.Fatal(err)
	}

	// Stream to compute signature
	body, err := io.ReadAll(req.Body)
	if err != nil {
		b.Fatal(err)
	}
	req.Body.Close()

	// Ensure signature was computed
	if req.Trailer.Get("Signature") == "" {
		b.Fatal("No signature in trailer")
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := VerifyHTTPRequestSignatureWithBody(
			benchmarkPublicKey,
			req,
			strings.NewReader(string(body)),
			mustParseTime(sigCtx.Timestamp()),
			sigCtx.Nonce(),
			nil,
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConcurrentStreamingSignatures(b *testing.B) {
	testBody := "Concurrent benchmark body for streaming signature"

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req, err := http.NewRequest("POST", "http://example.com/api", strings.NewReader(testBody))
			if err != nil {
				b.Fatal(err)
			}

			_, err = PrepareHTTPRequestForStreaming(benchmarkPrivateKey, req)
			if err != nil {
				b.Fatal(err)
			}

			_, err = io.ReadAll(req.Body)
			if err != nil {
				b.Fatal(err)
			}
			req.Body.Close()
		}
	})
}

func BenchmarkStreamingSignatureDifferentSizes(b *testing.B) {
	sizes := []struct {
		name string
		size int
	}{
		{"1KB", 1024},
		{"10KB", 10 * 1024},
		{"100KB", 100 * 1024},
		{"1MB", 1024 * 1024},
	}

	for _, size := range sizes {
		// Create test body of specified size
		testBody := strings.Repeat("x", size.size)

		b.Run(size.name, func(b *testing.B) {
			b.ResetTimer()
			b.SetBytes(int64(size.size)) // Report throughput

			for i := 0; i < b.N; i++ {
				req, err := http.NewRequest("POST", "http://example.com/api", strings.NewReader(testBody))
				if err != nil {
					b.Fatal(err)
				}

				_, err = PrepareHTTPRequestForStreaming(benchmarkPrivateKey, req)
				if err != nil {
					b.Fatal(err)
				}

				_, err = io.ReadAll(req.Body)
				if err != nil {
					b.Fatal(err)
				}
				req.Body.Close()
			}
		})
	}
}

func BenchmarkBufferPoolUsage(b *testing.B) {
	// Specifically benchmark the buffer pool usage in context-aware copy
	testData := strings.Repeat("Buffer pool benchmark data. ", 1000) // ~28KB

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req, err := http.NewRequest("POST", "http://example.com/api", strings.NewReader(testData))
			if err != nil {
				b.Fatal(err)
			}

			_, err = PrepareHTTPRequestForStreaming(benchmarkPrivateKey, req)
			if err != nil {
				b.Fatal(err)
			}

			_, err = io.ReadAll(req.Body)
			if err != nil {
				b.Fatal(err)
			}
			req.Body.Close()
		}
	})
}

func BenchmarkEndToEndStreamingSignature(b *testing.B) {
	// Complete end-to-end benchmark: prepare, stream, and verify
	testBody := "End-to-end benchmark test for complete streaming signature flow"

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Sign
		req, err := http.NewRequest("POST", "http://example.com/api", strings.NewReader(testBody))
		if err != nil {
			b.Fatal(err)
		}

		sigCtx, err := PrepareHTTPRequestForStreaming(benchmarkPrivateKey, req)
		if err != nil {
			b.Fatal(err)
		}

		// Stream
		body, err := io.ReadAll(req.Body)
		if err != nil {
			b.Fatal(err)
		}
		req.Body.Close()

		// Verify
		err = VerifyHTTPRequestSignatureWithBody(
			benchmarkPublicKey,
			req,
			strings.NewReader(string(body)),
			mustParseTime(sigCtx.Timestamp()),
			sigCtx.Nonce(),
			nil,
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStreamingSignatureMemoryProfile(b *testing.B) {
	// Test memory allocation patterns
	testBody := strings.Repeat("Memory profile test data. ", 500) // ~14KB

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req, err := http.NewRequest("POST", "http://example.com/api", strings.NewReader(testBody))
		if err != nil {
			b.Fatal(err)
		}

		_, err = PrepareHTTPRequestForStreaming(benchmarkPrivateKey, req)
		if err != nil {
			b.Fatal(err)
		}

		_, err = io.ReadAll(req.Body)
		if err != nil {
			b.Fatal(err)
		}
		req.Body.Close()
	}
}

// Helper function for benchmarks
func mustParseTime(timeStr string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, timeStr)
	if err != nil {
		panic("Failed to parse time: " + err.Error())
	}
	return t
}
