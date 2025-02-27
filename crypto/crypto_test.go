package crypto

import (
	"bytes"
	"crypto/ecdh"
	"encoding/json"
	"fmt"
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
