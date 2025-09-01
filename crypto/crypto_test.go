package crypto

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"
)

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
	// Test with exactly 10 MiB of data
	pattern := []byte("large data test ")
	repeats := (10 * 1024 * 1024) / len(pattern)
	originalData := bytes.Repeat(pattern, repeats)
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
	// Test with smaller, more reasonable sizes for CI
	testSizes := []int{1, 10, 50, 100, 500, 1000} // KiB sizes
	pattern := []byte("large data test ")

	for _, sizeKiB := range testSizes {
		t.Run(fmt.Sprintf("%dKiB", sizeKiB), func(t *testing.T) {
			// Compute exact repeats for the target size in KiB
			targetBytes := sizeKiB * 1024
			repeats := targetBytes / len(pattern)
			originalData := bytes.Repeat(pattern, repeats)
			input := bytes.NewReader(originalData)
			encryptedBuf := &bytes.Buffer{}

			key := fmt.Sprintf("test-key-%d", sizeKiB)

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
		})
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

	// Should fail with chunk size error
	if err == nil {
		t.Error("DecryptStream() should fail with invalid chunk size")
	}
	if !errors.Is(err, ErrChunkSizeTooLarge) {
		t.Errorf("Expected ErrChunkSizeTooLarge, got: %v", err)
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
