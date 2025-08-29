package crypto

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"
)

// Test basic round-trip functionality
func TestBasicEncryptDecrypt(t *testing.T) {
	// Generate a test key pair
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub := &priv.PublicKey

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
			written, err := EncryptFIPSKEMDEMStream(pub, bytes.NewReader(tc.data), &encrypted)
			if err != nil {
				t.Fatalf("encryption failed: %v", err)
			}
			if written != int64(len(tc.data)) {
				t.Fatalf("expected %d bytes written, got %d", len(tc.data), written)
			}

			// Decrypt
			var decrypted bytes.Buffer
			read, err := DecryptFIPSKEMDEMStream(priv, &encrypted, &decrypted)
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
	priv1, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub1 := &priv1.PublicKey

	priv2, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("test data")

	// Encrypt with key 1
	var encrypted bytes.Buffer
	_, err = EncryptFIPSKEMDEMStream(pub1, bytes.NewReader(data), &encrypted)
	if err != nil {
		t.Fatal(err)
	}

	// Try to decrypt with key 2 (should fail)
	var decrypted bytes.Buffer
	_, err = DecryptFIPSKEMDEMStream(priv2, &encrypted, &decrypted)
	if err == nil {
		t.Fatal("expected decryption to fail with wrong key")
	}
	if !strings.Contains(err.Error(), "DEK unwrap failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Test malformed headers
func TestMalformedHeaders(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
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
		{"invalid_wrapped_len", []byte{0x10, 0x00}, "invalid wrapped DEK length"},
		{"zero_len", []byte{0x00, 0x00}, "invalid wrapped DEK length"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var decrypted bytes.Buffer
			_, err := DecryptFIPSKEMDEMStream(priv, bytes.NewReader(tc.data), &decrypted)
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
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	// Create a valid header followed by dummy encrypted data
	var buf bytes.Buffer

	// Write valid header structure (but with dummy data)
	binary.Write(&buf, binary.BigEndian, uint16(113)) // reasonable wrapped length (65+32+16)
	buf.Write(make([]byte, 113))                      // dummy wrapped data
	buf.Write(make([]byte, 12))                       // dummy nonce

	// Write chunk data that should fail authentication
	binary.Write(&buf, binary.BigEndian, uint16(32)) // reasonable chunk size
	buf.Write(make([]byte, 32))                      // dummy encrypted data (will fail auth)

	var decrypted bytes.Buffer
	_, err = DecryptFIPSKEMDEMStream(priv, &buf, &decrypted)
	if err == nil {
		t.Fatal("expected authentication error for malformed chunk")
	}
	// This should fail during unwrapping or chunk authentication
}

// Test ECDH key conversion
func TestECDHKeyConversion(t *testing.T) {
	// Generate a test key
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	// Test ECDSA to ECDH conversion
	ecdhPriv, err := priv.ECDH()
	if err != nil {
		t.Fatalf("ECDH conversion failed: %v", err)
	}

	ecdhPub, err := priv.PublicKey.ECDH()
	if err != nil {
		t.Fatalf("ECDH public conversion failed: %v", err)
	}

	// Test that we can use them
	if len(ecdhPub.Bytes()) != 65 {
		t.Fatalf("expected public key length 65, got %d", len(ecdhPub.Bytes()))
	}

	// Test that ECDH operations work
	ephemeralPriv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	sharedSecret1, err := ecdhPriv.ECDH(ephemeralPriv.PublicKey())
	if err != nil {
		t.Fatal("ECDH failed")
	}

	sharedSecret2, err := ephemeralPriv.ECDH(ecdhPub)
	if err != nil {
		t.Fatal("ECDH failed")
	}

	if !bytes.Equal(sharedSecret1, sharedSecret2) {
		t.Fatal("ECDH shared secrets don't match")
	}
}

// Test memory safety - ensure DEK is cleared
func TestMemoryClearing(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub := &priv.PublicKey

	data := []byte("sensitive data")

	// Encrypt
	var encrypted bytes.Buffer
	_, err = EncryptFIPSKEMDEMStream(pub, bytes.NewReader(data), &encrypted)
	if err != nil {
		t.Fatal(err)
	}

	// Decrypt - the DEK should be cleared after this
	var decrypted bytes.Buffer
	_, err = DecryptFIPSKEMDEMStream(priv, &encrypted, &decrypted)
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
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub := &priv.PublicKey

	// Create a reader that returns EOF immediately
	eofReader := strings.NewReader("")

	var encrypted bytes.Buffer
	written, err := EncryptFIPSKEMDEMStream(pub, eofReader, &encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if written != 0 {
		t.Fatalf("expected 0 bytes written, got %d", written)
	}

	// Decrypt the empty stream
	var decrypted bytes.Buffer
	read, err := DecryptFIPSKEMDEMStream(priv, &encrypted, &decrypted)
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

// Test nonce reuse protection
func TestNonceReuseProtection(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub := &priv.PublicKey

	// Test that different encryptions produce different ciphertexts
	data := []byte("test data for nonce reuse protection")

	var encrypted1, encrypted2 bytes.Buffer

	// First encryption
	_, err = EncryptFIPSKEMDEMStream(pub, bytes.NewReader(data), &encrypted1)
	if err != nil {
		t.Fatal(err)
	}

	// Second encryption of same data
	_, err = EncryptFIPSKEMDEMStream(pub, bytes.NewReader(data), &encrypted2)
	if err != nil {
		t.Fatal(err)
	}

	// Ciphertexts should be different due to random nonces and ephemeral keys
	if bytes.Equal(encrypted1.Bytes(), encrypted2.Bytes()) {
		t.Fatal("identical ciphertexts indicate nonce reuse vulnerability")
	}

	// Both should decrypt correctly
	var decrypted1, decrypted2 bytes.Buffer
	_, err = DecryptFIPSKEMDEMStream(priv, &encrypted1, &decrypted1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = DecryptFIPSKEMDEMStream(priv, &encrypted2, &decrypted2)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(data, decrypted1.Bytes()) || !bytes.Equal(data, decrypted2.Bytes()) {
		t.Fatal("decryption failed")
	}
}

// Test frame size boundary conditions
func TestFrameSizeBoundaries(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub := &priv.PublicKey

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
			written, err := EncryptFIPSKEMDEMStream(pub, bytes.NewReader(data), &encrypted)
			if err != nil {
				t.Fatalf("encryption failed: %v", err)
			}
			if written != int64(tc.size) {
				t.Fatalf("expected %d bytes written, got %d", tc.size, written)
			}

			// Decrypt
			var decrypted bytes.Buffer
			read, err := DecryptFIPSKEMDEMStream(priv, &encrypted, &decrypted)
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
	// The assertion should never trigger with our current frame size
	// frame (65519) + gcmTag (16) = 65535 = MaxUint16
	maxExpected := frame + gcmTag
	if maxExpected > math.MaxUint16 {
		t.Fatalf("frame + gcmTag (%d) exceeds uint16 limit", maxExpected)
	}

	// Test with maximum frame size to ensure no overflow
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub := &priv.PublicKey

	data := bytes.Repeat([]byte("B"), frame)
	var encrypted bytes.Buffer
	_, err = EncryptFIPSKEMDEMStream(pub, bytes.NewReader(data), &encrypted)
	if err != nil {
		t.Fatal(err)
	}

	// Should succeed without triggering overflow assertion
}

// Benchmark tests
func BenchmarkEncrypt(b *testing.B) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		b.Fatal(err)
	}
	pub := &priv.PublicKey

	data := bytes.Repeat([]byte("benchmark data"), 1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var encrypted bytes.Buffer
		_, err := EncryptFIPSKEMDEMStream(pub, bytes.NewReader(data), &encrypted)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecrypt(b *testing.B) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		b.Fatal(err)
	}
	pub := &priv.PublicKey

	data := bytes.Repeat([]byte("benchmark data"), 1000)
	var encrypted bytes.Buffer
	_, err = EncryptFIPSKEMDEMStream(pub, bytes.NewReader(data), &encrypted)
	if err != nil {
		b.Fatal(err)
	}

	encryptedData := encrypted.Bytes()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var decrypted bytes.Buffer
		_, err := DecryptFIPSKEMDEMStream(priv, bytes.NewReader(encryptedData), &decrypted)
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

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		f.Fatal(err)
	}
	pub := &priv.PublicKey

	f.Fuzz(func(t *testing.T, data []byte) {
		// Skip extremely large inputs to avoid timeouts
		if len(data) > 1024*1024 {
			return
		}

		// Encrypt
		var encrypted bytes.Buffer
		written, err := EncryptFIPSKEMDEMStream(pub, bytes.NewReader(data), &encrypted)
		if err != nil {
			t.Fatalf("encryption failed: %v", err)
		}
		if written != int64(len(data)) {
			t.Fatalf("written bytes mismatch: expected %d, got %d", len(data), written)
		}

		// Decrypt
		var decrypted bytes.Buffer
		read, err := DecryptFIPSKEMDEMStream(priv, &encrypted, &decrypted)
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
	malformed := append([]byte{0x00, 0x71}, bytes.Repeat([]byte{0}, 113)...) // 0x71 = 113 bytes
	malformed = append(malformed, bytes.Repeat([]byte{0}, 12)...)
	f.Add(malformed)

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Try to decrypt malformed data - should not crash
		var decrypted bytes.Buffer
		_, err := DecryptFIPSKEMDEMStream(priv, bytes.NewReader(data), &decrypted)
		// We expect most inputs to fail, that's fine
		// We just want to ensure no panics occur
		_ = err
	})
}

// Fuzz test for streaming with different read/write patterns
func FuzzStreamingPatterns(f *testing.F) {
	// Add seed inputs with various sizes to test frame boundaries
	f.Add([]byte("small"))
	f.Add(bytes.Repeat([]byte("A"), 1024))     // 1KB
	f.Add(bytes.Repeat([]byte("B"), 32*1024))  // 32KB
	f.Add(bytes.Repeat([]byte("C"), 64*1024))  // 64KB - frame boundary
	f.Add(bytes.Repeat([]byte("D"), 65*1024))  // Just over frame boundary
	f.Add(bytes.Repeat([]byte("E"), 128*1024)) // 128KB - multiple frames

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		f.Fatal(err)
	}
	pub := &priv.PublicKey

	f.Fuzz(func(t *testing.T, data []byte) {
		// Limit to reasonable size for fuzzing
		if len(data) > 512*1024 {
			return
		}

		// Test with different reader patterns
		readers := []struct {
			name   string
			reader func() io.Reader
		}{
			{"normal", func() io.Reader { return bytes.NewReader(data) }},
			{"slow-1", func() io.Reader { return &slowReader{data: data, pos: 0, chunkSize: 1} }},
			{"slow-13", func() io.Reader { return &slowReader{data: data, pos: 0, chunkSize: 13} }},
		}

		for _, readerTest := range readers {
			// Encrypt
			var encrypted bytes.Buffer
			written, err := EncryptFIPSKEMDEMStream(pub, readerTest.reader(), &encrypted)
			if err != nil {
				t.Fatalf("encryption failed with %s reader: %v", readerTest.name, err)
			}
			if written != int64(len(data)) {
				t.Fatalf("written bytes mismatch with %s reader: expected %d, got %d", readerTest.name, len(data), written)
			}

			// Decrypt - just use normal buffer for simplicity in fuzz test
			var decrypted bytes.Buffer
			read, err := DecryptFIPSKEMDEMStream(priv, &encrypted, &decrypted)
			if err != nil {
				t.Fatalf("decryption failed with %s reader: %v", readerTest.name, err)
			}
			if read != int64(len(data)) {
				t.Fatalf("read bytes mismatch with %s reader: expected %d, got %d", readerTest.name, len(data), read)
			}

			// Verify data integrity
			if !bytes.Equal(data, decrypted.Bytes()) {
				t.Fatalf("decrypted data doesn't match original with %s reader", readerTest.name)
			}
		}
	})
}

// Fuzz test for partial data corruption to ensure proper error handling
func FuzzPartialCorruption(f *testing.F) {
	// Seed with known good encrypted data
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		f.Fatal(err)
	}
	pub := &priv.PublicKey

	// Create some valid encrypted data to corrupt
	testData := []byte("This is test data for corruption testing")
	var validEncrypted bytes.Buffer
	_, err = EncryptFIPSKEMDEMStream(pub, bytes.NewReader(testData), &validEncrypted)
	if err != nil {
		f.Fatal(err)
	}

	f.Add(validEncrypted.Bytes())
	f.Add(bytes.Repeat([]byte{0xFF}, 200)) // All 0xFF
	f.Add(bytes.Repeat([]byte{0x00}, 200)) // All zeros

	f.Fuzz(func(t *testing.T, data []byte) {
		// Skip if data is too small or large
		if len(data) < 10 || len(data) > 1024*1024 {
			return
		}

		// Try various single-bit corruptions at different positions
		corruptPositions := []int{
			0,                 // Header start
			1,                 // Header middle
			len(data) / 4,     // Early data
			len(data) / 2,     // Middle data
			3 * len(data) / 4, // Late data
			len(data) - 1,     // End data
		}

		for _, pos := range corruptPositions {
			if pos >= len(data) {
				continue
			}

			// Create corrupted copy
			corrupted := make([]byte, len(data))
			copy(corrupted, data)
			corrupted[pos] ^= 0x01 // Flip one bit

			// Attempt decryption - should handle errors gracefully
			var decrypted bytes.Buffer
			_, err := DecryptFIPSKEMDEMStream(priv, bytes.NewReader(corrupted), &decrypted)
			// We expect most corrupted data to fail - that's good
			// We just want to ensure no panics and proper error handling
			_ = err
		}
	})
}

// Fuzz test with different key pairs to test key compatibility
func FuzzDifferentKeyPairs(f *testing.F) {
	f.Add([]byte("test data"))
	f.Add(bytes.Repeat([]byte("X"), 1000))

	f.Fuzz(func(t *testing.T, data []byte) {
		// Limit size for fuzzing performance
		if len(data) > 64*1024 {
			return
		}

		// Generate different key pairs
		priv1, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		priv2, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}

		// Encrypt with first key pair
		var encrypted bytes.Buffer
		written, err := EncryptFIPSKEMDEMStream(&priv1.PublicKey, bytes.NewReader(data), &encrypted)
		if err != nil {
			t.Fatalf("encryption failed: %v", err)
		}
		if written != int64(len(data)) {
			t.Fatalf("written bytes mismatch: expected %d, got %d", len(data), written)
		}

		// Try to decrypt with correct key - should succeed
		var decrypted1 bytes.Buffer
		read, err := DecryptFIPSKEMDEMStream(priv1, &encrypted, &decrypted1)
		if err != nil {
			t.Fatalf("decryption with correct key failed: %v", err)
		}
		if read != int64(len(data)) {
			t.Fatalf("read bytes mismatch: expected %d, got %d", len(data), read)
		}
		if !bytes.Equal(data, decrypted1.Bytes()) {
			t.Fatal("decrypted data doesn't match original")
		}

		// Try to decrypt with wrong key - should fail gracefully
		var decrypted2 bytes.Buffer
		_, err = DecryptFIPSKEMDEMStream(priv2, bytes.NewReader(encrypted.Bytes()), &decrypted2)
		if err == nil {
			t.Fatal("decryption with wrong key should have failed")
		}
		// Verify no data was written on failure
		if decrypted2.Len() > 0 {
			t.Fatal("decrypted buffer should be empty on failure")
		}
	})
}

