package crypto

import (
	"bytes"
	"crypto/ecdh"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateKeyPair(t *testing.T) {
	keyPair, err := GenerateKeyPair()
	if err != nil {
		t.Errorf("GenerateKeyPair() error = %v", err)
		return
	}
	if keyPair.PrivateKey == nil {
		t.Error("GenerateKeyPair() privateKey is nil")
	}
	if keyPair.PublicKey == nil {
		t.Error("GenerateKeyPair() publicKey is nil")
	}
}

func TestEncodePrivateKeyToPEM(t *testing.T) {
	keyPair, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}

	pemData, err := EncodePrivateKeyToPEM(keyPair.PrivateKey)
	if err != nil {
		t.Fatalf("Failed to encode private key: %v", err)
	}
	if len(pemData) == 0 {
		t.Error("EncodePrivateKeyToPEM() returned empty PEM data")
	}

	if !strings.Contains(string(pemData), "-----BEGIN PRIVATE KEY-----") {
		t.Error("EncodePrivateKeyToPEM() PEM data doesn't contain expected header")
	}
}

func TestEncodePublicKeyToPEM(t *testing.T) {
	keyPair, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}

	pemData, err := EncodePublicKeyToPEM(keyPair.PublicKey)
	if err != nil {
		t.Fatalf("Failed to encode public key: %v", err)
	}
	if len(pemData) == 0 {
		t.Error("EncodePublicKeyToPEM() returned empty PEM data")
	}

	if !strings.Contains(string(pemData), "-----BEGIN PUBLIC KEY-----") {
		t.Error("EncodePublicKeyToPEM() PEM data doesn't contain expected header")
	}
}

func TestEncryptDecrypt(t *testing.T) {
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
			data:    bytes.Repeat([]byte("a"), 1024), // ECDH+AES can handle much larger data
			wantErr: false,
		},
	}

	aliceKeyPair, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate Alice's key pair: %v", err)
	}

	bobKeyPair, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate Bob's key pair: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Alice encrypts message for Bob
			encrypted, err := Encrypt(bobKeyPair.PublicKey, aliceKeyPair.PrivateKey, tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("Encrypt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			// Bob decrypts message from Alice
			decrypted, err := Decrypt(aliceKeyPair.PublicKey, bobKeyPair.PrivateKey, encrypted)
			if err != nil {
				t.Errorf("Decrypt() error = %v", err)
				return
			}

			// Compare original and decrypted data
			if !bytes.Equal(tt.data, decrypted) {
				t.Errorf("Decrypt() = %v, want %v", decrypted, tt.data)
			}
		})
	}
}

func TestDecrypt_InvalidInput(t *testing.T) {
	aliceKeyPair, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate Alice's key pair: %v", err)
	}

	bobKeyPair, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate Bob's key pair: %v", err)
	}

	_, err = Decrypt(aliceKeyPair.PublicKey, bobKeyPair.PrivateKey, []byte{})
	if err == nil {
		t.Error("Decrypt() should return error for empty ciphertext")
	}

	_, err = Decrypt(aliceKeyPair.PublicKey, bobKeyPair.PrivateKey, []byte("invalid ciphertext"))
	if err == nil {
		t.Error("Decrypt() should return error for invalid ciphertext")
	}
}

func TestKeyPairFileIO(t *testing.T) {
	// Create temporary directory for test files
	tempDir := t.TempDir()
	privateKeyPath := filepath.Join(tempDir, "private.pem")
	publicKeyPath := filepath.Join(tempDir, "public.pem")

	// Test cases
	tests := []struct {
		name string
	}{
		{"ECDH Key Pair"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Generate a key pair
			keyPair, err := GenerateKeyPair()
			if err != nil {
				t.Fatalf("Failed to generate key pair: %v", err)
			}

			// Write keys to files
			err = WriteKeyPairToFiles(keyPair, privateKeyPath, publicKeyPath)
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

			// Alice (original keypair) encrypts for Bob (read keys)
			encrypted, err := Encrypt(readPublicKey, keyPair.PrivateKey, testMessage)
			if err != nil {
				t.Fatalf("Failed to encrypt test message: %v", err)
			}

			// Bob (read keys) decrypts message from Alice
			decrypted, err := Decrypt(keyPair.PublicKey, readPrivateKey, encrypted)
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
	aliceKeyPair, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate Alice's key pair: %v", err)
	}

	bobKeyPair, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate Bob's key pair: %v", err)
	}

	// Test PEM encoding/decoding
	pemData, err := EncodePrivateKeyToPEM(aliceKeyPair.PrivateKey)
	if err != nil {
		t.Fatalf("Failed to encode private key: %v", err)
	}

	// Write PEM data to a temporary file
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "temp.pem")
	if err := os.WriteFile(tempFile, pemData, 0600); err != nil {
		t.Fatalf("Failed to write PEM data to temp file: %v", err)
	}

	// Read and test the key
	readPrivateKey, err := ReadPrivateKeyFromFile(tempFile)
	if err != nil {
		t.Fatalf("Failed to decode private key: %v", err)
	}

	// Test encryption/decryption with the keys
	testMessage := []byte("test message for encryption")
	encrypted, err := Encrypt(bobKeyPair.PublicKey, readPrivateKey, testMessage)
	if err != nil {
		t.Fatalf("Failed to encrypt: %v", err)
	}

	decrypted, err := Decrypt(aliceKeyPair.PublicKey, bobKeyPair.PrivateKey, encrypted)
	if err != nil {
		t.Fatalf("Failed to decrypt: %v", err)
	}

	if !bytes.Equal(testMessage, decrypted) {
		t.Error("Decrypted message doesn't match original")
	}
}

