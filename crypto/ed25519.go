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
		// Check for encrypted PEM blocks
		if x509.IsEncryptedPEMBlock(block) {
			return nil, fmt.Errorf("encrypted PEM blocks are not supported")
		}

		switch block.Type {
		case "PRIVATE KEY", "ED25519 PRIVATE KEY":
			// Parse as PKCS#8 format (both standard and non-standard ED25519 types)
			key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("failed to parse PKCS#8 private key (type: %s): %w", block.Type, err)
			}

			var ok bool
			privateKey, ok = key.(ed25519.PrivateKey)
			if !ok {
				return nil, fmt.Errorf("PKCS#8 key is not an ed25519 private key, got %T", key)
			}
		default:
			return nil, fmt.Errorf("unsupported PEM block type: %s (expected PRIVATE KEY)", block.Type)
		}
	} else {
		// Try to parse as raw ed25519 private key or seed
		switch len(keyData) {
		case ed25519.SeedSize: // 32 bytes
			// Generate full private key from seed
			privateKey = ed25519.NewKeyFromSeed(keyData)
		case ed25519.PrivateKeySize: // 64 bytes
			// Use as complete private key
			privateKey = ed25519.PrivateKey(keyData)
		default:
			return nil, fmt.Errorf("invalid ed25519 key size: expected %d bytes (seed) or %d bytes (private key), got %d", ed25519.SeedSize, ed25519.PrivateKeySize, len(keyData))
		}
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
