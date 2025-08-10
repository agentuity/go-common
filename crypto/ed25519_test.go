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
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate ed25519 key pair: %v", err)
	}

	// Extract the seed from the private key
	seed := privateKey.Seed()

	tests := []struct {
		name          string
		keyData       []byte
		expectErr     bool
		errorContains string
	}{
		{
			name:      "Valid raw private key (64 bytes)",
			keyData:   privateKey,
			expectErr: false,
		},
		{
			name:      "Valid raw seed (32 bytes)",
			keyData:   seed,
			expectErr: false,
		},
		{
			name:          "Invalid size - 31 bytes",
			keyData:       make([]byte, 31),
			expectErr:     true,
			errorContains: "invalid ed25519 key size",
		},
		{
			name:          "Invalid size - 33 bytes",
			keyData:       make([]byte, 33),
			expectErr:     true,
			errorContains: "invalid ed25519 key size",
		},
		{
			name:          "Invalid size - 63 bytes",
			keyData:       make([]byte, 63),
			expectErr:     true,
			errorContains: "invalid ed25519 key size",
		},
		{
			name:          "Invalid size - 65 bytes",
			keyData:       make([]byte, 65),
			expectErr:     true,
			errorContains: "invalid ed25519 key size",
		},
		{
			name:          "Empty key data",
			keyData:       []byte{},
			expectErr:     true,
			errorContains: "invalid ed25519 key size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseAndValidateEd25519PrivateKey(tt.keyData)
			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain '%s', got '%s'", tt.errorContains, err.Error())
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

			// Verify the derived public key matches when using seed
			derivedPublicKey := parsed.Public().(ed25519.PublicKey)
			if !derivedPublicKey.Equal(publicKey) {
				t.Errorf("Derived public key does not match original")
			}
		})
	}
}

func TestSeedToPrivateKeyConversion(t *testing.T) {
	// Test that a 32-byte seed correctly generates the expected 64-byte private key
	publicKey, originalPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate ed25519 key pair: %v", err)
	}

	seed := originalPrivateKey.Seed()

	// Parse the seed through our function
	parsedPrivateKey, err := ParseAndValidateEd25519PrivateKey(seed)
	if err != nil {
		t.Fatalf("Failed to parse seed: %v", err)
	}

	// Verify the derived private key matches the original
	if !parsedPrivateKey.Equal(originalPrivateKey) {
		t.Errorf("Private key derived from seed does not match original")
	}

	// Verify the public keys match
	parsedPublicKey := parsedPrivateKey.Public().(ed25519.PublicKey)
	if !parsedPublicKey.Equal(publicKey) {
		t.Errorf("Public key derived from seed does not match original")
	}

	// Test direct seed generation for consistency
	directPrivateKey := ed25519.NewKeyFromSeed(seed)
	if !directPrivateKey.Equal(parsedPrivateKey) {
		t.Errorf("Direct seed generation differs from parsed result")
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

	tests := []struct {
		name          string
		pemType       string
		expectErr     bool
		errorContains string
	}{
		{
			name:      "Valid PRIVATE KEY PEM",
			pemType:   "PRIVATE KEY",
			expectErr: false,
		},
		{
			name:      "Valid ED25519 PRIVATE KEY PEM (non-standard)",
			pemType:   "ED25519 PRIVATE KEY",
			expectErr: false,
		},
		{
			name:          "Invalid PEM type",
			pemType:       "RSA PRIVATE KEY",
			expectErr:     true,
			errorContains: "unsupported PEM block type",
		},
		{
			name:          "Invalid PEM type - PUBLIC KEY",
			pemType:       "PUBLIC KEY",
			expectErr:     true,
			errorContains: "unsupported PEM block type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create PEM block with specific type
			pemBlock := &pem.Block{
				Type:  tt.pemType,
				Bytes: pkcs8Bytes,
			}
			pemData := pem.EncodeToMemory(pemBlock)

			parsed, err := ParseAndValidateEd25519PrivateKey(pemData)
			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain '%s', got '%s'", tt.errorContains, err.Error())
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

			// Verify the parsed key matches the original
			if !ed25519.PrivateKey(parsed).Equal(privateKey) {
				t.Errorf("Parsed private key does not match original")
			}
		})
	}
}

func TestParseAndValidateEd25519PrivateKeyPEMErrors(t *testing.T) {
	tests := []struct {
		name          string
		pemData       []byte
		expectErr     bool
		errorContains string
	}{
		{
			name: "Invalid PKCS#8 data",
			pemData: pem.EncodeToMemory(&pem.Block{
				Type:  "PRIVATE KEY",
				Bytes: []byte("invalid pkcs8 data"),
			}),
			expectErr:     true,
			errorContains: "failed to parse PKCS#8 private key",
		},
		{
			name: "Non-ED25519 key in PKCS#8",
			pemData: func() []byte {
				// Create an RSA key and marshal it as PKCS#8
				// This will fail the ed25519 type assertion
				return pem.EncodeToMemory(&pem.Block{
					Type:  "PRIVATE KEY",
					Bytes: []byte{0x30, 0x82}, // Invalid but starts like PKCS#8
				})
			}(),
			expectErr:     true,
			errorContains: "failed to parse PKCS#8 private key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseAndValidateEd25519PrivateKey(tt.pemData)
			if !tt.expectErr {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				return
			}

			if err == nil {
				t.Errorf("Expected error but got none")
			} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
				t.Errorf("Expected error to contain '%s', got '%s'", tt.errorContains, err.Error())
			}
		})
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
