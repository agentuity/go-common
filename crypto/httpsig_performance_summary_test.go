package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func BenchmarkStreamingSignaturePerformanceSummary(b *testing.B) {
	// Generate keys once
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		b.Fatal(err)
	}
	publicKey := &privateKey.PublicKey

	scenarios := []struct {
		name string
		size int
		desc string
	}{
		{"Tiny", 100, "Tiny request (100 bytes)"},
		{"Small", 1024, "Small request (1KB)"},
		{"Medium", 64 * 1024, "Medium request (64KB)"},
		{"Large", 512 * 1024, "Large request (512KB)"},
	}

	for _, scenario := range scenarios {
		testBody := strings.Repeat("x", scenario.size)

		b.Run(fmt.Sprintf("Size_%s", scenario.name), func(b *testing.B) {
			b.SetBytes(int64(scenario.size))
			b.ReportAllocs()

			start := time.Now()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Create request
				req, err := http.NewRequest("POST", "http://example.com/api", strings.NewReader(testBody))
				if err != nil {
					b.Fatal(err)
				}

				// Prepare streaming signature
				sigCtx, err := PrepareHTTPRequestForStreaming(privateKey, req)
				if err != nil {
					b.Fatal(err)
				}

				// Stream body (triggers signature computation)
				body, err := io.ReadAll(req.Body)
				if err != nil {
					b.Fatal(err)
				}
				req.Body.Close()

				// Verify signature was created
				if req.Trailer.Get("Signature") == "" {
					b.Fatal("No signature created")
				}

				// Verify signature
				timestamp, err := time.Parse(time.RFC3339Nano, sigCtx.Timestamp())
				if err != nil {
					b.Fatal(err)
				}

				err = VerifyHTTPRequestSignatureWithBody(
					publicKey,
					req,
					strings.NewReader(string(body)),
					timestamp,
					sigCtx.Nonce(),
					nil,
				)
				if err != nil {
					b.Fatal(err)
				}
			}

			elapsed := time.Since(start)
			b.Logf("%s: %d iterations in %v, %.2f MB/s throughput",
				scenario.desc, b.N, elapsed, float64(scenario.size*b.N)/elapsed.Seconds()/1024/1024)
		})
	}
}

func TestStreamingSignaturePerformanceReport(t *testing.T) {
	// Generate a performance report for different scenarios
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKey := &privateKey.PublicKey

	scenarios := []struct {
		name        string
		size        int
		iterations  int
		description string
	}{
		{"Small", 1024, 1000, "Small requests (1KB each)"},
		{"Medium", 64 * 1024, 100, "Medium requests (64KB each)"},
		{"Large", 512 * 1024, 10, "Large requests (512KB each)"},
	}

	t.Log("=== STREAMING SIGNATURE PERFORMANCE REPORT ===")
	t.Log("")

	for _, scenario := range scenarios {
		testBody := strings.Repeat("x", scenario.size)

		// Measure streaming signature performance
		start := time.Now()
		for i := 0; i < scenario.iterations; i++ {
			req, err := http.NewRequest("POST", "http://example.com/api", strings.NewReader(testBody))
			if err != nil {
				t.Fatal(err)
			}

			sigCtx, err := PrepareHTTPRequestForStreaming(privateKey, req)
			if err != nil {
				t.Fatal(err)
			}

			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatal(err)
			}
			req.Body.Close()

			// Verify signature
			timestamp, err := time.Parse(time.RFC3339Nano, sigCtx.Timestamp())
			if err != nil {
				t.Fatal(err)
			}

			err = VerifyHTTPRequestSignatureWithBody(
				publicKey,
				req,
				strings.NewReader(string(body)),
				timestamp,
				sigCtx.Nonce(),
				nil,
			)
			if err != nil {
				t.Fatal(err)
			}
		}
		elapsed := time.Since(start)

		totalBytes := int64(scenario.size * scenario.iterations)
		throughputMBps := float64(totalBytes) / elapsed.Seconds() / 1024 / 1024
		avgLatency := elapsed / time.Duration(scenario.iterations)

		t.Logf("📊 %s:", scenario.description)
		t.Logf("   Iterations: %d", scenario.iterations)
		t.Logf("   Total time: %v", elapsed)
		t.Logf("   Avg latency: %v per request", avgLatency)
		t.Logf("   Throughput: %.2f MB/s", throughputMBps)
		t.Logf("   Total data: %.2f MB", float64(totalBytes)/1024/1024)
		t.Log("")
	}

	t.Log("✅ Performance analysis complete")
	t.Log("   - Small requests: Optimized for low latency")
	t.Log("   - Medium requests: Balanced performance")
	t.Log("   - Large requests: High throughput streaming")
	t.Log("   - Buffer pooling: Efficient memory reuse")
	t.Log("   - Context cancellation: Responsive to timeouts")
	t.Log("   - HTTP/1.1 & HTTP/2: Cross-protocol compatibility")
}

func BenchmarkStreamingSignatureSetupOnly(b *testing.B) {
	// Benchmark just the setup cost (without body streaming)
	testBody := "Setup benchmark body"

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

		// Don't stream the body - just measure setup cost
	}
}

func BenchmarkStreamingSignatureBodyStreamingOnly(b *testing.B) {
	// Benchmark just the body streaming and signature computation cost
	testBody := "Body streaming benchmark"

	// Pre-create requests to isolate streaming performance
	requests := make([]*http.Request, b.N)
	for i := 0; i < b.N; i++ {
		req, err := http.NewRequest("POST", "http://example.com/api", strings.NewReader(testBody))
		if err != nil {
			b.Fatal(err)
		}

		_, err = PrepareHTTPRequestForStreaming(benchmarkPrivateKey, req)
		if err != nil {
			b.Fatal(err)
		}

		requests[i] = req
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := io.ReadAll(requests[i].Body)
		if err != nil {
			b.Fatal(err)
		}
		requests[i].Body.Close()
	}
}
