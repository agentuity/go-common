package crypto

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	gocstring "github.com/agentuity/go-common/string"
)

// customReaderFunc allows function to implement io.Reader interface
type customReaderFunc func([]byte) (int, error)

func (c customReaderFunc) Read(p []byte) (int, error) {
	return c(p)
}

// SignatureContext holds the state needed for signature verification
type SignatureContext struct {
	timestamp string
	nonce     string
}

// Timestamp returns the timestamp used for signature generation
func (ctx *SignatureContext) Timestamp() string {
	return ctx.timestamp
}

// Nonce returns the nonce used for signature generation
func (ctx *SignatureContext) Nonce() string {
	return ctx.nonce
}

// StreamingSignatureReader wraps the request body and ensures signature is computed before request is sent
type StreamingSignatureReader struct {
	pr        *io.PipeReader
	pw        *io.PipeWriter
	wg        sync.WaitGroup
	err       error
	completed bool
	mu        sync.Mutex
}

// Read implements io.Reader - allows streaming while signature computation happens in parallel
func (s *StreamingSignatureReader) Read(p []byte) (int, error) {
	n, err := s.pr.Read(p)

	// If we reach EOF, ensure signature computation completed successfully
	if err == io.EOF {
		s.wg.Wait() // Wait for signature goroutine to complete
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.err != nil {
			return n, fmt.Errorf("signature computation failed: %w", s.err)
		}
	}

	return n, err
}

// Close implements io.Closer
func (s *StreamingSignatureReader) Close() error {
	s.wg.Wait() // Ensure goroutine completes
	return s.pr.Close()
}

// Error returns any error that occurred during signature computation
func (s *StreamingSignatureReader) Error() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// For public keys too:
func keyIDfromECDSAPublic(pub *ecdsa.PublicKey) (string, error) {
	spki, err := x509.MarshalPKIXPublicKey(pub) // DER-encoded SubjectPublicKeyInfo
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(spki)
	id := sum[:16] // 128-bit truncated ID (first 16 bytes)
	return "k1-ecdsa-" + hex.EncodeToString(id), nil
}

func hashSignature(req *http.Request, body []byte, timestamp string, nonce string) []byte {
	h := sha256.New()

	addHash := func(label string, item string) {
		fmt.Fprintf(h, "%s: %s\n", label, item)
	}
	addHash("method", req.Method)
	addHash("uri", req.URL.RequestURI())
	addHash("timestamp", timestamp)
	addHash("nonce", nonce)

	// Body integrity via digest to avoid huge buffers and delimiter issues
	sum := sha256.Sum256(body)
	addHash("body-sha256", hex.EncodeToString(sum[:]))

	return h.Sum(nil)
}

// hashSignatureStreaming is kept for backward compatibility
func hashSignatureStreaming(req *http.Request, bodyReader io.Reader, timestamp string, nonce string) ([]byte, error) {
	// Stream body and compute hash incrementally
	bodyHash := sha256.New()
	if _, err := io.Copy(bodyHash, bodyReader); err != nil {
		return nil, fmt.Errorf("failed to hash body: %w", err)
	}
	return hashSignatureWithBodyHash(req, bodyHash.Sum(nil), timestamp, nonce), nil
}

func SignHTTPRequest(key *ecdsa.PrivateKey, req *http.Request, body []byte) error {
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	nonce, err := gocstring.GenerateRandomString(16)
	if err != nil {
		return fmt.Errorf("failed to generate nonce: %w", err)
	}

	sig, err := ecdsa.SignASN1(rand.Reader, key, hashSignature(req, body, timestamp, nonce))
	if err != nil {
		return err
	}

	keyID, err := keyIDfromECDSAPublic(&key.PublicKey)
	if err != nil {
		return err
	}

	req.Header.Set("Signature", base64.StdEncoding.EncodeToString(sig))
	req.Header.Set("X-Signature-Alg", "ecdsa-sha256")
	req.Header.Set("X-Signature-KeyID", keyID)
	req.Header.Set("X-Signature-Timestamp", timestamp)
	req.Header.Set("X-Signature-Nonce", nonce)
	return nil
}

