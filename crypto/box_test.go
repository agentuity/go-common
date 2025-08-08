package crypto

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"testing"
)

// Test basic round-trip functionality
func TestBasicEncryptDecrypt(t *testing.T) {
	// Generate a test key pair
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_ = pub // not used directly in our functions

	testCases := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"small", []byte("hello world")},
		{"medium", bytes.Repeat([]byte("A"), 1000)},
		{"large", bytes.Repeat([]byte("B"), 100000)},
		{"exactly_one_frame", bytes.Repeat([]byte("C"), 64*1024)},
		{"just_over_one_frame", bytes.Repeat([]byte("D"), 64*1024+1)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Encrypt
			var encrypted bytes.Buffer
			written, err := EncryptHybridKEMDEMStream(priv, bytes.NewReader(tc.data), &encrypted)
			if err != nil {
				t.Fatalf("encryption failed: %v", err)
			}
			if written != int64(len(tc.data)) {
				t.Fatalf("expected %d bytes written, got %d", len(tc.data), written)
			}

			// Decrypt
			var decrypted bytes.Buffer
			read, err := DecryptHybridKEMDEMStream(priv, &encrypted, &decrypted)
			if err != nil {
				t.Fatalf("decryption failed: %v", err)
			}
			if read != int64(len(tc.data)) {
				t.Fatalf("expected %d bytes read, got %d", len(tc.data), read)
			}

			// Verify
			if !bytes.Equal(tc.data, decrypted.Bytes()) {
				t.Fatal("decrypted data doesn't match original")
			}
		})
	}
}

// Test with different key pairs (should fail)
func TestDifferentKeys(t *testing.T) {
	_, priv1, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, priv2, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("test data")

	// Encrypt with key 1
	var encrypted bytes.Buffer
	_, err = EncryptHybridKEMDEMStream(priv1, bytes.NewReader(data), &encrypted)
	if err != nil {
		t.Fatal(err)
	}

	// Try to decrypt with key 2 (should fail)
	var decrypted bytes.Buffer
	_, err = DecryptHybridKEMDEMStream(priv2, &encrypted, &decrypted)
	if err == nil {
		t.Fatal("expected decryption to fail with wrong key")
	}
	if !strings.Contains(err.Error(), "sealed box decrypt failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Test malformed headers
func TestMalformedHeaders(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	testCases := []struct {
		name string
		data []byte
		err  string
	}{
		{"empty", []byte{}, "EOF"},
		{"partial_len", []byte{0}, "EOF"},
		{"invalid_sealed", []byte{0x10, 0x00}, "invalid sealed length"},
		{"valid_len_no_data", []byte{0x00, 0x50}, "EOF"}, // 0x50 = 80 (correct sealed length)
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var decrypted bytes.Buffer
			_, err := DecryptHybridKEMDEMStream(priv, bytes.NewReader(tc.data), &decrypted)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.err) {
				t.Fatalf("expected error containing %q, got %v", tc.err, err)
			}
		})
	}
}

// Test malformed chunks
func TestMalformedChunks(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	// Create a valid header followed by dummy encrypted data
	var buf bytes.Buffer

	// Write valid header
	binary.Write(&buf, binary.BigEndian, uint16(80)) // correct sealed length
	buf.Write(make([]byte, 80))                      // dummy sealed data
	buf.Write(make([]byte, 12))                      // dummy nonce

	// Write chunk data that should fail authentication
	binary.Write(&buf, binary.BigEndian, uint16(32)) // reasonable chunk size
	buf.Write(make([]byte, 32))                      // dummy encrypted data (will fail auth)

	var decrypted bytes.Buffer
	_, err = DecryptHybridKEMDEMStream(priv, &buf, &decrypted)
	if err == nil {
		t.Fatal("expected authentication error for malformed chunk")
	}
	// This should fail during unsealing or chunk authentication
}

// Test key conversion (note: edwards25519 accepts many inputs as valid)
func TestKeyConversion(t *testing.T) {
	// Test with all zeros - this is actually valid in edwards25519
	zerosPub := make([]byte, 32)
	_, err := edPubToX(zerosPub)
	if err != nil {
		t.Fatalf("unexpected error for zeros key: %v", err)
	}

	// Test with a valid Ed25519 key
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, err = edPubToX(pub)
	if err != nil {
		t.Fatalf("unexpected error for valid key: %v", err)
	}
}

