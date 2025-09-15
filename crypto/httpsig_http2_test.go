package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/http2"
)

func TestHTTP2StreamingSignatureTrailers(t *testing.T) {
	// Generate test keys
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}
	publicKey := &privateKey.PublicKey

	testBody := "HTTP/2 streaming signature test with trailers"
	var serverReceivedTrailer bool
	var serverVerificationSuccess bool
	var serverProtocol string

	// Create HTTP/2 test server
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverProtocol = r.Proto
		t.Logf("Server received %s request via %s", r.Method, r.Proto)
		t.Logf("Content-Length: %d", r.ContentLength)
		t.Logf("Transfer-Encoding: %v", r.TransferEncoding)
		t.Logf("Trailer header announced: %q", r.Header.Get("Trailer"))
		t.Logf("Initial Trailer: %v", r.Trailer)

		// Read the body completely - required for trailers
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
		t.Logf("✓ Server received signature trailer via %s: %s...", r.Proto, signature[:min(32, len(signature))])

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
		t.Logf("✅ Server successfully verified signature via %s!", r.Proto)

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf("Signature verified via %s", r.Proto)))
	}))

	// Configure TLS for HTTP/2
	server.TLS = &tls.Config{
		NextProtos: []string{"h2", "http/1.1"},
	}

	// Enable HTTP/2 on the server
	err = http2.ConfigureServer(server.Config, &http2.Server{})
	if err != nil {
		t.Fatalf("Failed to configure HTTP/2: %v", err)
	}

	// Start server with TLS (required for HTTP/2)
	server.StartTLS()
	defer server.Close()

	// Create HTTP/2 client
	client := &http.Client{
		Transport: &http2.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // For test server
				NextProtos:         []string{"h2"},
			},
		},
	}

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
	t.Logf("Client Trailer header set: %q", req.Header.Get("Trailer"))

	// Send request to server - transport handles streaming and signature computation
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

	t.Logf("Response protocol: %s", resp.Proto)
	t.Logf("Response status: %d", resp.StatusCode)
	t.Logf("Response body: %s", string(responseBody))

	// Verify success
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", resp.StatusCode)
	}

	if serverProtocol != "HTTP/2.0" {
		t.Fatalf("Expected HTTP/2.0, got %s", serverProtocol)
	}

	if !serverReceivedTrailer {
		t.Fatal("Server did not receive trailer")
	}

	if !serverVerificationSuccess {
		t.Fatal("Server signature verification failed")
	}

	t.Log("🎉 HTTP/2 streaming signature trailer test PASSED!")
	t.Log("  ✅ Request sent via HTTP/2.0")
	t.Log("  ✅ Trailers properly handled in HTTP/2")
	t.Log("  ✅ Signature computed during streaming")
	t.Log("  ✅ Server received trailer via HTTP/2")
	t.Log("  ✅ Server verified signature successfully")
	t.Log("  ✅ No forced chunked encoding (HTTP/2 handles frames natively)")
}

func TestHTTP2vsHTTP1StreamingComparison(t *testing.T) {
	// Generate test keys
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}
	publicKey := &privateKey.PublicKey

	testBody := "Protocol comparison test for streaming signatures"

	// Test both HTTP/1.1 and HTTP/2
	protocols := []struct {
		name      string
		setupFunc func() (*httptest.Server, *http.Client)
	}{
		{
			name: "HTTP/1.1",
			setupFunc: func() (*httptest.Server, *http.Client) {
				server := httptest.NewServer(createTestHandler(t, publicKey))
				client := server.Client()
				return server, client
			},
		},
		{
			name: "HTTP/2",
			setupFunc: func() (*httptest.Server, *http.Client) {
				server := httptest.NewUnstartedServer(createTestHandler(t, publicKey))

				// Configure TLS for HTTP/2
				server.TLS = &tls.Config{
					NextProtos: []string{"h2", "http/1.1"},
				}

				http2.ConfigureServer(server.Config, &http2.Server{})
				server.StartTLS()

				client := &http.Client{
					Transport: &http2.Transport{
						TLSClientConfig: &tls.Config{
							InsecureSkipVerify: true,
							NextProtos:         []string{"h2"},
						},
					},
				}
				return server, client
			},
		},
	}

	for _, protocol := range protocols {
		t.Run(protocol.name, func(t *testing.T) {
			server, client := protocol.setupFunc()
			defer server.Close()

			// Create and prepare request
			req, err := http.NewRequest("POST", server.URL+"/test", strings.NewReader(testBody))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			_, err = PrepareHTTPRequestForStreaming(privateKey, req)
			if err != nil {
				t.Fatalf("Failed to prepare streaming: %v", err)
			}

			// Send request
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

			// Verify success
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Expected 200 OK, got %d", resp.StatusCode)
			}

			t.Logf("✅ %s streaming signature test passed", protocol.name)
			t.Logf("   Protocol: %s", resp.Proto)
			t.Logf("   Response: %s", string(responseBody))
		})
	}
}

// Helper function to create a consistent test handler
func createTestHandler(t *testing.T, publicKey *ecdsa.PublicKey) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Read body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}
		r.Body.Close()

		// Check for signature in trailer
		signature := r.Trailer.Get("Signature")
		if signature == "" {
			http.Error(w, "No signature in trailer", http.StatusUnauthorized)
			return
		}

		// Verify signature
		timestampStr := r.Header.Get("X-Signature-Timestamp")
		nonce := r.Header.Get("X-Signature-Nonce")
		timestamp, err := time.Parse(time.RFC3339Nano, timestampStr)
		if err != nil {
			http.Error(w, "Invalid timestamp", http.StatusBadRequest)
			return
		}

		err = VerifyHTTPRequestSignatureWithBody(
			publicKey,
			r,
			strings.NewReader(string(body)),
			timestamp,
			nonce,
			nil,
		)

		if err != nil {
			http.Error(w, "Signature verification failed", http.StatusUnauthorized)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf("Verified via %s", r.Proto)))
	}
}