func TestEncryptWithInvalidKeys(t *testing.T) {
	keyPair1, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate key pair 1: %v", err)
	}
	keyPair2, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate key pair 2: %v", err)
	}

	// Test with nil keys
	testData := []byte("test data")
	_, err = Encrypt(nil, keyPair1.PrivateKey, testData)
	if err == nil {
		t.Error("Encrypt() should fail with nil public key")
	}

	_, err = Encrypt(keyPair2.PublicKey, nil, testData)
	if err == nil {
		t.Error("Encrypt() should fail with nil private key")
	}

	// Test with mismatched key pairs
	keyPair3, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate key pair 3: %v", err)
	}

	msg := []byte("test message")
	encrypted, err := Encrypt(keyPair2.PublicKey, keyPair1.PrivateKey, msg)
	if err != nil {
		t.Fatalf("Failed to encrypt with valid keys: %v", err)
	}

	// Try to decrypt with wrong key pair (keyPair3)
	_, err = Decrypt(keyPair1.PublicKey, keyPair3.PrivateKey, encrypted)
	if err == nil {
		t.Error("Decrypt() should fail with mismatched key pairs")
	}
}

func TestEncryptLargeData(t *testing.T) {
	keyPair1, _ := GenerateKeyPair()
	keyPair2, _ := GenerateKeyPair()

	// Test with 1MB of data
	largeData := bytes.Repeat([]byte("A"), 1024*1024)
	encrypted, err := Encrypt(keyPair2.PublicKey, keyPair1.PrivateKey, largeData)
	if err != nil {
		t.Errorf("Encrypt() failed with large data: %v", err)
	}

	decrypted, err := Decrypt(keyPair1.PublicKey, keyPair2.PrivateKey, encrypted)
	if err != nil {
		t.Errorf("Decrypt() failed with large data: %v", err)
	}

	if !bytes.Equal(largeData, decrypted) {
		t.Error("Decrypted large data doesn't match original")
	}
}

func TestDecryptWithTamperedData(t *testing.T) {
	keyPair1, _ := GenerateKeyPair()
	keyPair2, _ := GenerateKeyPair()

	original := []byte("test message")
	encrypted, _ := Encrypt(keyPair2.PublicKey, keyPair1.PrivateKey, original)

	// Test with tampered ciphertext
	tampered := make([]byte, len(encrypted))
	copy(tampered, encrypted)
	tampered[len(tampered)-1] ^= 0x01 // Flip one bit

	_, err := Decrypt(keyPair1.PublicKey, keyPair2.PrivateKey, tampered)
	if err == nil {
		t.Error("Decrypt() should fail with tampered ciphertext")
	}

	// Test with truncated ciphertext
	truncated := encrypted[:len(encrypted)-1]
	_, err = Decrypt(keyPair1.PublicKey, keyPair2.PrivateKey, truncated)
	if err == nil {
		t.Error("Decrypt() should fail with truncated ciphertext")
	}
}

func TestKeyPairFileIOWithInvalidData(t *testing.T) {
	tempDir := t.TempDir()
	privateKeyPath := filepath.Join(tempDir, "private.pem")

	// Test with invalid PEM data
	invalidPEM := []byte("-----BEGIN PRIVATE KEY-----\ninvalid data\n-----END PRIVATE KEY-----")
	err := os.WriteFile(privateKeyPath, invalidPEM, 0600)
	if err != nil {
		t.Fatalf("Failed to write invalid private key file: %v", err)
	}

	_, err = ReadPrivateKeyFromFile(privateKeyPath)
	if err == nil {
		t.Error("ReadPrivateKeyFromFile() should fail with invalid PEM data")
	}

	// Test with wrong key type
	wrongTypePEM := []byte("-----BEGIN RSA PRIVATE KEY-----\nMIICXQIBAAKBgQC8AEqKyFXCzHVx1H+ywrKVhYhkVBwsJ5g5fdEtxhHkl7dPCqQq6RQRKGa1qGm6jeBFxZhutMwBUHPTdz9SMUGZpGbpbcQ95/73KZcvBNhHFQVNKp1zibNVq0gnV0RYE5T6MR8fnXBPRYhUDZMcXVfwpSptsTmBHUf3tX+GhXLtlQIDAQABAoGANhFG7YGZuNJ2TubQvhHJQvgpqmJzO+E5ECZlHJWHFS0pQILhGh6FGh4GUZGHtYGVVWLRgGHDVQPMwkwYRXZKfvVpHd7LQlHhDBd5Qw7zJZ1J7qAm0ZHaU1XPz3GSoGYYwUXuF4+Nl0UNlRFJIZCVXlL5IRnAF6TXXhGzLYUFoQECQQDgZd390f5PQXJ0cF8VyQGvVQNYC3AFcO9KXZ5kmZUxQDiCRTiVqZ/QpJrRbKaGXgmJKpyrgZBGypQjXJKXGqtRAkEA1uVE/Y8gf7C/J9/Oa4O4P0C4qsYn9c2HyQWXgKlb5QgBBUZsXS/RQqVhpERQpRGJfKLFb6JO+HjvFNrLMBtVZQJBAJXcHvXwTcBYZF5D0h3lW2Qc9Q0EZhDvPZo7qwF5+OZhm2fM1J3WFQZuHLqFZwBXJC9RbgZFPvNjuEDKBVDVqmECQQCz39tZGS6Uzmk7QWqvdVwZ7DtHSNRE4VFwz7qqc+3ZnO9cENbfkFLXdxEWnYFLsS2DPu26QUi+yWBz/8ib6tblAkAEjSlQYGZg/BGkJ+NcK5HQKmlHAh0ZEgzM1dG6cFbFrVWj7bZ7F4XxVVjkNQKZmVm3o+l6UQhJtGdNXD2HcvmF\n-----END RSA PRIVATE KEY-----")
	err = os.WriteFile(privateKeyPath, wrongTypePEM, 0600)
	if err != nil {
		t.Fatalf("Failed to write wrong type private key file: %v", err)
	}

	_, err = ReadPrivateKeyFromFile(privateKeyPath)
	if err == nil {
		t.Error("ReadPrivateKeyFromFile() should fail with wrong key type")
	}

	// Test with empty file
	err = os.WriteFile(privateKeyPath, []byte{}, 0600)
	if err != nil {
		t.Fatalf("Failed to write empty file: %v", err)
	}

	_, err = ReadPrivateKeyFromFile(privateKeyPath)
	if err == nil {
		t.Error("ReadPrivateKeyFromFile() should fail with empty file")
	}
}