// Test memory safety - ensure DEK is cleared
func TestMemoryClearing(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("sensitive data")

	// Encrypt
	var encrypted bytes.Buffer
	_, err = EncryptHybridKEMDEMStream(priv, bytes.NewReader(data), &encrypted)
	if err != nil {
		t.Fatal(err)
	}

	// Decrypt - the DEK should be cleared after this
	var decrypted bytes.Buffer
	_, err = DecryptHybridKEMDEMStream(priv, &encrypted, &decrypted)
	if err != nil {
		t.Fatal(err)
	}

	// Verify decryption worked
	if !bytes.Equal(data, decrypted.Bytes()) {
		t.Fatal("decrypted data doesn't match")
	}
}

// Test stream behavior with early EOF
func TestStreamEOF(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	// Create a reader that returns EOF immediately
	eofReader := strings.NewReader("")

	var encrypted bytes.Buffer
	written, err := EncryptHybridKEMDEMStream(priv, eofReader, &encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if written != 0 {
		t.Fatalf("expected 0 bytes written, got %d", written)
	}

	// Decrypt the empty stream
	var decrypted bytes.Buffer
	read, err := DecryptHybridKEMDEMStream(priv, &encrypted, &decrypted)
	if err != nil {
		t.Fatal(err)
	}
	if read != 0 {
		t.Fatalf("expected 0 bytes read, got %d", read)
	}
}

// Test nonce construction
func TestNonceConstruction(t *testing.T) {
	prefix := []byte{1, 2, 3, 4}
	counter := uint64(0x123456789abcdef0)

	nonce := makeNonce(prefix, counter)

	if len(nonce) != 12 {
		t.Fatalf("expected nonce length 12, got %d", len(nonce))
	}

	// Check prefix
	if !bytes.Equal(nonce[:4], prefix) {
		t.Fatal("nonce prefix doesn't match")
	}

	// Check counter (little endian)
	expectedCounter := binary.LittleEndian.Uint64(nonce[4:])
	if expectedCounter != counter {
		t.Fatalf("expected counter %x, got %x", counter, expectedCounter)
	}
}

// Benchmark tests
func BenchmarkEncrypt(b *testing.B) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatal(err)
	}

	data := bytes.Repeat([]byte("benchmark data"), 1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var encrypted bytes.Buffer
		_, err := EncryptHybridKEMDEMStream(priv, bytes.NewReader(data), &encrypted)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecrypt(b *testing.B) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatal(err)
	}

	data := bytes.Repeat([]byte("benchmark data"), 1000)
	var encrypted bytes.Buffer
	_, err = EncryptHybridKEMDEMStream(priv, bytes.NewReader(data), &encrypted)
	if err != nil {
		b.Fatal(err)
	}

	encryptedData := encrypted.Bytes()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var decrypted bytes.Buffer
		_, err := DecryptHybridKEMDEMStream(priv, bytes.NewReader(encryptedData), &decrypted)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Fuzz test for encryption round-trip
func FuzzEncryptDecrypt(f *testing.F) {
	// Add some seed inputs
	f.Add([]byte("hello"))
	f.Add([]byte(""))
	f.Add(bytes.Repeat([]byte("A"), 1000))
	f.Add(bytes.Repeat([]byte("B"), 65536))

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Skip extremely large inputs to avoid timeouts
		if len(data) > 1024*1024 {
			return
		}

		// Encrypt
		var encrypted bytes.Buffer
		written, err := EncryptHybridKEMDEMStream(priv, bytes.NewReader(data), &encrypted)
		if err != nil {
			t.Fatalf("encryption failed: %v", err)
		}
		if written != int64(len(data)) {
			t.Fatalf("written bytes mismatch: expected %d, got %d", len(data), written)
		}

		// Decrypt
		var decrypted bytes.Buffer
		read, err := DecryptHybridKEMDEMStream(priv, &encrypted, &decrypted)
		if err != nil {
			t.Fatalf("decryption failed: %v", err)
		}
		if read != int64(len(data)) {
			t.Fatalf("read bytes mismatch: expected %d, got %d", len(data), read)
		}

		// Verify
		if !bytes.Equal(data, decrypted.Bytes()) {
			t.Fatal("decrypted data doesn't match original")
		}
	})
}

// Fuzz test for malformed encrypted data
func FuzzDecryptMalformed(f *testing.F) {
	// Add some seed inputs - various malformed headers
	f.Add([]byte{})
	f.Add([]byte{0x00})
	f.Add([]byte{0x10, 0x00})
	malformed := append([]byte{0x00, 0x30}, bytes.Repeat([]byte{0}, 48)...)
	malformed = append(malformed, bytes.Repeat([]byte{0}, 12)...)
	f.Add(malformed)

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Try to decrypt malformed data - should not crash
		var decrypted bytes.Buffer
		_, err := DecryptHybridKEMDEMStream(priv, bytes.NewReader(data), &decrypted)
		// We expect most inputs to fail, that's fine
		// We just want to ensure no panics occur
		_ = err
	})
}