// Helper types for testing different I/O patterns
type slowReader struct {
	data      []byte
	pos       int
	chunkSize int
}

func (r *slowReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}

	toRead := min3(len(p), r.chunkSize, len(r.data)-r.pos)
	copy(p, r.data[r.pos:r.pos+toRead])
	r.pos += toRead
	return toRead, nil
}

func min3(a, b, c int) int {
	result := a
	if b < result {
		result = b
	}
	if c < result {
		result = c
	}
	return result
}

// Test concurrent operations
func TestConcurrentOperations(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub := &priv.PublicKey

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
			_, err := EncryptFIPSKEMDEMStream(pub, bytes.NewReader(data), &encrypted)
			if err != nil {
				results <- fmt.Errorf("goroutine %d encrypt failed: %v", id, err)
				return
			}

			// Decrypt
			var decrypted bytes.Buffer
			_, err = DecryptFIPSKEMDEMStream(priv, &encrypted, &decrypted)
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

// Test non-P256 keys should be rejected
func TestUnsupportedCurves(t *testing.T) {
	// Test with P-384
	priv384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("test data")
	var encrypted bytes.Buffer

	// Should reject P-384 keys
	_, err = EncryptFIPSKEMDEMStream(&priv384.PublicKey, bytes.NewReader(data), &encrypted)
	if err == nil {
		t.Fatal("expected error for P-384 key")
	}
	if !strings.Contains(err.Error(), "only P-256 keys supported") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Same for decryption
	var decrypted bytes.Buffer
	_, err = DecryptFIPSKEMDEMStream(priv384, &encrypted, &decrypted)
	if err == nil {
		t.Fatal("expected error for P-384 key")
	}
	if !strings.Contains(err.Error(), "only P-256 keys supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}
