package crypto

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"
)

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

func SignHTTPRequest(key *ecdsa.PrivateKey, req *http.Request, body []byte) error {
	keyID, err := keyIDfromECDSAPublic(&key.PublicKey)
	if err != nil {
		return err
	}
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	nonce := rand.Text()

	sig, err := ecdsa.SignASN1(rand.Reader, key, hashSignature(req, body, timestamp, nonce))
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

func VerifyHTTPRequest(key *ecdsa.PublicKey, req *http.Request, body []byte, checkNonce func(string) error) error {
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

	b64Sig := req.Header.Get("Signature")
	if b64Sig == "" {
		return fmt.Errorf("missing signature")
	}
	sig, err := base64.StdEncoding.DecodeString(b64Sig)
	if err != nil {
		return err
	}

	timestamp := req.Header.Get("X-Signature-Timestamp")
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

	nonce := req.Header.Get("X-Signature-Nonce")
	if nonce == "" {
		return fmt.Errorf("missing nonce")
	}
	if checkNonce == nil {
		checkNonce = func(string) error { return nil }
	}
	if err := checkNonce(nonce); err != nil {
		return err
	}
	hash := hashSignature(req, body, timestamp, nonce)
	if !ecdsa.VerifyASN1(key, hash[:], sig) {
		return fmt.Errorf("invalid signature")
	}
	return nil
}