// PrepareHTTPRequestForStreaming sets up a request for streaming signature.
// Returns a SignatureContext that should be used to complete the signature after the body is streamed.
func PrepareHTTPRequestForStreaming(key *ecdsa.PrivateKey, req *http.Request) (*SignatureContext, error) {
	keyID, err := keyIDfromECDSAPublic(&key.PublicKey)
	if err != nil {
		return nil, err
	}

	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	nonce, err := gocstring.GenerateRandomString(16)
	if err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Handle nil req.Body by treating it as http.NoBody
	origBody := req.Body
	if origBody == nil {
		origBody = http.NoBody
	}

	// Announce trailer via request headers and set placeholder
	req.Header.Set("Trailer", "Signature")
	req.Trailer = make(http.Header)
	req.Trailer.Set("Signature", "")

	// Enable chunked encoding for HTTP/1.1 (net/http will handle HTTP/2 appropriately)
	req.ContentLength = -1
	req.Header.Del("Content-Length") // Remove Content-Length header to avoid conflicts

	// Create streaming signature reader with proper synchronization
	pr, pw := io.Pipe()
	reader := &StreamingSignatureReader{
		pr: pr,
		pw: pw,
	}

	// Set up wait group to block request until signature is ready
	reader.wg.Add(1)
	req.Body = reader

	// Start goroutine to read original body, hash it, and write to pipe
	go func() {
		defer reader.wg.Done() // Signal completion
		defer pw.Close()
		defer origBody.Close()

		h := sha256.New()
		tee := io.TeeReader(origBody, h)

		// Copy body data to pipe while hashing
		if _, err := io.Copy(pw, tee); err != nil {
			reader.mu.Lock()
			reader.err = fmt.Errorf("failed to stream body: %w", err)
			reader.mu.Unlock()
			return
		}

		// Set trailer value after hash computed
		bodyHash := h.Sum(nil)
		hash := hashSignatureWithBodyHash(req, bodyHash, timestamp, nonce)
		sig, sigErr := ecdsa.SignASN1(rand.Reader, key, hash)
		if sigErr != nil {
			reader.mu.Lock()
			reader.err = fmt.Errorf("failed to sign request: %w", sigErr)
			reader.mu.Unlock()
			return
		}

		// Successfully computed signature
		req.Trailer.Set("Signature", base64.StdEncoding.EncodeToString(sig))
	}()

	// Set signature metadata headers
	req.Header.Set("X-Signature-Alg", "ecdsa-sha256")
	req.Header.Set("X-Signature-KeyID", keyID)
	req.Header.Set("X-Signature-Timestamp", timestamp)
	req.Header.Set("X-Signature-Nonce", nonce)

	return &SignatureContext{
		timestamp: timestamp,
		nonce:     nonce,
	}, nil
}

// hashSignatureWithBodyHash creates signature hash when body hash is already computed
func hashSignatureWithBodyHash(req *http.Request, bodyHash []byte, timestamp string, nonce string) []byte {
	h := sha256.New()

	addHash := func(label string, item string) {
		fmt.Fprintf(h, "%s: %s\n", label, item)
	}
	addHash("method", req.Method)
	addHash("uri", req.URL.RequestURI())
	addHash("timestamp", timestamp)
	addHash("nonce", nonce)
	addHash("body-sha256", hex.EncodeToString(bodyHash))

	return h.Sum(nil)
}

func VerifyHTTPRequest(key *ecdsa.PublicKey, req *http.Request, body []byte, checkNonce func(string) error) error {
	return VerifyHTTPRequestStreaming(key, req, bytes.NewReader(body), checkNonce)
}

func VerifyHTTPRequestStreaming(key *ecdsa.PublicKey, req *http.Request, bodyReader io.Reader, checkNonce func(string) error) error {
	hash, err := hashSignatureStreaming(req, bodyReader, req.Header.Get("X-Signature-Timestamp"), req.Header.Get("X-Signature-Nonce"))
	if err != nil {
		return err
	}
	return verifySignatureWithHash(key, req, hash, checkNonce)
}

