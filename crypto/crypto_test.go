package crypto

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateRSAKeyPair(t *testing.T) {
	tests := []struct {
		name    string
		bits    int
		wantErr bool
	}{
		{
			name:    "valid 2048 bits",
			bits:    2048,
			wantErr: false,
		},
		{
			name:    "valid 4096 bits",
			bits:    4096,
			wantErr: false,
		},
		{
			name:    "invalid bits",
			bits:    1,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			privKey, pubKey, err := GenerateRSAKeyPair(tt.bits)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateRSAKeyPair() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if privKey == nil {
					t.Error("GenerateRSAKeyPair() privateKey is nil")
				}
				if pubKey == nil {
					t.Error("GenerateRSAKeyPair() publicKey is nil")
				}
			}
		})
	}
}

func TestEncodePrivateKeyToPEM(t *testing.T) {
	privKey, _, err := GenerateRSAKeyPair(2048)
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}

	pemData := EncodePrivateKeyToPEM(privKey)
	if len(pemData) == 0 {
		t.Error("EncodePrivateKeyToPEM() returned empty PEM data")
	}

	if !strings.Contains(string(pemData), "-----BEGIN PRIVATE KEY-----") {
		t.Error("EncodePrivateKeyToPEM() PEM data doesn't contain expected header")
	}
}

func TestEncodePublicKeyToPEM(t *testing.T) {
	_, pubKey, err := GenerateRSAKeyPair(2048)
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}

	pemData := EncodePublicKeyToPEM(pubKey)
	if len(pemData) == 0 {
		t.Error("EncodePublicKeyToPEM() returned empty PEM data")
	}

	if !strings.Contains(string(pemData), "-----BEGIN PUBLIC KEY-----") {
		t.Error("EncodePublicKeyToPEM() PEM data doesn't contain expected header")
	}
}

func TestEncryptDecryptWithRSA(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{
			name:    "valid data",
			data:    []byte("hello world"),
			wantErr: false,
		},
		{
			name:    "empty data",
			data:    []byte{},
			wantErr: true,
		},
		{
			name:    "long data",
			data:    bytes.Repeat([]byte("a"), 190), // Should be less than key size
			wantErr: false,
		},
	}

	privKey, pubKey, err := GenerateRSAKeyPair(2048)
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test encryption
			encrypted, err := EncryptWithRSA(pubKey, tt.data, []byte("test"))
			if (err != nil) != tt.wantErr {
				t.Errorf("EncryptWithRSA() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			// Test decryption
			decrypted, err := DecryptWithRSA(privKey, encrypted, []byte("test"))
			if err != nil {
				t.Errorf("DecryptWithRSA() error = %v", err)
				return
			}

			// Compare original and decrypted data
			if !bytes.Equal(tt.data, decrypted) {
				t.Errorf("DecryptWithRSA() = %v, want %v", decrypted, tt.data)
			}
		})
	}
}

func TestDecryptWithRSA_InvalidInput(t *testing.T) {
	privKey, _, err := GenerateRSAKeyPair(2048)
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}

	_, err = DecryptWithRSA(privKey, []byte{}, []byte("test"))
	if err == nil {
		t.Error("DecryptWithRSA() should return error for empty ciphertext")
	}

	_, err = DecryptWithRSA(privKey, []byte("invalid ciphertext"), []byte("test"))
	if err == nil {
		t.Error("DecryptWithRSA() should return error for invalid ciphertext")
	}
}

