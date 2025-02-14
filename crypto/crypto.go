package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
)

// KeyPair represents an ECDH key pair
type KeyPair struct {
	PrivateKey *ecdh.PrivateKey
	PublicKey  *ecdh.PublicKey
}

// GenerateKeyPair generates a new ECDH key pair using P-256
func GenerateKeyPair() (*KeyPair, error) {
	curve := ecdh.P256()
	privateKey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ECDH key pair: %w", err)
	}

	return &KeyPair{
		PrivateKey: privateKey,
		PublicKey:  privateKey.PublicKey(),
	}, nil
}

// EncodePrivateKeyToPEM converts an ECDH private key to PEM format using PKCS#8
func EncodePrivateKeyToPEM(privateKey *ecdh.PrivateKey) []byte {
	// Get raw private key bytes
	privBytes := privateKey.Bytes()

	privPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: privBytes,
		},
	)
	return privPEM
}

// EncodePublicKeyToPEM converts an ECDH public key to PEM format using PKIX
func EncodePublicKeyToPEM(publicKey *ecdh.PublicKey) []byte {
	// Get raw public key bytes
	pubBytes := publicKey.Bytes()

	pubPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "PUBLIC KEY",
			Bytes: pubBytes,
		},
	)
	return pubPEM
}

// Encrypt encrypts data using AES-GCM with a shared secret derived from ECDH
func Encrypt(publicKey *ecdh.PublicKey, privateKey *ecdh.PrivateKey, plaintext []byte) ([]byte, error) {
	if publicKey == nil || privateKey == nil {
		return nil, fmt.Errorf("public key and private key cannot be nil")
	}
	if len(plaintext) == 0 {
		return nil, fmt.Errorf("data to encrypt cannot be empty")
	}

	// Generate shared secret
	secret, err := privateKey.ECDH(publicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to generate shared secret: %w", err)
	}

	// Create AES-GCM cipher
	block, err := aes.NewCipher(secret)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt and seal
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt decrypts data using AES-GCM with a shared secret derived from ECDH
func Decrypt(publicKey *ecdh.PublicKey, privateKey *ecdh.PrivateKey, ciphertext []byte) ([]byte, error) {
	if publicKey == nil || privateKey == nil {
		return nil, fmt.Errorf("public key and private key cannot be nil")
	}
	if len(ciphertext) == 0 {
		return nil, fmt.Errorf("ciphertext to decrypt cannot be empty")
	}

	// Generate shared secret
	secret, err := privateKey.ECDH(publicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to generate shared secret: %w", err)
	}

	// Create AES-GCM cipher
	block, err := aes.NewCipher(secret)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	return plaintext, nil
}

// WriteKeyPairToFiles writes ECDH key pair to files with specified permissions
func WriteKeyPairToFiles(keyPair *KeyPair, privateKeyPath, publicKeyPath string) error {
	// Write private key with restricted permissions (600 - owner read/write only)
	privPEM := EncodePrivateKeyToPEM(keyPair.PrivateKey)
	if err := os.WriteFile(privateKeyPath, privPEM, 0600); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}

	// Write public key with less restrictive permissions (644 - owner read/write, others read)
	pubPEM := EncodePublicKeyToPEM(keyPair.PublicKey)
	if err := os.WriteFile(publicKeyPath, pubPEM, 0644); err != nil {
		return fmt.Errorf("failed to write public key: %w", err)
	}

	return nil
}

// ReadPrivateKeyFromFile reads and parses an ECDH private key from a file
func ReadPrivateKeyFromFile(privateKeyPath string) (*ecdh.PrivateKey, error) {
	privPEM, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key file: %w", err)
	}

	block, _ := pem.Decode(privPEM)
	if block == nil || !strings.Contains(block.Type, "PRIVATE KEY") {
		return nil, fmt.Errorf("failed to decode PEM block containing private key")
	}

	// Parse the private key directly as an EC key
	curve := ecdh.P256()
	privateKey, err := curve.NewPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to create ECDH private key: %w", err)
	}

	return privateKey, nil
}

// ReadPublicKeyFromFile reads and parses an ECDH public key from a file
func ReadPublicKeyFromFile(publicKeyPath string) (*ecdh.PublicKey, error) {
	pubPEM, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read public key file: %w", err)
	}

	block, _ := pem.Decode(pubPEM)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, fmt.Errorf("failed to decode PEM block containing public key")
	}

	// Parse the public key directly as an EC key
	curve := ecdh.P256()
	publicKey, err := curve.NewPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to create ECDH public key: %w", err)
	}

	return publicKey, nil
}