func TestSharedSecretConsistency(t *testing.T) {
	// Test that the same shared secret is generated in both directions
	aliceKeyPair, _ := GenerateKeyPair()
	bobKeyPair, _ := GenerateKeyPair()

	// Test multiple messages to ensure consistent encryption/decryption
	messages := []string{
		"short message",
		"longer message with some numbers 12345",
		strings.Repeat("very long message ", 100),
	}

	for _, msg := range messages {
		// Alice encrypts for Bob
		encrypted1, err := Encrypt(bobKeyPair.PublicKey, aliceKeyPair.PrivateKey, []byte(msg))
		if err != nil {
			t.Errorf("Failed to encrypt first message: %v", err)
		}

		// Bob encrypts for Alice
		encrypted2, err := Encrypt(aliceKeyPair.PublicKey, bobKeyPair.PrivateKey, []byte(msg))
		if err != nil {
			t.Errorf("Failed to encrypt second message: %v", err)
		}

		// Decrypt both messages
		decrypted1, err := Decrypt(aliceKeyPair.PublicKey, bobKeyPair.PrivateKey, encrypted1)
		if err != nil {
			t.Errorf("Failed to decrypt first message: %v", err)
		}

		decrypted2, err := Decrypt(bobKeyPair.PublicKey, aliceKeyPair.PrivateKey, encrypted2)
		if err != nil {
			t.Errorf("Failed to decrypt second message: %v", err)
		}

		// Verify both decrypted messages match the original
		if !bytes.Equal([]byte(msg), decrypted1) || !bytes.Equal([]byte(msg), decrypted2) {
			t.Error("Decrypted messages don't match original")
		}
	}
}

func FuzzEncryptDecrypt(f *testing.F) {
	// Add seed corpus
	f.Add([]byte("hello world"), uint8(0))
	f.Add([]byte{}, uint8(1))
	f.Add(bytes.Repeat([]byte("A"), 1024), uint8(2))

	// Generate a valid key pair for testing
	aliceKeyPair, err := GenerateKeyPair()
	if err != nil {
		f.Fatal(err)
	}
	bobKeyPair, err := GenerateKeyPair()
	if err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, data []byte, mode uint8) {
		// Test different scenarios based on mode
		switch mode % 4 {
		case 0:
			// Normal encryption/decryption
			encrypted, err := Encrypt(bobKeyPair.PublicKey, aliceKeyPair.PrivateKey, data)
			if err != nil && len(data) > 0 {
				t.Logf("Valid encryption failed: %v", err)
				return
			}
			if len(data) == 0 && err == nil {
				t.Error("Expected error for empty data")
				return
			}
			if len(data) > 0 {
				decrypted, err := Decrypt(aliceKeyPair.PublicKey, bobKeyPair.PrivateKey, encrypted)
				if err != nil {
					t.Logf("Valid decryption failed: %v", err)
					return
				}
				if !bytes.Equal(data, decrypted) {
					t.Error("Decrypted data doesn't match original")
				}
			}

		case 1:
			// Try to decrypt random data
			_, err := Decrypt(aliceKeyPair.PublicKey, bobKeyPair.PrivateKey, data)
			if err == nil && len(data) > 0 {
				t.Error("Expected error when decrypting random data")
			}

		case 2:
			// Test with modified ciphertext
			if len(data) > 0 {
				encrypted, err := Encrypt(bobKeyPair.PublicKey, aliceKeyPair.PrivateKey, data)
				if err != nil {
					return
				}
				// Modify random bytes in the ciphertext
				modified := make([]byte, len(encrypted))
				copy(modified, encrypted)
				for i := 0; i < len(modified); i++ {
					modified[i] ^= byte(i)
				}
				_, err = Decrypt(aliceKeyPair.PublicKey, bobKeyPair.PrivateKey, modified)
				if err == nil {
					t.Error("Expected error when decrypting modified ciphertext")
				}
			}

		case 3:
			// Test with truncated ciphertext
			if len(data) > 0 {
				encrypted, err := Encrypt(bobKeyPair.PublicKey, aliceKeyPair.PrivateKey, data)
				if err != nil {
					return
				}
				if len(encrypted) > 0 {
					truncated := encrypted[:len(encrypted)-1]
					_, err = Decrypt(aliceKeyPair.PublicKey, bobKeyPair.PrivateKey, truncated)
					if err == nil {
						t.Error("Expected error when decrypting truncated ciphertext")
					}
				}
			}
		}
	})
}