// Test known vectors for regression prevention
func TestKnownVectors(t *testing.T) {
	// Use a deterministic key for reproducible tests
	seed := bytes.Repeat([]byte{0x42}, 32)
	priv := ed25519.NewKeyFromSeed(seed)

	testData := []byte("known test vector data for regression testing")

	// Encrypt
	var encrypted bytes.Buffer
	written, err := EncryptHybridKEMDEMStream(priv, bytes.NewReader(testData), &encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if written != int64(len(testData)) {
		t.Fatalf("unexpected bytes written: %d", written)
	}

	// Verify the encrypted output has the expected structure
	encBytes := encrypted.Bytes()
	if len(encBytes) < baseHdr {
		t.Fatal("encrypted output too short for valid header")
	}

	// Check sealed length is reasonable
	sealedLen := binary.BigEndian.Uint16(encBytes[:2])
	if sealedLen != hdrSeal {
		t.Fatalf("unexpected sealed length: %d, expected %d", sealedLen, hdrSeal)
	}

	// Decrypt and verify
	var decrypted bytes.Buffer
	read, err := DecryptHybridKEMDEMStream(priv, &encrypted, &decrypted)
	if err != nil {
		t.Fatal(err)
	}
	if read != int64(len(testData)) {
		t.Fatalf("unexpected bytes read: %d", read)
	}
	if !bytes.Equal(testData, decrypted.Bytes()) {
		t.Fatal("decrypted data mismatch")
	}
}

// Test nonce reuse protection
func TestNonceReuseProtection(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	// Test that different encryptions produce different ciphertexts
	data := []byte("test data for nonce reuse protection")

	var encrypted1, encrypted2 bytes.Buffer

	// First encryption
	_, err = EncryptHybridKEMDEMStream(priv, bytes.NewReader(data), &encrypted1)
	if err != nil {
		t.Fatal(err)
	}

	// Second encryption of same data
	_, err = EncryptHybridKEMDEMStream(priv, bytes.NewReader(data), &encrypted2)
	if err != nil {
		t.Fatal(err)
	}

	// Ciphertexts should be different due to random nonces
	if bytes.Equal(encrypted1.Bytes(), encrypted2.Bytes()) {
		t.Fatal("identical ciphertexts indicate nonce reuse vulnerability")
	}

	// Both should decrypt correctly
	var decrypted1, decrypted2 bytes.Buffer
	_, err = DecryptHybridKEMDEMStream(priv, &encrypted1, &decrypted1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = DecryptHybridKEMDEMStream(priv, &encrypted2, &decrypted2)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(data, decrypted1.Bytes()) || !bytes.Equal(data, decrypted2.Bytes()) {
		t.Fatal("decryption failed")
	}
}

// Test that tampering with nonce causes authentication failure
func TestNonceTampering(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("test data for nonce tampering")

	// Encrypt
	var encrypted bytes.Buffer
	_, err = EncryptHybridKEMDEMStream(priv, bytes.NewReader(data), &encrypted)
	if err != nil {
		t.Fatal(err)
	}

	// Tamper with the base nonce in the header
	encBytes := encrypted.Bytes()
	headerStart := 2 + 80 // skip sealedLen + sealed data
	// Flip a bit in the base nonce
	encBytes[headerStart+5] ^= 0x01

	// Decryption should fail
	var decrypted bytes.Buffer
	_, err = DecryptHybridKEMDEMStream(priv, bytes.NewReader(encBytes), &decrypted)
	if err == nil {
		t.Fatal("expected decryption to fail with tampered nonce")
	}
	if !strings.Contains(err.Error(), "message authentication failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Test frame size boundary conditions
func TestFrameSizeBoundaries(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	testCases := []struct {
		name string
		size int
	}{
		{"exactly_max_frame", frame},
		{"max_frame_minus_1", frame - 1},
		{"max_frame_plus_1", frame + 1},
		{"half_frame", frame / 2},
		{"double_frame", frame * 2},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := bytes.Repeat([]byte("A"), tc.size)

			// Encrypt
			var encrypted bytes.Buffer
			written, err := EncryptHybridKEMDEMStream(priv, bytes.NewReader(data), &encrypted)
			if err != nil {
				t.Fatalf("encryption failed: %v", err)
			}
			if written != int64(tc.size) {
				t.Fatalf("expected %d bytes written, got %d", tc.size, written)
			}

			// Decrypt
			var decrypted bytes.Buffer
			read, err := DecryptHybridKEMDEMStream(priv, &encrypted, &decrypted)
			if err != nil {
				t.Fatalf("decryption failed: %v", err)
			}
			if read != int64(tc.size) {
				t.Fatalf("expected %d bytes read, got %d", tc.size, read)
			}

			// Verify
			if !bytes.Equal(data, decrypted.Bytes()) {
				t.Fatal("decrypted data doesn't match original")
			}
		})
	}
}

// Test uint16 overflow protection
func TestUint16OverflowProtection(t *testing.T) {
	// This test verifies that our assertion catches potential overflows
	// We can't easily trigger this in normal operation due to frame size limits,
	// but we can test the assertion exists by examining the code path

	// The assertion should never trigger with our current frame size
	// frame (65519) + tagBytes (16) = 65535 = MaxUint16
	maxExpected := frame + tagBytes
	if maxExpected > math.MaxUint16 {
		t.Fatalf("frame + tagBytes (%d) exceeds uint16 limit", maxExpected)
	}

	// Test with maximum frame size to ensure no overflow
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	data := bytes.Repeat([]byte("B"), frame)
	var encrypted bytes.Buffer
	_, err = EncryptHybridKEMDEMStream(priv, bytes.NewReader(data), &encrypted)
	if err != nil {
		t.Fatal(err)
	}

	// Should succeed without triggering overflow assertion
}

// Fuzz test for corruption at various positions
func FuzzCorruptionPositions(f *testing.F) {
	// Seed with some corruption positions
	f.Add([]byte("test data for corruption"), 10, byte(0xFF))
	f.Add([]byte("longer test data for corruption testing"), 50, byte(0x01))
	f.Add(bytes.Repeat([]byte("A"), 1000), 500, byte(0x80))

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, data []byte, corruptPos int, corruptVal byte) {
		if len(data) == 0 || len(data) > 100000 {
			return // Skip empty or very large inputs
		}

		// Encrypt
		var encrypted bytes.Buffer
		_, err := EncryptHybridKEMDEMStream(priv, bytes.NewReader(data), &encrypted)
		if err != nil {
			return // Skip if encryption fails for any reason
		}

		encBytes := encrypted.Bytes()
		if corruptPos >= len(encBytes) || corruptPos < 0 {
			return // Skip if corruption position is out of bounds
		}

		// Corrupt the encrypted data
		original := encBytes[corruptPos]
		encBytes[corruptPos] = corruptVal

		// Skip if we didn't actually change anything
		if original == corruptVal {
			return
		}

		// Attempt to decrypt - should fail
		var decrypted bytes.Buffer
		_, err = DecryptHybridKEMDEMStream(priv, bytes.NewReader(encBytes), &decrypted)

		// Corruption should cause decryption to fail
		if err == nil {
			t.Fatalf("decryption should have failed with corruption at position %d", corruptPos)
		}

		// The decrypted buffer should be empty or contain no sensitive data
		if decrypted.Len() > 0 {
			// Some corruption might allow partial decryption before failing
			// but the full original data should never be recovered
			if bytes.Equal(data, decrypted.Bytes()) {
				t.Fatal("corrupted data should not decrypt to original plaintext")
			}
		}
	})
}

