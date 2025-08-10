package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
)

func TestParseAndValidateEd25519PrivateKey(t *testing.T) {
	// Generate a test ed25519 key pair
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate ed25519 key pair: %v", err)
	}

	tests := []struct {
		name      string
		keyData   []byte
		expectErr bool
	}{
		{
			name:      "Valid raw private key",
			keyData:   privateKey,
			expectErr: false,
		},
		{
			name:      "Invalid raw private key - wrong size",
			keyData:   make([]byte, 32), // ed25519 private key should be 64 bytes
			expectErr: true,
		},
		{
			name:      "Empty key data",
			keyData:   []byte{},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseAndValidateEd25519PrivateKey(tt.keyData)
			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			if len(parsed) != ed25519.PrivateKeySize {
				t.Errorf("Expected private key size %d, got %d", ed25519.PrivateKeySize, len(parsed))
			}
		})
	}
}

func TestParseAndValidateEd25519PrivateKeyPEM(t *testing.T) {
	// Generate a test ed25519 key pair
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate ed25519 key pair: %v", err)
	}

	// Convert to PKCS#8 format
	pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("Failed to marshal private key: %v", err)
	}

	// Create PEM block
	pemBlock := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: pkcs8Bytes,
	}
	pemData := pem.EncodeToMemory(pemBlock)

	parsed, err := ParseAndValidateEd25519PrivateKey(pemData)
	if err != nil {
		t.Errorf("Failed to parse PEM private key: %v", err)
		return
	}

	if len(parsed) != ed25519.PrivateKeySize {
		t.Errorf("Expected private key size %d, got %d", ed25519.PrivateKeySize, len(parsed))
	}

	// Verify the parsed key matches the original
	if !ed25519.PrivateKey(parsed).Equal(privateKey) {
		t.Errorf("Parsed private key does not match original")
	}
}

func TestExtractEd25519PublicKeyAsPEM(t *testing.T) {
	// Generate a test ed25519 key pair
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate ed25519 key pair: %v", err)
	}

	// Extract public key as PEM
	pemData, err := ExtractEd25519PublicKeyAsPEM(privateKey)
	if err != nil {
		t.Errorf("Failed to extract public key as PEM: %v", err)
		return
	}

	// Verify it's valid PEM
	block, _ := pem.Decode(pemData)
	if block == nil {
		t.Errorf("Failed to decode PEM data")
		return
	}

	if block.Type != "PUBLIC KEY" {
		t.Errorf("Expected PEM block type 'PUBLIC KEY', got '%s'", block.Type)
	}

	// Parse the public key from PEM
	parsedPublicKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Errorf("Failed to parse public key from PEM: %v", err)
		return
	}

	// Verify it's an ed25519 public key
	ed25519PublicKey, ok := parsedPublicKey.(ed25519.PublicKey)
	if !ok {
		t.Errorf("Parsed public key is not ed25519.PublicKey")
		return
	}

	// Verify the extracted public key matches the original
	if !ed25519PublicKey.Equal(publicKey) {
		t.Errorf("Extracted public key does not match original")
	}

	// Verify PEM format contains expected headers
	pemString := string(pemData)
	if !strings.Contains(pemString, "-----BEGIN PUBLIC KEY-----") {
		t.Errorf("PEM data missing BEGIN header")
	}
	if !strings.Contains(pemString, "-----END PUBLIC KEY-----") {
		t.Errorf("PEM data missing END header")
	}
}

func TestExtractEd25519PublicKeyAsPEMInvalidKey(t *testing.T) {
	// Test with invalid private key
	invalidKey := make([]byte, 32) // Wrong size

	_, err := ExtractEd25519PublicKeyAsPEM(invalidKey)
	if err == nil {
		t.Errorf("Expected error for invalid private key size")
	}
}

func TestRoundTrip(t *testing.T) {
	// Generate a test ed25519 key pair
	originalPublicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate ed25519 key pair: %v", err)
	}

	// Parse and validate the private key
	parsedPrivateKey, err := ParseAndValidateEd25519PrivateKey(privateKey)
	if err != nil {
		t.Errorf("Failed to parse private key: %v", err)
		return
	}

	// Extract public key as PEM
	pemData, err := ExtractEd25519PublicKeyAsPEM(parsedPrivateKey)
	if err != nil {
		t.Errorf("Failed to extract public key as PEM: %v", err)
		return
	}

	// Parse the public key back from PEM
	block, _ := pem.Decode(pemData)
	if block == nil {
		t.Errorf("Failed to decode PEM data")
		return
	}

	parsedPublicKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Errorf("Failed to parse public key from PEM: %v", err)
		return
	}

	ed25519PublicKey, ok := parsedPublicKey.(ed25519.PublicKey)
	if !ok {
		t.Errorf("Parsed public key is not ed25519.PublicKey")
		return
	}

	// Verify the round-trip preserved the original public key
	if !ed25519PublicKey.Equal(originalPublicKey) {
		t.Errorf("Round-trip did not preserve original public key")
	}
}