func FuzzPEMEncoding(f *testing.F) {
	// Add seed corpus
	keyPair, err := GenerateKeyPair()
	if err != nil {
		f.Fatal(err)
	}
	validPrivPEM, err := EncodePrivateKeyToPEM(keyPair.PrivateKey)
	if err != nil {
		f.Fatal(err)
	}
	validPubPEM, err := EncodePublicKeyToPEM(keyPair.PublicKey)
	if err != nil {
		f.Fatal(err)
	}

	f.Add(validPrivPEM, uint8(0))
	f.Add(validPubPEM, uint8(1))
	f.Add([]byte("invalid PEM"), uint8(2))

	f.Fuzz(func(t *testing.T, data []byte, mode uint8) {
		tempDir := t.TempDir()
		keyPath := filepath.Join(tempDir, "key.pem")

		err := os.WriteFile(keyPath, data, 0600)
		if err != nil {
			return
		}

		switch mode % 2 {
		case 0:
			// Test private key reading
			_, err := ReadPrivateKeyFromFile(keyPath)
			if err == nil {
				// If no error, verify it was valid PEM data
				if !bytes.Contains(data, []byte("-----BEGIN PRIVATE KEY-----")) {
					t.Error("ReadPrivateKeyFromFile() succeeded with invalid PEM data")
				}
			}

		case 1:
			// Test public key reading
			_, err := ReadPublicKeyFromFile(keyPath)
			if err == nil {
				// If no error, verify it was valid PEM data
				if !bytes.Contains(data, []byte("-----BEGIN PUBLIC KEY-----")) {
					t.Error("ReadPublicKeyFromFile() succeeded with invalid PEM data")
				}
			}
		}
	})
}

func TestConcurrentEncryptionDecryption(t *testing.T) {
	aliceKeyPair, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate Alice's key pair: %v", err)
	}
	bobKeyPair, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate Bob's key pair: %v", err)
	}

	testMessage := []byte("concurrent test message")
	numGoroutines := 100
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			encrypted, err := Encrypt(bobKeyPair.PublicKey, aliceKeyPair.PrivateKey, testMessage)
			if err != nil {
				errors <- fmt.Errorf("encryption failed: %v", err)
				return
			}

			decrypted, err := Decrypt(aliceKeyPair.PublicKey, bobKeyPair.PrivateKey, encrypted)
			if err != nil {
				errors <- fmt.Errorf("decryption failed: %v", err)
				return
			}

			if !bytes.Equal(testMessage, decrypted) {
				errors <- fmt.Errorf("decrypted message doesn't match original")
				return
			}

			errors <- nil
		}()
	}

	for i := 0; i < numGoroutines; i++ {
		if err := <-errors; err != nil {
			t.Error(err)
		}
	}
}

func TestNonDeterministicEncryption(t *testing.T) {
	aliceKeyPair, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate Alice's key pair: %v", err)
	}
	bobKeyPair, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate Bob's key pair: %v", err)
	}

	testMessage := []byte("test message for non-deterministic encryption")

	encrypted1, err := Encrypt(bobKeyPair.PublicKey, aliceKeyPair.PrivateKey, testMessage)
	if err != nil {
		t.Fatalf("Failed to encrypt first message: %v", err)
	}

	encrypted2, err := Encrypt(bobKeyPair.PublicKey, aliceKeyPair.PrivateKey, testMessage)
	if err != nil {
		t.Fatalf("Failed to encrypt second message: %v", err)
	}

	if bytes.Equal(encrypted1, encrypted2) {
		t.Error("Encryption should be non-deterministic, but produced identical ciphertexts")
	}
}

func TestKeySerialization(t *testing.T) {
	keyPair, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}

	// Serialize keys to JSON
	privKeyJSON, err := json.Marshal(keyPair.PrivateKey.Bytes())
	if err != nil {
		t.Fatalf("Failed to serialize private key: %v", err)
	}

	pubKeyJSON, err := json.Marshal(keyPair.PublicKey.Bytes())
	if err != nil {
		t.Fatalf("Failed to serialize public key: %v", err)
	}

	// Deserialize keys from JSON
	var privKeyBytes, pubKeyBytes []byte
	if err := json.Unmarshal(privKeyJSON, &privKeyBytes); err != nil {
		t.Fatalf("Failed to deserialize private key: %v", err)
	}

	if err := json.Unmarshal(pubKeyJSON, &pubKeyBytes); err != nil {
		t.Fatalf("Failed to deserialize public key: %v", err)
	}

	curve := ecdh.P256()
	deserializedPrivateKey, err := curve.NewPrivateKey(privKeyBytes)
	if err != nil {
		t.Fatalf("Failed to create private key from bytes: %v", err)
	}

	deserializedPublicKey, err := curve.NewPublicKey(pubKeyBytes)
	if err != nil {
		t.Fatalf("Failed to create public key from bytes: %v", err)
	}

	// Verify that the deserialized keys match the original keys
	if !bytes.Equal(deserializedPrivateKey.Bytes(), keyPair.PrivateKey.Bytes()) {
		t.Error("Deserialized private key does not match original")
	}

	if !bytes.Equal(deserializedPublicKey.Bytes(), keyPair.PublicKey.Bytes()) {
		t.Error("Deserialized public key does not match original")
	}
}

func BenchmarkEncrypt(b *testing.B) {
	aliceKeyPair, err := GenerateKeyPair()
	if err != nil {
		b.Fatalf("Failed to generate Alice's key pair: %v", err)
	}
	bobKeyPair, err := GenerateKeyPair()
	if err != nil {
		b.Fatalf("Failed to generate Bob's key pair: %v", err)
	}

	testMessage := []byte("benchmark test message")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Encrypt(bobKeyPair.PublicKey, aliceKeyPair.PrivateKey, testMessage)
		if err != nil {
			b.Fatalf("Encryption failed: %v", err)
		}
	}
}

