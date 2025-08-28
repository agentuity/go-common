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
	canon := fmt.Append(body, req.Method, req.URL.RequestURI(), timestamp, nonce)
	hash := sha256.Sum256(canon)
	return hash[:]
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

func VerifyHTTPRequest(key *ecdsa.PublicKey, req *http.Request, body []byte, checkNonce func() error) error {
	sig, err := base64.StdEncoding.DecodeString(req.Header.Get("Signature"))
	if err != nil {
		return err
	}
	timestamp := req.Header.Get("X-Signature-Timestamp")
	if timestamp == "" {
		return fmt.Errorf("missing timestamp")
	}
	nonce := req.Header.Get("X-Signature-Nonce")
	if nonce == "" {
		return fmt.Errorf("missing nonce")
	}
	if err := checkNonce(); err != nil {
		return err
	}
	hash := hashSignature(req, body, timestamp, nonce)
	if !ecdsa.VerifyASN1(key, hash[:], sig) {
		return fmt.Errorf("invalid signature")
	}
	return nil
}
