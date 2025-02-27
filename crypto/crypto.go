package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
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
func EncodePrivateKeyToPEM(privateKey *ecdh.PrivateKey) ([]byte, error) {
	privDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to ASN.1 marshal private key: %w", err)
	}

	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privDER,
	})

	return privPEM, nil
}

// EncodePublicKeyToPEM converts an ECDH public key to PEM format using PKIX
func EncodePublicKeyToPEM(publicKey *ecdh.PublicKey) ([]byte, error) {
	// Get raw public key bytes
	pubBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal public key: %w", err)
	}

	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	})
	return pubPEM, nil
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
	privPEM, err := EncodePrivateKeyToPEM(keyPair.PrivateKey)
	if err != nil {
		return fmt.Errorf("failed to encode private key: %w", err)
	}
	if err := os.WriteFile(privateKeyPath, privPEM, 0600); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}

	// Write public key with less restrictive permissions (644 - owner read/write, others read)
	pubPEM, err := EncodePublicKeyToPEM(keyPair.PublicKey)
	if err != nil {
		return fmt.Errorf("failed to encode public key: %w", err)
	}
	if err := os.WriteFile(publicKeyPath, pubPEM, 0644); err != nil {
		return fmt.Errorf("failed to write public key: %w", err)
	}

	return nil
}

// ReadPrivateKeyFromFile reads and parses an ECDH private key from a file
func ReadPrivateKeyFromFile(privateKeyPath string) (*ecdh.PrivateKey, error) {
	keyBytes, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key file: %w", err)
	}
	return ReadPrivateKey(keyBytes)
}

// ReadPrivateKey reads and parses an ECDH private key from pem encoded bytes
func ReadPrivateKey(privateKey []byte) (*ecdh.PrivateKey, error) {
	block, _ := pem.Decode(privateKey)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	pkcs8Key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PKCS8 private key: %w", err)
	}

	ecdsaPriv, ok := pkcs8Key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("the private key is not an ECDSA key")
	}

	ecdhPriv, err := ecdh.P256().NewPrivateKey(ecdsaPriv.D.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to reconstruct ECDH private key: %w", err)
	}

	return ecdhPriv, nil
}

// ReadPublicKeyFromFile reads and parses an ECDH public key from a file
func ReadPublicKeyFromFile(publicKeyPath string) (*ecdh.PublicKey, error) {
	keyBytes, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read public key file: %w", err)
	}
	return ReadPublicKey(keyBytes)
}

// ReadPublicKey reads and parses an ECDH public key from pem encoded bytes
func ReadPublicKey(publicKey []byte) (*ecdh.PublicKey, error) {
	block, _ := pem.Decode(publicKey)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	parsedKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PKIX public key: %w", err)
	}

	// x509.ParsePKIXPublicKey returns an *ecdsa.PublicKey for EC keys.
	ecdsaPub, ok := parsedKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("the public key is not an ECDSA key")
	}

	// Convert the ECDSA public key to an uncompressed EC point.
	// For a P-256 curve, the uncompressed point is 65 bytes:
	// 0x04 || X (32 bytes) || Y (32 bytes)
	byteLen := (ecdsaPub.Curve.Params().BitSize + 7) / 8
	uncompressed := make([]byte, 1+2*byteLen)
	uncompressed[0] = 4

	// Get X and Y coordinates, padded to the correct length.
	xBytes := ecdsaPub.X.Bytes()
	yBytes := ecdsaPub.Y.Bytes()

	copy(uncompressed[1+byteLen-len(xBytes):1+byteLen], xBytes)
	copy(uncompressed[1+2*byteLen-len(yBytes):1+2*byteLen], yBytes)

	// Now create an ECDH public key from the uncompressed bytes.
	ecdhPub, err := ecdh.P256().NewPublicKey(uncompressed)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to ECDH public key: %w", err)
	}

	return ecdhPub, nil
}