func BenchmarkDecrypt(b *testing.B) {
	aliceKeyPair, err := GenerateKeyPair()
	if err != nil {
		b.Fatalf("Failed to generate Alice's key pair: %v", err)
	}
	bobKeyPair, err := GenerateKeyPair()
	if err != nil {
		b.Fatalf("Failed to generate Bob's key pair: %v", err)
	}

	testMessage := []byte("benchmark test message")
	encrypted, err := Encrypt(bobKeyPair.PublicKey, aliceKeyPair.PrivateKey, testMessage)
	if err != nil {
		b.Fatalf("Failed to encrypt message: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Decrypt(aliceKeyPair.PublicKey, bobKeyPair.PrivateKey, encrypted)
		if err != nil {
			b.Fatalf("Decryption failed: %v", err)
		}
	}
}

type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error { return nil }

func TestStreamEncryptionDecryption(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		key     string
		wantErr bool
	}{
		{
			name:    "valid data",
			data:    []byte("hello world"),
			key:     "test-key-1",
			wantErr: false,
		},
		{
			name:    "empty data",
			data:    []byte{},
			key:     "test-key-2",
			wantErr: false,
		},
		{
			name:    "long data",
			data:    bytes.Repeat([]byte("a"), 1024*1024), // 1MB
			key:     "test-key-3",
			wantErr: false,
		},
		{
			name:    "empty key",
			data:    []byte("test data"),
			key:     "",
			wantErr: false, // Empty key is valid, though not recommended
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create input and output buffers
			input := bytes.NewReader(tt.data)
			encryptedBuf := &bytes.Buffer{}

			// Encrypt
			err := EncryptStream(input, nopCloser{encryptedBuf}, tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("EncryptStream() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			// Create decryption buffers
			encryptedReader := bytes.NewReader(encryptedBuf.Bytes())
			decryptedBuf := &bytes.Buffer{}

			// Decrypt
			err = DecryptStream(encryptedReader, nopCloser{decryptedBuf}, tt.key)
			if err != nil {
				t.Errorf("DecryptStream() error = %v", err)
				return
			}

			// Compare original and decrypted data
			if !bytes.Equal(tt.data, decryptedBuf.Bytes()) {
				t.Errorf("DecryptStream() = %v, want %v", decryptedBuf.Bytes(), tt.data)
			}
		})
	}
}

func TestStreamDecryptionWithWrongKey(t *testing.T) {
	originalData := []byte("sensitive data")
	input := bytes.NewReader(originalData)
	encryptedBuf := &bytes.Buffer{}

	// Encrypt with one key
	err := EncryptStream(input, nopCloser{encryptedBuf}, "correct-key")
	if err != nil {
		t.Fatalf("EncryptStream() error = %v", err)
	}

	// Try to decrypt with wrong key
	encryptedReader := bytes.NewReader(encryptedBuf.Bytes())
	decryptedBuf := &bytes.Buffer{}
	err = DecryptStream(encryptedReader, nopCloser{decryptedBuf}, "wrong-key")
	if err == nil {
		t.Error("DecryptStream() with wrong key should return error")
	}
}

func TestStreamDecryptionWithTamperedData(t *testing.T) {
	originalData := []byte("sensitive data")
	input := bytes.NewReader(originalData)
	encryptedBuf := &bytes.Buffer{}

	// Encrypt
	err := EncryptStream(input, nopCloser{encryptedBuf}, "test-key")
	if err != nil {
		t.Fatalf("EncryptStream() error = %v", err)
	}

	// Tamper with encrypted data
	encryptedData := encryptedBuf.Bytes()
	tamperedData := make([]byte, len(encryptedData))
	copy(tamperedData, encryptedData)
	tamperedData[len(tamperedData)-1] ^= 0x01 // Flip last bit

	// Try to decrypt tampered data
	tamperedReader := bytes.NewReader(tamperedData)
	decryptedBuf := &bytes.Buffer{}
	err = DecryptStream(tamperedReader, nopCloser{decryptedBuf}, "test-key")
	if err == nil {
		t.Error("DecryptStream() with tampered data should return error")
	}
}

func TestStreamEncryptionWithLargeData(t *testing.T) {
	// Test with 10MB of data
	originalData := bytes.Repeat([]byte("large data test "), 10*1024*1024)
	input := bytes.NewReader(originalData)
	encryptedBuf := &bytes.Buffer{}

	// Encrypt
	err := EncryptStream(input, nopCloser{encryptedBuf}, "test-key")
	if err != nil {
		t.Fatalf("EncryptStream() error = %v", err)
	}

	// Decrypt
	encryptedReader := bytes.NewReader(encryptedBuf.Bytes())
	decryptedBuf := &bytes.Buffer{}
	err = DecryptStream(encryptedReader, nopCloser{decryptedBuf}, "test-key")
	if err != nil {
		t.Fatalf("DecryptStream() error = %v", err)
	}

	// Verify
	if !bytes.Equal(originalData, decryptedBuf.Bytes()) {
		t.Error("Decrypted large data doesn't match original")
	}
}

func TestStreamEncryptionWithVariousSizeOfData(t *testing.T) {
	for i := 1; i < 1024; i++ {
		originalData := bytes.Repeat([]byte("large data test "), 2*1024*i)
		input := bytes.NewReader(originalData)
		encryptedBuf := &bytes.Buffer{}

		key := fmt.Sprintf("test-key-%d", i)

		// Encrypt
		err := EncryptStream(input, nopCloser{encryptedBuf}, key)
		if err != nil {
			t.Fatalf("EncryptStream() error = %v", err)
		}

		// Decrypt
		encryptedReader := bytes.NewReader(encryptedBuf.Bytes())
		decryptedBuf := &bytes.Buffer{}
		err = DecryptStream(encryptedReader, nopCloser{decryptedBuf}, key)
		if err != nil {
			t.Fatalf("DecryptStream() error = %v", err)
		}

		// Verify
		if !bytes.Equal(originalData, decryptedBuf.Bytes()) {
			t.Error("Decrypted large data doesn't match original")
		}
	}
}

func TestStreamEncryptionWithMultipleKeys(t *testing.T) {
	data := []byte("test data")
	keys := []string{"key1", "key2", "key3"}

	for _, key1 := range keys {
		for _, key2 := range keys {
			input := bytes.NewReader(data)
			encryptedBuf := &bytes.Buffer{}

			// Encrypt with key1
			err := EncryptStream(input, nopCloser{encryptedBuf}, key1)
			if err != nil {
				t.Fatalf("EncryptStream() error = %v", err)
			}

			// Try to decrypt with key2
			encryptedReader := bytes.NewReader(encryptedBuf.Bytes())
			decryptedBuf := &bytes.Buffer{}
			err = DecryptStream(encryptedReader, nopCloser{decryptedBuf}, key2)

			if key1 == key2 {
				// Should succeed with same key
				if err != nil {
					t.Errorf("DecryptStream() with matching key error = %v", err)
				}
				if !bytes.Equal(data, decryptedBuf.Bytes()) {
					t.Error("Decrypted data doesn't match original")
				}
			} else {
				// Should fail with different key
				if err == nil {
					t.Error("DecryptStream() with different key should return error")
				}
			}
		}
	}
}

func BenchmarkStreamEncryption(b *testing.B) {
	data := bytes.Repeat([]byte("benchmark data "), 1024) // ~13KB
	key := "benchmark-key"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		input := bytes.NewReader(data)
		output := &bytes.Buffer{}
		err := EncryptStream(input, nopCloser{output}, key)
		if err != nil {
			b.Fatalf("EncryptStream() error = %v", err)
		}
	}
}