func TestKeyPairFileIO(t *testing.T) {
	// Create temporary directory for test files
	tempDir := t.TempDir()
	privateKeyPath := filepath.Join(tempDir, "private.pem")
	publicKeyPath := filepath.Join(tempDir, "public.pem")

	// Test cases
	tests := []struct {
		name    string
		keyBits int
	}{
		{"2048-bit RSA Key", 2048},
		{"4096-bit RSA Key", 4096},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Generate a key pair
			privateKey, _, err := GenerateRSAKeyPair(tt.keyBits)
			if err != nil {
				t.Fatalf("Failed to generate RSA key pair: %v", err)
			}

			// Write keys to files
			err = WriteKeyPairToFiles(privateKey, privateKeyPath, publicKeyPath)
			if err != nil {
				t.Fatalf("Failed to write key pair to files: %v", err)
			}

			// Check file permissions
			privateStat, err := os.Stat(privateKeyPath)
			if err != nil {
				t.Fatalf("Failed to stat private key file: %v", err)
			}
			if privateStat.Mode().Perm() != 0600 {
				t.Errorf("Private key file has wrong permissions. Got: %v, Want: %v",
					privateStat.Mode().Perm(), 0600)
			}

			publicStat, err := os.Stat(publicKeyPath)
			if err != nil {
				t.Fatalf("Failed to stat public key file: %v", err)
			}
			if publicStat.Mode().Perm() != 0644 {
				t.Errorf("Public key file has wrong permissions. Got: %v, Want: %v",
					publicStat.Mode().Perm(), 0644)
			}

			// Read keys back from files
			readPrivateKey, err := ReadPrivateKeyFromFile(privateKeyPath)
			if err != nil {
				t.Fatalf("Failed to read private key from file: %v", err)
			}

			readPublicKey, err := ReadPublicKeyFromFile(publicKeyPath)
			if err != nil {
				t.Fatalf("Failed to read public key from file: %v", err)
			}

			// Test encryption/decryption with read keys to verify they work
			testMessage := []byte("test message for encryption")
			encrypted, err := EncryptWithRSA(readPublicKey, testMessage, []byte("foobar"))
			if err != nil {
				t.Fatalf("Failed to encrypt test message: %v", err)
			}

			decrypted, err := DecryptWithRSA(readPrivateKey, encrypted, []byte("foobar"))
			if err != nil {
				t.Fatalf("Failed to decrypt test message: %v", err)
			}

			if string(decrypted) != string(testMessage) {
				t.Errorf("Decrypted message doesn't match original. Got: %s, Want: %s",
					string(decrypted), string(testMessage))
			}
		})
	}
}

func TestReadKeyPairFromNonExistentFile(t *testing.T) {
	nonExistentPath := "/path/that/does/not/exist/key.pem"

	// Test reading non-existent private key
	_, err := ReadPrivateKeyFromFile(nonExistentPath)
	if err == nil {
		t.Error("Expected error when reading non-existent private key file, got nil")
	}

	// Test reading non-existent public key
	_, err = ReadPublicKeyFromFile(nonExistentPath)
	if err == nil {
		t.Error("Expected error when reading non-existent public key file, got nil")
	}
}

func TestReadInvalidKeyFiles(t *testing.T) {
	// Create temporary directory for test files
	tempDir := t.TempDir()
	invalidKeyPath := filepath.Join(tempDir, "invalid.pem")

	// Write invalid data to file
	invalidData := []byte("invalid key data")
	err := os.WriteFile(invalidKeyPath, invalidData, 0600)
	if err != nil {
		t.Fatalf("Failed to write invalid key file: %v", err)
	}

	// Test reading invalid private key
	_, err = ReadPrivateKeyFromFile(invalidKeyPath)
	if err == nil {
		t.Error("Expected error when reading invalid private key file, got nil")
	}

	// Test reading invalid public key
	_, err = ReadPublicKeyFromFile(invalidKeyPath)
	if err == nil {
		t.Error("Expected error when reading invalid public key file, got nil")
	}
}