// VerifyHTTPRequestSignatureWithBody verifies a streaming signature from request trailer without requiring SignatureContext.
// This allows verification to be decoupled from signing by reading and hashing the body on the verifier side.
func VerifyHTTPRequestSignatureWithBody(key *ecdsa.PublicKey, req *http.Request, bodyReader io.Reader, timestamp time.Time, nonce string, checkNonce func(string) error) error {
	// Read and hash the body on the verifier side
	bodyHash := sha256.New()
	if _, err := io.Copy(bodyHash, bodyReader); err != nil {
		return fmt.Errorf("failed to hash body: %w", err)
	}

	// Create signature hash using the computed body hash
	timestampStr := timestamp.Format(time.RFC3339Nano)
	hash := hashSignatureWithBodyHash(req, bodyHash.Sum(nil), timestampStr, nonce)

	// Get signature from request trailer
	b64Sig := req.Trailer.Get("Signature")
	if b64Sig == "" {
		return fmt.Errorf("missing signature in request trailer")
	}
	sig, err := base64.StdEncoding.DecodeString(b64Sig)
	if err != nil {
		return err
	}

	// Verify signature
	if !ecdsa.VerifyASN1(key, hash, sig) {
		return fmt.Errorf("invalid signature")
	}

	// Verify metadata using the provided timestamp and nonce
	return verifySignatureMetadataWithTime(req, timestamp, nonce, key, checkNonce)
}

// verifySignatureWithHash handles common verification logic when hash is already computed
func verifySignatureWithHash(key *ecdsa.PublicKey, req *http.Request, hash []byte, checkNonce func(string) error) error {
	timestamp := req.Header.Get("X-Signature-Timestamp")
	nonce := req.Header.Get("X-Signature-Nonce")

	b64Sig := req.Header.Get("Signature")
	if b64Sig == "" {
		return fmt.Errorf("missing signature")
	}
	sig, err := base64.StdEncoding.DecodeString(b64Sig)
	if err != nil {
		return err
	}

	if !ecdsa.VerifyASN1(key, hash, sig) {
		return fmt.Errorf("invalid signature")
	}

	return verifySignatureMetadata(req, timestamp, nonce, key, checkNonce)
}

// verifySignatureMetadata validates signature metadata (algorithm, key ID, timestamp, nonce)
func verifySignatureMetadata(req *http.Request, timestamp, nonce string, key *ecdsa.PublicKey, checkNonce func(string) error) error {
	// Validate algorithm
	alg := req.Header.Get("X-Signature-Alg")
	if alg != "ecdsa-sha256" {
		return fmt.Errorf("unsupported signature algorithm: %q", alg)
	}

	// Validate key ID
	kid := req.Header.Get("X-Signature-KeyID")
	if kid == "" {
		return fmt.Errorf("missing key id")
	}

	// Bind provided key to header to prevent mismatches
	if expected, err := keyIDfromECDSAPublic(key); err != nil {
		return err
	} else if kid != expected {
		return fmt.Errorf("key id mismatch")
	}

	if nonce == "" {
		return fmt.Errorf("missing nonce")
	}
	if checkNonce == nil {
		checkNonce = func(string) error { return nil }
	}
	if err := checkNonce(nonce); err != nil {
		return err
	}

	if timestamp == "" {
		return fmt.Errorf("missing timestamp")
	}

	// Parse and enforce timestamp skew
	ts, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}

	if skew := time.Since(ts); skew < -time.Minute || skew > time.Minute {
		return fmt.Errorf("timestamp outside acceptable skew")
	}

	return nil
}

// verifySignatureMetadataWithTime validates signature metadata using time.Time instead of string
func verifySignatureMetadataWithTime(req *http.Request, timestamp time.Time, nonce string, key *ecdsa.PublicKey, checkNonce func(string) error) error {
	// Validate algorithm
	alg := req.Header.Get("X-Signature-Alg")
	if alg != "ecdsa-sha256" {
		return fmt.Errorf("unsupported signature algorithm: %q", alg)
	}

	// Validate key ID
	kid := req.Header.Get("X-Signature-KeyID")
	if kid == "" {
		return fmt.Errorf("missing key id")
	}

	// Bind provided key to header to prevent mismatches
	if expected, err := keyIDfromECDSAPublic(key); err != nil {
		return err
	} else if kid != expected {
		return fmt.Errorf("key id mismatch")
	}

	// Enforce timestamp skew using the provided time.Time
	if skew := time.Since(timestamp); skew < -time.Minute || skew > time.Minute {
		return fmt.Errorf("timestamp outside acceptable skew")
	}

	if nonce == "" {
		return fmt.Errorf("missing nonce")
	}
	if checkNonce == nil {
		checkNonce = func(string) error { return nil }
	}
	if err := checkNonce(nonce); err != nil {
		return err
	}

	return nil
}