func BenchmarkStreamDecryption(b *testing.B) {
	data := bytes.Repeat([]byte("benchmark data "), 1024) // ~13KB
	key := "benchmark-key"

	// Create encrypted data for benchmark
	input := bytes.NewReader(data)
	encryptedBuf := &bytes.Buffer{}
	err := EncryptStream(input, nopCloser{encryptedBuf}, key)
	if err != nil {
		b.Fatalf("Setup encryption failed: %v", err)
	}
	encryptedData := encryptedBuf.Bytes()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		input := bytes.NewReader(encryptedData)
		output := &bytes.Buffer{}
		err := DecryptStream(input, nopCloser{output}, key)
		if err != nil {
			b.Fatalf("DecryptStream() error = %v", err)
		}
	}
}

func TestStreamChunkSizeEncoding(t *testing.T) {
	// Test with various chunk sizes to validate encoding/decoding
	sizes := []int{
		64,               // Small chunk
		64 * 1024,        // 64KB (normal chunk)
		1024 * 1024,      // 1MB
		10*1024*1024 - 1, // Just under max
	}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("chunk_size_%d", size), func(t *testing.T) {
			// Create test data slightly larger than chunk size to force multiple chunks
			data := bytes.Repeat([]byte("test"), (size/4)+1)
			input := bytes.NewReader(data)
			encryptedBuf := &bytes.Buffer{}

			// Encrypt
			err := EncryptStream(input, nopCloser{encryptedBuf}, "test-key")
			if err != nil {
				t.Fatalf("EncryptStream() error = %v", err)
			}

			// Verify the first chunk size
			encryptedData := encryptedBuf.Bytes()
			if len(encryptedData) < 4 {
				t.Fatal("Encrypted data too short")
			}

			// Read the chunk size (first 4 bytes, little-endian)
			chunkSize := uint32(encryptedData[0]) |
				uint32(encryptedData[1])<<8 |
				uint32(encryptedData[2])<<16 |
				uint32(encryptedData[3])<<24

			// Verify chunk size is reasonable
			if chunkSize > 10*1024*1024 {
				t.Errorf("Chunk size too large: %d", chunkSize)
			}

			// Try to decrypt
			decryptedBuf := &bytes.Buffer{}
			err = DecryptStream(bytes.NewReader(encryptedData), nopCloser{decryptedBuf}, "test-key")
			if err != nil {
				t.Fatalf("DecryptStream() error = %v", err)
			}

			// Verify decrypted data matches original
			if !bytes.Equal(data, decryptedBuf.Bytes()) {
				t.Error("Decrypted data doesn't match original")
			}
		})
	}
}

// Also add a test for invalid chunk sizes
func TestStreamInvalidChunkSize(t *testing.T) {
	// Create a malicious encrypted stream with an invalid chunk size
	var invalidData []byte

	// Add an invalid chunk size (larger than 10MB)
	invalidSize := []byte{0xFF, 0xFF, 0xFF, 0xFF} // Maximum uint32
	invalidData = append(invalidData, invalidSize...)

	// Add some dummy data
	invalidData = append(invalidData, bytes.Repeat([]byte{0}, 100)...)

	// Try to decrypt
	decryptedBuf := &bytes.Buffer{}
	err := DecryptStream(bytes.NewReader(invalidData), nopCloser{decryptedBuf}, "test-key")

	// Should fail with "chunk size too large" error
	if err == nil {
		t.Error("DecryptStream() should fail with invalid chunk size")
	}
	if !strings.Contains(err.Error(), "chunk size too large") {
		t.Errorf("Expected 'chunk size too large' error, got: %v", err)
	}
}

// errorReader simulates network issues by returning errors after N bytes
type errorReader struct {
	r        io.Reader
	bytesMax int
	count    int
}

func (er *errorReader) Read(p []byte) (n int, err error) {
	if er.count >= er.bytesMax {
		return 0, fmt.Errorf("simulated network error after %d bytes", er.count)
	}

	// Calculate how many bytes we can read
	remaining := er.bytesMax - er.count
	if len(p) > remaining {
		p = p[:remaining]
	}

	n, err = er.r.Read(p)
	er.count += n

	// If this read would put us over the limit, return an error
	if er.count >= er.bytesMax {
		err = fmt.Errorf("simulated network error at exactly %d bytes", er.bytesMax)
	}
	return n, err
}

