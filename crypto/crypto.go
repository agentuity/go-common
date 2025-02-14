package crypto

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
)

// GenerateRSAKeyPair generates a new RSA key pair with the specified bit size
func GenerateRSAKeyPair(bits int) (*rsa.PrivateKey, *rsa.PublicKey, error) {
	privKey, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate RSA key pair: %w", err)
	}
	return privKey, &privKey.PublicKey, nil
}

// EncodePrivateKeyToPEM converts an RSA private key to PEM format using PKCS#8
func EncodePrivateKeyToPEM(privateKey *rsa.PrivateKey) []byte {
	privBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		// Since this is an internal conversion that should never fail for an RSA key,
		// we'll panic if it does. This would indicate a serious internal error.
		panic(fmt.Sprintf("failed to marshal private key: %v", err))
	}
	privPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: privBytes,
		},
	)
	return privPEM
}

// EncodePublicKeyToPEM converts an RSA public key to PEM format using PKIX
func EncodePublicKeyToPEM(publicKey *rsa.PublicKey) []byte {
	pubBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		// Since this is an internal conversion that should never fail for an RSA key,
		// we'll panic if it does. This would indicate a serious internal error.
		panic(fmt.Sprintf("failed to marshal public key: %v", err))
	}
	pubPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "PUBLIC KEY",
			Bytes: pubBytes,
		},
	)
	return pubPEM
}

// EncryptWithRSA encrypts data using RSA-OAEP with SHA256
func EncryptWithRSA(publicKey *rsa.PublicKey, data []byte, label []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data to encrypt cannot be empty")
	}

	hash := sha256.New()

	ciphertext, err := rsa.EncryptOAEP(hash, rand.Reader, publicKey, data, label)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt data: %w", err)
	}

	return ciphertext, nil
}

// DecryptWithRSA decrypts data using RSA-OAEP with SHA256
func DecryptWithRSA(privateKey *rsa.PrivateKey, ciphertext []byte, label []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, fmt.Errorf("ciphertext to decrypt cannot be empty")
	}

	hash := sha256.New()

	plaintext, err := rsa.DecryptOAEP(hash, rand.Reader, privateKey, ciphertext, label)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt data: %w", err)
	}

	return plaintext, nil
}

// WriteKeyPairToFiles writes RSA key pair to files with specified permissions
func WriteKeyPairToFiles(privateKey *rsa.PrivateKey, privateKeyPath, publicKeyPath string) error {
	// Write private key with restricted permissions (600 - owner read/write only)
	privPEM := EncodePrivateKeyToPEM(privateKey)
	if err := os.WriteFile(privateKeyPath, privPEM, 0600); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}

	// Write public key with less restrictive permissions (644 - owner read/write, others read)
	pubPEM := EncodePublicKeyToPEM(&privateKey.PublicKey)
	if err := os.WriteFile(publicKeyPath, pubPEM, 0644); err != nil {
		return fmt.Errorf("failed to write public key: %w", err)
	}

	return nil
}

// PrivateKeyFromBase64 decodes a base64 encoded PEM string and parses it into an RSA private key
func PrivateKeyFromBase64(buf []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(buf)
	if block == nil || !strings.Contains(block.Type, "PRIVATE KEY") {
		return nil, fmt.Errorf("failed to decode PEM block containing private key")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	privKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is not an RSA private key")
	}

	return privKey, nil
}

// ReadPrivateKeyFromFile reads and parses an RSA private key from a file
func ReadPrivateKeyFromFile(privateKeyPath string) (*rsa.PrivateKey, error) {
	privPEM, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key file: %w", err)
	}

	return PrivateKeyFromBase64(privPEM)
}

// ReadPublicKeyFromFile reads and parses an RSA public key from a file
func ReadPublicKeyFromFile(publicKeyPath string) (*rsa.PublicKey, error) {
	pubPEM, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read public key file: %w", err)
	}

	block, _ := pem.Decode(pubPEM)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, fmt.Errorf("failed to decode PEM block containing public key")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}

	pubKey, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("key is not an RSA public key")
	}

	return pubKey, nil
}
