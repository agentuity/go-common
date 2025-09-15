package crypto

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"sync"
	"time"

	gocstring "github.com/agentuity/go-common/string"
)

const (
	HeaderSignature           = "Signature"
	HeaderSignatureAlg        = "X-Signature-Alg"
	HeaderSignatureKeyID      = "X-Signature-KeyID"
	HeaderSignatureTimestamp  = "X-Signature-Timestamp"
	HeaderSignatureNonce      = "X-Signature-Nonce"
	SignatureAlgorithmECDSA   = "ecdsa-sha256"
	SignatureECDSAKeyIDPrefix = "k1-ecdsa-"
)

// Predefined errors for better error handling and testing
var (
	ErrSignatureComputationTimeout = errors.New("signature computation timeout")
)

func keyIDfromECDSAPublic(pub *ecdsa.PublicKey) (string, error) {
	spki, err := x509.MarshalPKIXPublicKey(pub) // DER-encoded SubjectPublicKeyInfo
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(spki)
	id := sum[:16] // 128-bit truncated ID (first 16 bytes)
	return SignatureECDSAKeyIDPrefix + hex.EncodeToString(id), nil
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

// SignHTTPRequest signs an HTTP request with the given private key and body and sets the signature headers in the request.
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

	req.Header.Set(HeaderSignature, base64.StdEncoding.EncodeToString(sig))
	req.Header.Set(HeaderSignatureAlg, SignatureAlgorithmECDSA)
	req.Header.Set(HeaderSignatureKeyID, keyID)
	req.Header.Set(HeaderSignatureTimestamp, timestamp)
	req.Header.Set(HeaderSignatureNonce, nonce)
	return nil
}

// synchronousSigningReader implements synchronous signature computation to prevent race conditions
type synchronousSigningReader struct {
	reader    io.ReadCloser
	hasher    hash.Hash
	key       *ecdsa.PrivateKey
	req       *http.Request
	timestamp string
	nonce     string
	signed    bool
	mu        sync.Mutex
}

func (s *synchronousSigningReader) Read(p []byte) (n int, err error) {
	n, err = s.reader.Read(p)
	if n > 0 {
		s.hasher.Write(p[:n])
	}

	// On EOF, compute and set signature synchronously
	if err == io.EOF {
		s.mu.Lock()
		if !s.signed {
			if err := s.setSignature(); err != nil {
				s.mu.Unlock()
				return 0, err
			}
			s.signed = true
		}
		s.mu.Unlock()
	}

	return n, err
}

func (s *synchronousSigningReader) Close() error {
	// Ensure signature is set even if EOF wasn't reached normally
	s.mu.Lock()
	if !s.signed {
		if err := s.setSignature(); err != nil {
			s.mu.Unlock()
			return err
		}
		s.signed = true
	}
	s.mu.Unlock()

	if s.reader != nil {
		return s.reader.Close()
	}
	return nil
}

func (s *synchronousSigningReader) setSignature() error {
	bodyHash := s.hasher.Sum(nil)
	hash := hashSignatureWithBodyHash(s.req, bodyHash, s.timestamp, s.nonce)
	sig, err := ecdsa.SignASN1(rand.Reader, s.key, hash)
	if err != nil {
		return err
	}

	// Set the signature in the trailer
	s.req.Trailer.Set(HeaderSignature, base64.StdEncoding.EncodeToString(sig))
	return nil
}

