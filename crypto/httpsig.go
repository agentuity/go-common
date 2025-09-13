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
	"hash"
	"io"
	"net/http"
	"time"

	gocstring "github.com/agentuity/go-common/string"
)

// bodyHashingReader wraps an io.ReadCloser and computes SHA256 hash as data is read
type bodyHashingReader struct {
	rc io.ReadCloser
	h  hash.Hash
}

func (b *bodyHashingReader) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	if n > 0 {
		b.h.Write(p[:n])
	}
	return n, err
}

func (b *bodyHashingReader) Close() error {
	return b.rc.Close()
}

// Sum returns the computed hash. Should only be called after reading is complete.
func (b *bodyHashingReader) Sum() []byte {
	return b.h.Sum(nil)
}

// SignatureContext holds the state needed to complete a signature after streaming
type SignatureContext struct {
	bodyHasher *bodyHashingReader
	timestamp  string
	nonce      string
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
		h.Write(fmt.Appendf([]byte{}, "%s: %s\n", label, item))
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

	// Wrap the request body with a hashing reader
	bodyHasher := &bodyHashingReader{
		rc: req.Body,
		h:  sha256.New(),
	}
	req.Body = bodyHasher

	// Set signature metadata headers (signature will be added later via trailer)
	req.Header.Set("X-Signature-Alg", "ecdsa-sha256")
	req.Header.Set("X-Signature-KeyID", keyID)
	req.Header.Set("X-Signature-Timestamp", timestamp)
	req.Header.Set("X-Signature-Nonce", nonce)
	req.Header.Set("Trailer", "Signature")

	return &SignatureContext{
		bodyHasher: bodyHasher,
		timestamp:  timestamp,
		nonce:      nonce,
	}, nil
}

// CompleteHTTPRequestSignature completes the signature after the request body has been streamed.
// This should be called after the body has been fully read (e.g., in an HTTP transport's response handler).
// The signature is set as an HTTP trailer.
func CompleteHTTPRequestSignature(key *ecdsa.PrivateKey, req *http.Request, ctx *SignatureContext, resp *http.Response) error {
	bodyHash := ctx.bodyHasher.Sum()

	// Create signature using the streamed body hash
	hash := hashSignatureWithBodyHash(req, bodyHash, ctx.timestamp, ctx.nonce)
	sig, err := ecdsa.SignASN1(rand.Reader, key, hash)
	if err != nil {
		return err
	}

	// Add signature to response trailer
	if resp.Trailer == nil {
		resp.Trailer = make(http.Header)
	}
	resp.Trailer.Set("Signature", base64.StdEncoding.EncodeToString(sig))

	// Announce signature trailer in response headers
	resp.Header.Add("Trailer", "Signature")

	return nil
}

// hashSignatureWithBodyHash creates signature hash when body hash is already computed
func hashSignatureWithBodyHash(req *http.Request, bodyHash []byte, timestamp string, nonce string) []byte {
	h := sha256.New()

	addHash := func(label string, item string) {
		h.Write(fmt.Appendf([]byte{}, "%s: %s\n", label, item))
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

// VerifyHTTPResponseSignature verifies a signature that was sent via HTTP trailers (for streaming requests)
func VerifyHTTPResponseSignature(key *ecdsa.PublicKey, req *http.Request, resp *http.Response, ctx *SignatureContext, checkNonce func(string) error) error {
	bodyHash := ctx.bodyHasher.Sum()
	hash := hashSignatureWithBodyHash(req, bodyHash, ctx.timestamp, ctx.nonce)

	// Get signature from trailer
	b64Sig := resp.Trailer.Get("Signature")
	if b64Sig == "" {
		return fmt.Errorf("missing signature in trailer")
	}
	sig, err := base64.StdEncoding.DecodeString(b64Sig)
	if err != nil {
		return err
	}

	if !ecdsa.VerifyASN1(key, hash, sig) {
		return fmt.Errorf("invalid signature")
	}

	return verifySignatureMetadata(req, ctx.timestamp, ctx.nonce, key, checkNonce)
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