func TestStreamInterruption(t *testing.T) {
	data := bytes.Repeat([]byte("test data for interruption "), 1024) // ~16KB
	key := "test-key"

	// Calculate sizes for a typical chunk
	chunkSize := 64 * 1024 // 64KB chunks
	headerSize := 4        // 4 bytes for size
	nonceSize := 12        // 12 bytes for nonce
	tagSize := 16          // 16 bytes for GCM tag

	testCases := []struct {
		name           string
		encInterruptAt int
		decInterruptAt int
		expectEncErr   bool
		expectDecErr   bool
		expectPartial  bool // Whether we expect partial successful decryption
	}{
		{
			name:           "interrupt_at_start",
			encInterruptAt: 2, // Interrupt during size header
			decInterruptAt: 2,
			expectEncErr:   true,
			expectDecErr:   true,
			expectPartial:  false,
		},
		{
			name:           "interrupt_at_nonce",
			encInterruptAt: headerSize + 6, // Interrupt during nonce
			decInterruptAt: headerSize + 6,
			expectEncErr:   true,
			expectDecErr:   true,
			expectPartial:  false,
		},
		{
			name:           "interrupt_mid_chunk",
			encInterruptAt: headerSize + nonceSize + 1000, // Interrupt during chunk data
			decInterruptAt: headerSize + nonceSize + 1000,
			expectEncErr:   true,
			expectDecErr:   true,
			expectPartial:  false,
		},
		{
			name:           "interrupt_between_chunks",
			encInterruptAt: headerSize + nonceSize + chunkSize + tagSize + 2, // During header of second chunk
			decInterruptAt: headerSize + nonceSize + chunkSize + tagSize + 2,
			expectEncErr:   false, // The first chunk was successfully written
			expectDecErr:   false, // The first chunk should be successfully decrypted
			expectPartial:  true,  // We expect partial data to be decrypted
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test encryption interruption
			input := &errorReader{bytes.NewReader(data), tc.encInterruptAt, 0}
			encryptedBuf := &bytes.Buffer{}
			encErr := EncryptStream(input, nopCloser{encryptedBuf}, key)

			if (encErr != nil) != tc.expectEncErr {
				t.Errorf("%s: EncryptStream() error = %v, want error? %v", tc.name, encErr, tc.expectEncErr)
			}

			// If we have any encrypted data, test decryption interruption
			if encryptedBuf.Len() > 0 {
				t.Logf("%s: Successfully encrypted %d bytes before interruption", tc.name, encryptedBuf.Len())

				// Create a new error reader with the decryption interrupt point
				decInput := &errorReader{bytes.NewReader(encryptedBuf.Bytes()), tc.decInterruptAt, 0}
				decryptedBuf := &bytes.Buffer{}
				decErr := DecryptStream(decInput, nopCloser{decryptedBuf}, key)

				if (decErr != nil) != tc.expectDecErr {
					t.Errorf("%s: DecryptStream() error = %v, want error? %v", tc.name, decErr, tc.expectDecErr)
				}

				// If we expect partial success or full success
				if tc.expectPartial || decErr == nil {
					if decryptedBuf.Len() == 0 {
						t.Errorf("%s: Expected partial decryption but got no data", tc.name)
					}
					// Verify the decrypted data matches the corresponding part of the original
					if !bytes.Equal(data[:decryptedBuf.Len()], decryptedBuf.Bytes()) {
						t.Errorf("%s: Decrypted data doesn't match original (got %d bytes, want %d bytes)",
							tc.name, decryptedBuf.Len(), len(data[:decryptedBuf.Len()]))
					} else {
						t.Logf("%s: Successfully decrypted %d bytes", tc.name, decryptedBuf.Len())
					}
				} else if decryptedBuf.Len() > 0 {
					t.Errorf("%s: Got unexpected decrypted data (%d bytes)", tc.name, decryptedBuf.Len())
				}
			} else {
				t.Logf("%s: No data was encrypted before interruption", tc.name)
			}
		})
	}
}

// memoryTrackingWriter tracks the maximum amount of memory used
type memoryTrackingWriter struct {
	buf       *bytes.Buffer
	maxMemory int
}

func (w *memoryTrackingWriter) Write(p []byte) (n int, err error) {
	w.maxMemory = max(w.maxMemory, w.buf.Len()+len(p))
	return w.buf.Write(p)
}

func (w *memoryTrackingWriter) Close() error {
	return nil
}