// PrepareHTTPRequestForStreaming sets up a request for streaming signature.
func PrepareHTTPRequestForStreaming(key *ecdsa.PrivateKey, req *http.Request) error {
	keyID, err := keyIDfromECDSAPublic(&key.PublicKey)
	if err != nil {
		return err
	}

	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	nonce, err := gocstring.GenerateRandomString(16)
	if err != nil {
		return fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Handle nil req.Body by treating it as http.NoBody
	origBody := req.Body
	if origBody == nil {
		origBody = http.NoBody
	}

	// Announce trailer via request headers and set placeholder
	req.Header.Set("Trailer", HeaderSignature)
	req.Trailer = make(http.Header)
	req.Trailer.Set(HeaderSignature, "")

	// Enable chunked encoding for HTTP/1.1 (net/http will handle HTTP/2 appropriately)
	req.ContentLength = -1
	req.Header.Del("Content-Length") // Remove Content-Length header to avoid conflicts

	// Create synchronous signing reader that computes signature during Read/Close
	req.Body = &synchronousSigningReader{
		reader:    origBody,
		hasher:    sha256.New(),
		key:       key,
		req:       req,
		timestamp: timestamp,
		nonce:     nonce,
		signed:    false,
	}

	// Set signature metadata headers
	req.Header.Set(HeaderSignatureAlg, SignatureAlgorithmECDSA)
	req.Header.Set(HeaderSignatureKeyID, keyID)
	req.Header.Set(HeaderSignatureTimestamp, timestamp)
	req.Header.Set(HeaderSignatureNonce, nonce)

	return nil
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

// VerifyHTTPRequest verifies a request and body against the provided HTTP signature headers to ensure that the request is valid and the body matches the signature.
func VerifyHTTPRequest(key *ecdsa.PublicKey, req *http.Request, body []byte, checkNonce func(string) error) error {
	return VerifyHTTPRequestStreaming(key, req, bytes.NewReader(body), checkNonce)
}

// VerifyHTTPRequestStreaming verifies a request and body against the provided HTTP signature headers to ensure that the request is valid and the body matches the signature.
func VerifyHTTPRequestStreaming(key *ecdsa.PublicKey, req *http.Request, bodyReader io.Reader, checkNonce func(string) error) error {
	hash, err := hashSignatureStreaming(req, bodyReader, req.Header.Get(HeaderSignatureTimestamp), req.Header.Get(HeaderSignatureNonce))
	if err != nil {
		return err
	}
	signature := req.Trailer.Get(HeaderSignature)
	if signature == "" {
		signature = req.Header.Get(HeaderSignature)
		if signature == "" {
			return fmt.Errorf("missing header or trailer: %s", HeaderSignature)
		}
	}
	return verifySignatureWithHash(key, req, hash, checkNonce, signature)
}

// VerifyHTTPRequestSignatureWithBody verifies a request and body against the provided HTTP signature headers to ensure that the request is valid and the body matches the signature.
func VerifyHTTPRequestSignatureWithBody(key *ecdsa.PublicKey, req *http.Request, bodyReader io.Reader, timestamp time.Time, nonce string, checkNonce func(string) error) error {
	// Guard against misuse: cross-check provided metadata vs headers
	if ht := req.Header.Get(HeaderSignatureTimestamp); ht != "" && ht != timestamp.Format(time.RFC3339Nano) {
		return fmt.Errorf("timestamp mismatch between header (%s) and parameter (%s)", ht, timestamp.Format(time.RFC3339Nano))
	}
	if hn := req.Header.Get(HeaderSignatureNonce); hn != "" && hn != nonce {
		return fmt.Errorf("nonce mismatch between header (%s) and parameter (%s)", hn, nonce)
	}

	// Read and hash the body on the verifier side
	bodyHash := sha256.New()
	if _, err := io.Copy(bodyHash, bodyReader); err != nil {
		return fmt.Errorf("failed to hash body: %w", err)
	}

	// Create signature hash using the computed body hash
	timestampStr := timestamp.Format(time.RFC3339Nano)
	hash := hashSignatureWithBodyHash(req, bodyHash.Sum(nil), timestampStr, nonce)

	// Get signature from request trailer
	b64Sig := req.Trailer.Get(HeaderSignature)
	if b64Sig == "" {
		return fmt.Errorf("missing %s in request trailer", HeaderSignature)
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
func verifySignatureWithHash(key *ecdsa.PublicKey, req *http.Request, hash []byte, checkNonce func(string) error, signature string) error {
	timestamp := req.Header.Get(HeaderSignatureTimestamp)
	nonce := req.Header.Get(HeaderSignatureNonce)

	sig, err := base64.StdEncoding.DecodeString(signature)
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
	alg := req.Header.Get(HeaderSignatureAlg)
	if alg != SignatureAlgorithmECDSA {
		return fmt.Errorf("unsupported signature algorithm: %q", alg)
	}

	// Validate key ID
	kid := req.Header.Get(HeaderSignatureKeyID)
	if kid == "" {
		return fmt.Errorf("missing %s", HeaderSignatureKeyID)
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
	alg := req.Header.Get(HeaderSignatureAlg)
	if alg != SignatureAlgorithmECDSA {
		return fmt.Errorf("unsupported signature algorithm: %q", alg)
	}

	// Validate key ID
	kid := req.Header.Get(HeaderSignatureKeyID)
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