func TestPEMEncodingAndCrypto(t *testing.T) {
	// Test private key in PEM format
	// generated with:
	// openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:4096 -out private.pem
	//
	testPrivateKeyPEM := []byte(`-----BEGIN PRIVATE KEY-----
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
6hG43ZPf9LDiEdQiCEPjLxLiSJAOZeY=
-----END PRIVATE KEY-----
`)

	// Step 1: Decode the private key from PEM
	privateKey, err := PrivateKeyFromBase64(testPrivateKeyPEM)
	if err != nil {
		t.Fatalf("Failed to decode private key: %v", err)
	}

	// Step 2: Re-encode the private key to PEM
	reEncodedPEM := EncodePrivateKeyToPEM(privateKey)

	// Step 3: Verify that the re-encoded PEM matches the original
	if !bytes.Equal(testPrivateKeyPEM, reEncodedPEM) {
		t.Error("Re-encoded PEM does not match original PEM")
	}

	// Step 4: Test encryption and decryption
	testMessage := []byte("Hello, this is a test message!")

	// Test the public key encoded PEM from the command line:
	// openssl rsa -in private.pem -pubout -out public.pem
	// the private.pem is from the previous step

	block, _ := pem.Decode([]byte(`-----BEGIN PUBLIC KEY-----
MIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEA1GbsMp5bj5LlnryItla6
4GQDpM3M6hRMG/cY4uoQ4kuWrLf720fIr0EVTytAptKWaLsy335jeNP5YdybNc/2
Q/MRbpfGpTvQQ2QyoQb3anWKlgPw6xd2KKREj41YewTFG+/LSJWf9IOFmYai8r4w
wZ4IKi+XmfRnabwxf1nyNeuC3iSFG/inrR3TraesB7c/upVW+EPBtlehVzS4hH6e
QKIOlqyCGOj9FapelVKkCuUw6d/tUzOY/XOa17UC72ZDAnMbjj9R3z8m8JLFb8wU
jKOvzQlsLgUBrbfPmh+Olu8EvbnYvyyjrSDDCSbl55KgvO9U1ihVZix+3iN72I4M
RLHGjMKNvivo7ofGrobq/ei655i2JJC6qY4XKONpOpaSTCrRhE96FpSAd2ftTJZb
mVDkA7E6hJHI87s3m+NS3UsXJEuG+L3WSpeoWgCJIDZFU4EcHiRWzUcCwKRqrg5j
fVNLiFEDoC6PGqFVRyRC2DVx2qBBb+h1ZFLUeeBWLTD87sm7a55w4FwXO7OM+sBV
AnW1+XfeCSA8C2NJ/MsLslyMMkI2iEwX6aA8bGI8J9922t0/8FO1ZlB8Q+88xcyo
CbhI7cFzeG5TlD9BuA387K4//4fVsJVUrMJtrUXq0IRf8+gDjbb23S17CvHnbeJi
0U5CyHcDIQU5XtUHXQscN9UCAwEAAQ==
-----END PUBLIC KEY-----
`))
	if block == nil || block.Type != "PUBLIC KEY" {
		t.Fatal("Failed to decode public key PEM block")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatalf("Failed to parse public key: %v", err)
	}

	pubKey, ok := pub.(*rsa.PublicKey)
	if !ok {
		t.Fatal("Parsed key is not an RSA public key")
	}

	// Test encryption with decoded public key
	encryptedWithDecodedKey, err := EncryptWithRSA(pubKey, testMessage, []byte("x"))
	if err != nil {
		t.Fatalf("Failed to encrypt with decoded public key: %v", err)
	}

	// Decrypt using original private key
	decryptedFromDecodedKey, err := DecryptWithRSA(privateKey, encryptedWithDecodedKey, []byte("x"))
	if err != nil {
		t.Fatalf("Failed to decrypt message encrypted with decoded key: %v", err)
	}

	if !bytes.Equal(testMessage, decryptedFromDecodedKey) {
		t.Error("Decrypted message from decoded key does not match original message")
	}

	// Encrypt using the public key derived from the private key
	encrypted, err := EncryptWithRSA(&privateKey.PublicKey, testMessage, []byte(""))
	if err != nil {
		t.Fatalf("Failed to encrypt message: %v", err)
	}

	// Decrypt using the private key
	decrypted, err := DecryptWithRSA(privateKey, encrypted, []byte(""))
	if err != nil {
		t.Fatalf("Failed to decrypt message: %v", err)
	}

	// Verify the decrypted message matches the original
	if !bytes.Equal(testMessage, decrypted) {
		t.Error("Decrypted message does not match original message")
	}
}