func TestStreamMemoryUsage(t *testing.T) {
	// Test with progressively larger data sizes
	sizes := []int{
		64 * 1024,       // 64KB
		256 * 1024,      // 256KB
		1024 * 1024,     // 1MB
		4 * 1024 * 1024, // 4MB
	}

	key := "test-key"

	// Calculate overhead components:
	headerSize := 4        // 4 bytes for size
	nonceSize := 12        // 12 bytes for nonce
	tagSize := 16          // 16 bytes for GCM tag
	chunkSize := 64 * 1024 // 64KB chunks

	// Base overhead includes:
	// - Initial buffer allocation (64KB)
	// - GCM context (~4KB)
	// - Go's internal buffer management (~32KB)
	// - Additional safety margin (32KB)
	baseOverhead := 256 * 1024 // 256KB base overhead

	for _, size := range sizes {
		t.Run(fmt.Sprintf("size_%d", size), func(t *testing.T) {
			data := bytes.Repeat([]byte("memory test "), size/11)

			// Calculate number of chunks (round up)
			numChunks := (len(data) + chunkSize - 1) / chunkSize

			// Calculate per-chunk overhead
			chunkOverhead := headerSize + nonceSize + tagSize // overhead per chunk
			totalChunkOverhead := chunkOverhead * numChunks

			// Calculate total expected memory:
			// - Base overhead
			// - Original data size
			// - Total chunk overhead
			// - Add 50% buffer for Go's memory management and temporary buffers
			expectedMaxMemory := int(float64(baseOverhead+size+totalChunkOverhead) * 1.5)

			// Track memory during encryption
			encryptedWriter := &memoryTrackingWriter{buf: &bytes.Buffer{}}
			err := EncryptStream(bytes.NewReader(data), encryptedWriter, key)
			if err != nil {
				t.Fatalf("EncryptStream() error = %v", err)
			}

			if encryptedWriter.maxMemory > expectedMaxMemory {
				t.Errorf("Encryption used too much memory: got %d bytes, want <= %d bytes\n"+
					"Details:\n"+
					"  Data size: %d\n"+
					"  Chunks: %d\n"+
					"  Per-chunk overhead: %d\n"+
					"  Total chunk overhead: %d\n"+
					"  Base overhead: %d\n"+
					"  Expected max (with 50%% buffer): %d",
					encryptedWriter.maxMemory, expectedMaxMemory,
					size, numChunks, chunkOverhead, totalChunkOverhead,
					baseOverhead, expectedMaxMemory)
			}

			// Track memory during decryption
			decryptedWriter := &memoryTrackingWriter{buf: &bytes.Buffer{}}
			err = DecryptStream(bytes.NewReader(encryptedWriter.buf.Bytes()), decryptedWriter, key)
			if err != nil {
				t.Fatalf("DecryptStream() error = %v", err)
			}

			if decryptedWriter.maxMemory > expectedMaxMemory {
				t.Errorf("Decryption used too much memory: got %d bytes, want <= %d bytes\n"+
					"Details:\n"+
					"  Data size: %d\n"+
					"  Chunks: %d\n"+
					"  Per-chunk overhead: %d\n"+
					"  Total chunk overhead: %d\n"+
					"  Base overhead: %d\n"+
					"  Expected max (with 50%% buffer): %d",
					decryptedWriter.maxMemory, expectedMaxMemory,
					size, numChunks, chunkOverhead, totalChunkOverhead,
					baseOverhead, expectedMaxMemory)
			}

			// Verify data integrity
			if !bytes.Equal(data, decryptedWriter.buf.Bytes()) {
				t.Error("Decrypted data doesn't match original")
			}
		})
	}
}

func TestStreamBoundaries(t *testing.T) {
	key := "test-key"
	chunkSize := 64 * 1024 // 64KB chunks

	testCases := []struct {
		name string
		size int
	}{
		{"at_chunk_boundary", chunkSize},
		{"one_byte_before_boundary", chunkSize - 1},
		{"one_byte_after_boundary", chunkSize + 1},
		{"half_chunk", chunkSize / 2},
		{"double_chunk", chunkSize * 2},
		{"just_under_two_chunks", chunkSize*2 - 1},
		{"just_over_two_chunks", chunkSize*2 + 1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := bytes.Repeat([]byte{0x55}, tc.size) // Use pattern that's easy to verify

			// Encrypt
			encryptedBuf := &bytes.Buffer{}
			err := EncryptStream(bytes.NewReader(data), nopCloser{encryptedBuf}, key)
			if err != nil {
				t.Fatalf("EncryptStream() error = %v", err)
			}

			// Decrypt
			decryptedBuf := &bytes.Buffer{}
			err = DecryptStream(bytes.NewReader(encryptedBuf.Bytes()), nopCloser{decryptedBuf}, key)
			if err != nil {
				t.Fatalf("DecryptStream() error = %v", err)
			}

			// Verify
			if !bytes.Equal(data, decryptedBuf.Bytes()) {
				t.Errorf("Data mismatch at size %d", tc.size)
			}
		})
	}
}

func TestStreamKeyReuse(t *testing.T) {
	key := "test-key"
	data1 := []byte("first message")
	data2 := []byte("second message")

	// Encrypt both messages with the same key
	encryptedBuf1 := &bytes.Buffer{}
	encryptedBuf2 := &bytes.Buffer{}

	err := EncryptStream(bytes.NewReader(data1), nopCloser{encryptedBuf1}, key)
	if err != nil {
		t.Fatalf("First EncryptStream() error = %v", err)
	}

	err = EncryptStream(bytes.NewReader(data2), nopCloser{encryptedBuf2}, key)
	if err != nil {
		t.Fatalf("Second EncryptStream() error = %v", err)
	}

	// Verify ciphertexts are different even with same key
	if bytes.Equal(encryptedBuf1.Bytes(), encryptedBuf2.Bytes()) {
		t.Error("Ciphertexts should be different even with same key")
	}

	// Verify both decrypt correctly
	decryptedBuf1 := &bytes.Buffer{}
	decryptedBuf2 := &bytes.Buffer{}

	err = DecryptStream(bytes.NewReader(encryptedBuf1.Bytes()), nopCloser{decryptedBuf1}, key)
	if err != nil {
		t.Fatalf("First DecryptStream() error = %v", err)
	}

	err = DecryptStream(bytes.NewReader(encryptedBuf2.Bytes()), nopCloser{decryptedBuf2}, key)
	if err != nil {
		t.Fatalf("Second DecryptStream() error = %v", err)
	}

	// Verify decrypted data matches original
	if !bytes.Equal(data1, decryptedBuf1.Bytes()) {
		t.Error("First decrypted message doesn't match original")
	}
	if !bytes.Equal(data2, decryptedBuf2.Bytes()) {
		t.Error("Second decrypted message doesn't match original")
	}

	// Try to decrypt first message with modified key
	modifiedKey := key + "modified"
	decryptedBuf3 := &bytes.Buffer{}
	err = DecryptStream(bytes.NewReader(encryptedBuf1.Bytes()), nopCloser{decryptedBuf3}, modifiedKey)
	if err == nil {
		t.Error("DecryptStream() should fail with wrong key")
	}
}
