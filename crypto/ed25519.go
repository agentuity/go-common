package crypto

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

// ParseAndValidateEd25519PrivateKey parses and validates an ed25519 private key
// and returns the ed25519.PrivateKey object. The input can be either raw bytes
// or PEM-encoded private key.
func ParseAndValidateEd25519PrivateKey(keyData []byte) (ed25519.PrivateKey, error) {
	var privateKey ed25519.PrivateKey

	// Try to decode as PEM first
	if block, _ := pem.Decode(keyData); block != nil {
		switch block.Type {
		case "PRIVATE KEY":
			// PKCS#8 format
			key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("failed to parse PKCS#8 private key: %w", err)
			}

			var ok bool
			privateKey, ok = key.(ed25519.PrivateKey)
			if !ok {
				return nil, fmt.Errorf("key is not an ed25519 private key")
			}
		case "ED25519 PRIVATE KEY":
			// Ed25519 specific format
			key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("failed to parse ed25519 private key: %w", err)
			}

			var ok bool
			privateKey, ok = key.(ed25519.PrivateKey)
			if !ok {
				return nil, fmt.Errorf("key is not an ed25519 private key")
			}
		default:
			return nil, fmt.Errorf("unsupported PEM block type: %s", block.Type)
		}
	} else {
		// Try to parse as raw ed25519 private key
		if len(keyData) != ed25519.PrivateKeySize {
			return nil, fmt.Errorf("invalid ed25519 private key size: expected %d bytes, got %d", ed25519.PrivateKeySize, len(keyData))
		}
		privateKey = ed25519.PrivateKey(keyData)
	}

	// Validate the private key by checking its length
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid ed25519 private key size: expected %d bytes, got %d", ed25519.PrivateKeySize, len(privateKey))
	}

	return privateKey, nil
}

// ExtractEd25519PublicKeyAsPEM extracts the public key from an ed25519 private key
// and returns it in PEM format.
func ExtractEd25519PublicKeyAsPEM(privateKey ed25519.PrivateKey) ([]byte, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid ed25519 private key size: expected %d bytes, got %d", ed25519.PrivateKeySize, len(privateKey))
	}

	// Extract the public key
	publicKey := privateKey.Public().(ed25519.PublicKey)

	// Marshal the public key in PKIX format
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal public key: %w", err)
	}

	// Create PEM block
	pemBlock := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	}

	// Encode to PEM format
	return pem.EncodeToMemory(pemBlock), nil
}
