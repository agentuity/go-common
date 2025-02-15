package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"encoding/asn1"
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
func EncodePrivateKeyToPEM(privateKey *ecdh.PrivateKey) []byte {
	// Use OID for id-ecPublicKey with P-256 curve: 1.2.840.10045.3.1.7
	pkcs8Key := pkcs8ECDHPrivateKey{
		Version:    0,
		Algorithm:  asn1.ObjectIdentifier{1, 2, 840, 10045, 3, 1, 7},
		PrivateKey: privateKey.Bytes(),
	}

	privDER, err := asn1.Marshal(pkcs8Key)
	if err != nil {
		panic(fmt.Sprintf("failed to ASN.1 marshal private key: %s", err))
	}

	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privDER,
	})

	return privPEM
}

// EncodePublicKeyToPEM converts an ECDH public key to PEM format using PKIX
func EncodePublicKeyToPEM(publicKey *ecdh.PublicKey) []byte {
	// Get raw public key bytes
	pubBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal public key: %s", err))
	}

	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	})
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

// ASN.1 structure for a minimal PKCS#8-like ECDH private key.
// (Note: Go’s x509.MarshalPKCS8PrivateKey doesn’t support crypto/ecdh types,
// so we do a minimal manual ASN.1 wrap.)
type pkcs8ECDHPrivateKey struct {
	Version    int
	Algorithm  asn1.ObjectIdentifier
	PrivateKey []byte
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
	keyBytes, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key file: %w", err)
	}

	block, _ := pem.Decode(keyBytes)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	var pkcs8Key pkcs8ECDHPrivateKey
	if _, err = asn1.Unmarshal(block.Bytes, &pkcs8Key); err != nil {
		return nil, fmt.Errorf("failed to ASN.1 unmarshal private key: %w", err)
	}

	// Verify OID matches P-256
	expectedOID := asn1.ObjectIdentifier{1, 2, 840, 10045, 3, 1, 7}
	if !pkcs8Key.Algorithm.Equal(expectedOID) {
		return nil, fmt.Errorf("the private key is not a P-256 ECDH key")
	}

	ecdhPriv, err := ecdh.P256().NewPrivateKey(pkcs8Key.PrivateKey)
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

	block, _ := pem.Decode(keyBytes)
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