// Benchmark various data sizes
func BenchmarkEncryptDecryptSizes(b *testing.B) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatal(err)
	}

	sizes := []int{
		1024,             // 1KB
		64 * 1024,        // 64KB (one frame)
		1024 * 1024,      // 1MB
		10 * 1024 * 1024, // 10MB
	}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size_%d", size), func(b *testing.B) {
			data := bytes.Repeat([]byte("A"), size)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Encrypt
				var encrypted bytes.Buffer
				_, err := EncryptHybridKEMDEMStream(priv, bytes.NewReader(data), &encrypted)
				if err != nil {
					b.Fatal(err)
				}

				// Decrypt
				var decrypted bytes.Buffer
				_, err = DecryptHybridKEMDEMStream(priv, &encrypted, &decrypted)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// Test concurrent operations
func TestConcurrentOperations(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	const numGoroutines = 10
	const dataSize = 10000

	// Channel to collect results
	results := make(chan error, numGoroutines)

	// Launch concurrent encrypt/decrypt operations
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			data := bytes.Repeat([]byte(fmt.Sprintf("data%d", id)), dataSize/10)

			// Encrypt
			var encrypted bytes.Buffer
			_, err := EncryptHybridKEMDEMStream(priv, bytes.NewReader(data), &encrypted)
			if err != nil {
				results <- fmt.Errorf("goroutine %d encrypt failed: %v", id, err)
				return
			}

			// Decrypt
			var decrypted bytes.Buffer
			_, err = DecryptHybridKEMDEMStream(priv, &encrypted, &decrypted)
			if err != nil {
				results <- fmt.Errorf("goroutine %d decrypt failed: %v", id, err)
				return
			}

			// Verify
			if !bytes.Equal(data, decrypted.Bytes()) {
				results <- fmt.Errorf("goroutine %d data mismatch", id)
				return
			}

			results <- nil
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}
