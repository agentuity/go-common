package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

// ErrChunkSizeTooLarge is returned when a chunk size exceeds the maximum allowed size.
var ErrChunkSizeTooLarge = errors.New("chunk size too large")

// deriveKey derives a 32-byte encryption key from a string using SHA-256
func deriveKey(key string) []byte {
	hash := sha256.Sum256([]byte(key))
	return hash[:]
}

// EncryptStream encrypts data from a reader using a chunked approach
// where each chunk has its own nonce and authentication tag.
// This is secure for large files and streaming data sources.
// The format of each chunk is: [4-byte chunk size (little-endian)][nonce][encrypted data][tag]
func EncryptStream(reader io.Reader, writer io.WriteCloser, key string) error {
	defer writer.Close()

	// Derive 32-byte key for AES-256
	derivedKey := deriveKey(key)

	// Create AES-256 cipher
	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return fmt.Errorf("failed to create AES cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	chunkSize := 64 * 1024 // 64KB chunks
	buf := make([]byte, chunkSize)

	for {
		// Read a chunk
		n, err := reader.Read(buf)
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read input: %w", err)
		}
		if n == 0 {
			if err == io.EOF {
				break
			}
			continue
		}

		// Generate a new nonce for each chunk
		nonce := make([]byte, nonceSize)
		if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
			return fmt.Errorf("failed to generate nonce: %w", err)
		}

		// Encrypt the chunk
		encrypted := gcm.Seal(nil, nonce, buf[:n], nil)

		// Write chunk size (4 bytes, little-endian)
		// The size includes only the encrypted data, not the nonce
		sizeBytes := []byte{
			byte(len(encrypted)),
			byte(len(encrypted) >> 8),
			byte(len(encrypted) >> 16),
			byte(len(encrypted) >> 24),
		}
		if _, err := writer.Write(sizeBytes); err != nil {
			return fmt.Errorf("failed to write chunk size: %w", err)
		}

		// Write nonce
		if _, err := writer.Write(nonce); err != nil {
			return fmt.Errorf("failed to write nonce: %w", err)
		}

		// Write encrypted chunk
		if _, err := writer.Write(encrypted); err != nil {
			return fmt.Errorf("failed to write encrypted chunk: %w", err)
		}
	}

	return nil
}

// DecryptStream decrypts data from a reader (like an HTTP response body)
// using a chunked approach where each chunk has its own nonce and authentication tag.
// This is secure for large files and streaming data sources.
// The format of each chunk is: [4-byte chunk size (little-endian)][nonce][encrypted data][tag]
func DecryptStream(reader io.Reader, writer io.WriteCloser, key string) error {
	defer writer.Close()

	// Derive 32-byte key for AES-256
	derivedKey := deriveKey(key)

	// Create AES-256 cipher
	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return fmt.Errorf("failed to create AES cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()

	// Buffer for reading chunk size (4 bytes)
	sizeBuffer := make([]byte, 4)

	for {
		// Read chunk size (4 bytes)
		_, err := io.ReadFull(reader, sizeBuffer)
		if err == io.EOF {
			// End of stream
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read chunk size: %w", err)
		}

		// Convert 4 bytes to uint32 (chunk size, little-endian)
		chunkSize := uint32(sizeBuffer[0]) |
			uint32(sizeBuffer[1])<<8 |
			uint32(sizeBuffer[2])<<16 |
			uint32(sizeBuffer[3])<<24

		// Sanity check on chunk size
		if chunkSize > 10*1024*1024 { // 10MB max chunk size
			return ErrChunkSizeTooLarge
		}

		// Read nonce
		nonce := make([]byte, nonceSize)
		if _, err := io.ReadFull(reader, nonce); err != nil {
			return fmt.Errorf("failed to read nonce: %w", err)
		}

		// Read encrypted chunk
		encryptedChunk := make([]byte, chunkSize)
		if _, err := io.ReadFull(reader, encryptedChunk); err != nil {
			return fmt.Errorf("failed to read encrypted chunk: %w", err)
		}

		// Decrypt chunk
		plaintext, err := gcm.Open(nil, nonce, encryptedChunk, nil)
		if err != nil {
			return fmt.Errorf("failed to decrypt chunk: %w", err)
		}

		// Write decrypted data
		if _, err := writer.Write(plaintext); err != nil {
			return fmt.Errorf("failed to write decrypted data: %w", err)
		}
	}

	return nil
}
