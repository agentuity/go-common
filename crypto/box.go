// Package crypto implements a **FIPS 140-3 compliant KEM-DEM envelope encryption scheme**
// suitable for multi-gigabyte streams using ECDSA P-256 and AES-256-GCM.
// This design is Go 1.24+ FIPS compatible (GOFIPS140=v1.0.0) and depends only
// on standard library crypto packages.
//
// ──────────────────────────  Design summary  ─────────────────────────────
//
//	⚙  KEM  (Key-Encapsulation Mechanism)
//	    • ECDH P-256 + AES-256-GCM for DEK wrapping
//	    • Output: variable-size encrypted DEK (48-byte DEK + 16-byte GCM tag + ephemeral pubkey)
//	    • Provides forward secrecy for each blob
//
//	⚙  DEM  (Data-Encapsulation Mechanism)
//	    • AES-256-GCM in ~64 KiB framed chunks (65519 bytes max)
//	    • Nonce = 4-byte random prefix ∥ 8-byte little-endian counter
//	    • First frame authenticates header via associated data (prevents tampering)
//	    • Constant ~64 KiB RAM, O(1) header re-wrap for key rotation
//
//	⚙  Fleet key
//	    • Single ECDSA P-256 key-pair per customer
//	    • Public key used directly for ECDH operations
//	    • Private key stored in cloud secret store and fetched at boot
//
//	File layout
//	 ┌─────────────────────────────────────────────────────────────────────────┐
//	 │ uint16 wrappedLen │ 97B wrapped DEK │ 12B base nonce │ frames... │
//	 └─────────────────────────────────────────────────────────────────────────┘
//	                             ▲                    ▲
//	                             │                    └─ AES-256-GCM frames
//	                             └─ ECDH + AES-GCM wrapped DEK
//
//	Security properties
//	• Confidentiality & integrity: AES-256-GCM per frame
//	• Header authentication: first frame includes header as associated data
//	• Forward-secrecy per object: new ephemeral ECDSA key each encryption
//	• Key rotation: requires re-wrapping only the ~100-byte header
//	• FIPS 140-3 compliant: uses only approved algorithms
//
//	Typical workflow
//	────────────────
//	  Publisher:
//	    1) generate DEK, encrypt stream → dst
//	    2) ephemeral ECDH + AES-GCM wrap DEK with fleet public key
//	    3) write header {len, wrapped DEK, nonce} - ~109 bytes total
//	    4) first frame includes header as associated data for authentication
//
//	  Machine node:
//	    1) read header, unwrap DEK with fleet private key via ECDH
//	    2) stream-decrypt frames on the fly (first frame verifies header)
//
// Public API
// ──────────
//
//	EncryptFIPSKEMDEMStream(pub *ecdsa.PublicKey, r io.Reader, w io.Writer)
//	DecryptFIPSKEMDEMStream(priv *ecdsa.PrivateKey, r io.Reader, w io.Writer)
//
// Both return the number of plaintext bytes processed and ensure that
// every error path is authenticated-failure-safe.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"math"
)

const (
	frame     = 65519 // max frame size that fits in uint16 with GCM overhead
	dekSize   = 32    // AES-256 key size
	gcmTag    = 16    // GCM authentication tag size
	pubkeyLen = 65    // uncompressed P-256 public key (1 byte + 32 + 32 bytes)
	// wrapped DEK = ephemeral pubkey + encrypted DEK + GCM tag
	wrappedDEKSize = pubkeyLen + dekSize + gcmTag // 65 + 32 + 16 = 113 bytes
	baseHdr        = 2 + wrappedDEKSize + 12      // len field + wrapped DEK + base nonce = 127 bytes
)

// ---------------- internal helpers ----------------

// wrapDEKWithECDH wraps a DEK using ECDH + AES-GCM
func wrapDEKWithECDH(dek []byte, recipientPub *ecdsa.PublicKey) ([]byte, error) {
	// Convert ECDSA public key to ECDH public key
	recipientECDH, err := recipientPub.ECDH()
	if err != nil {
		return nil, err
	}

	// Generate ephemeral ECDH key pair
	ephemeralPriv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	// Perform ECDH
	sharedSecret, err := ephemeralPriv.ECDH(recipientECDH)
	if err != nil {
		return nil, errors.New("ECDH failed")
	}
	defer func() {
		for i := range sharedSecret {
			sharedSecret[i] = 0
		}
	}()

	// Derive AES key from shared secret using SHA-256
	kek := sha256.Sum256(sharedSecret)
	defer func() {
		for i := range kek {
			kek[i] = 0
		}
	}()

	// Encrypt DEK with AES-256-GCM
	block, err := aes.NewCipher(kek[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nil, nonce, dek, nil)

	// Format: ephemeral_pubkey || nonce || ciphertext
	ephemeralPubBytes := ephemeralPriv.PublicKey().Bytes()
	result := make([]byte, 0, len(ephemeralPubBytes)+len(nonce)+len(ciphertext))
	result = append(result, ephemeralPubBytes...)
	result = append(result, nonce...)
	result = append(result, ciphertext...)

	return result, nil
}

// unwrapDEKWithECDH unwraps a DEK using ECDH + AES-GCM
func unwrapDEKWithECDH(wrapped []byte, recipientPriv *ecdsa.PrivateKey) ([]byte, error) {
	if len(wrapped) < pubkeyLen+12+dekSize+gcmTag {
		return nil, errors.New("wrapped DEK too short")
	}

	// Parse components
	ephemeralPubBytes := wrapped[:pubkeyLen]
	remaining := wrapped[pubkeyLen:]

	// Convert recipient private key to ECDH
	recipientECDH, err := recipientPriv.ECDH()
	if err != nil {
		return nil, err
	}

	// Parse ephemeral public key
	ephemeralPubECDH, err := ecdh.P256().NewPublicKey(ephemeralPubBytes)
	if err != nil {
		return nil, err
	}

	// Perform ECDH
	sharedSecret, err := recipientECDH.ECDH(ephemeralPubECDH)
	if err != nil {
		return nil, errors.New("ECDH failed")
	}
	defer func() {
		for i := range sharedSecret {
			sharedSecret[i] = 0
		}
	}()

	// Derive AES key from shared secret
	kek := sha256.Sum256(sharedSecret)
	defer func() {
		for i := range kek {
			kek[i] = 0
		}
	}()

	// Decrypt DEK
	block, err := aes.NewCipher(kek[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(remaining) < nonceSize {
		return nil, errors.New("invalid wrapped DEK format")
	}

	nonce := remaining[:nonceSize]
	ciphertext := remaining[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("DEK unwrap failed")
	}

	return plaintext, nil
}

// build chunk nonce = 4-byte random prefix (from base) || 8-byte counter
func makeNonce(prefix []byte, counter uint64) []byte {
	nonce := make([]byte, 12) // AES-GCM standard nonce size
	copy(nonce, prefix)
	binary.LittleEndian.PutUint64(nonce[4:], counter)
	return nonce
}

// ---------------- PUBLIC API ----------------------

// EncryptFIPSKEMDEMStream copies src → dst using FIPS-approved algorithms, returns plaintext bytes written.
func EncryptFIPSKEMDEMStream(pub *ecdsa.PublicKey, src io.Reader, dst io.Writer) (int64, error) {
	if pub.Curve != elliptic.P256() {
		return 0, errors.New("only P-256 keys supported")
	}

	// 1. Generate random DEK for AES-256-GCM
	dek := make([]byte, dekSize)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return 0, err
	}
	defer func() {
		for i := range dek {
			dek[i] = 0
		}
	}()

	// 2. Wrap DEK using ECDH + AES-GCM
	wrapped, err := wrapDEKWithECDH(dek, pub)
	if err != nil {
		return 0, err
	}

	// 3. Generate random base nonce prefix (4 B random + 8 B counter)
	baseNonce := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, baseNonce[:4]); err != nil {
		return 0, err
	}

	// 4. Write header: [uint16 len][wrapped-DEK][base nonce]
	if err := binary.Write(dst, binary.BigEndian, uint16(len(wrapped))); err != nil {
		return 0, err
	}
	if _, err := dst.Write(wrapped); err != nil {
		return 0, err
	}
	if _, err := dst.Write(baseNonce); err != nil {
		return 0, err
	}

	// 5. Initialize AES-256-GCM
	block, err := aes.NewCipher(dek)
	if err != nil {
		return 0, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return 0, err
	}

	// 6. Stream encrypt payload in frames
	buf := make([]byte, frame)
	defer func() {
		for i := range buf {
			buf[i] = 0
		}
	}()
	var counter uint64
	var total int64

	// Prepare header for authentication with first frame
	headerAD := make([]byte, 2+12) // wrappedLen + baseNonce
	binary.BigEndian.PutUint16(headerAD[0:2], uint16(len(wrapped)))
	copy(headerAD[2:], baseNonce)

	for {
		n, readErr := io.ReadFull(src, buf)
		if readErr == io.EOF {
			break
		}
		if readErr != nil && readErr != io.ErrUnexpectedEOF {
			return total, readErr
		}

		nonce := makeNonce(baseNonce, counter)
		var ct []byte
		if counter == 0 {
			// First frame: authenticate header as associated data
			ct = aead.Seal(nil, nonce, buf[:n], headerAD)
		} else {
			// Subsequent frames: no associated data
			ct = aead.Seal(nil, nonce, buf[:n], nil)
		}

		// Defensive assertion: ensure ciphertext length fits in uint16
		if len(ct) > math.MaxUint16 {
			return total, errors.New("ciphertext length exceeds uint16 limit")
		}

		if err := binary.Write(dst, binary.BigEndian, uint16(len(ct))); err != nil {
			return total, err
		}
		if _, err := dst.Write(ct); err != nil {
			return total, err
		}

		counter++
		total += int64(n)
		if readErr == io.ErrUnexpectedEOF {
			break
		}
	}
	return total, nil
}

// DecryptFIPSKEMDEMStream reverses EncryptFIPSKEMDEMStream using FIPS-approved algorithms.
func DecryptFIPSKEMDEMStream(priv *ecdsa.PrivateKey, src io.Reader, dst io.Writer) (int64, error) {
	if priv.Curve != elliptic.P256() {
		return 0, errors.New("only P-256 keys supported")
	}

	// 1. Read header
	var wrappedLen uint16
	if err := binary.Read(src, binary.BigEndian, &wrappedLen); err != nil {
		return 0, err
	}
	if wrappedLen == 0 || wrappedLen > 200 { // reasonable bounds check
		return 0, errors.New("invalid wrapped DEK length")
	}

	wrapped := make([]byte, wrappedLen)
	if _, err := io.ReadFull(src, wrapped); err != nil {
		return 0, err
	}

	baseNonce := make([]byte, 12)
	if _, err := io.ReadFull(src, baseNonce); err != nil {
		return 0, err
	}

	// 2. Unwrap DEK using ECDH + AES-GCM
	dek, err := unwrapDEKWithECDH(wrapped, priv)
	if err != nil {
		return 0, err
	}
	defer func() {
		for i := range dek {
			dek[i] = 0
		}
	}()

	// 3. Initialize AES-256-GCM
	block, err := aes.NewCipher(dek)
	if err != nil {
		return 0, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return 0, err
	}

	// 4. Stream decrypt frames
	var counter uint64
	var total int64

	// Prepare header for authentication with first frame
	headerAD := make([]byte, 2+12) // wrappedLen + baseNonce
	binary.BigEndian.PutUint16(headerAD[0:2], wrappedLen)
	copy(headerAD[2:], baseNonce)

	for {
		var chunkLen uint16
		if err := binary.Read(src, binary.BigEndian, &chunkLen); err != nil {
			if err == io.EOF {
				break
			}
			return total, err
		}
		if int(chunkLen) > frame+gcmTag { // frame + GCM overhead
			return total, errors.New("chunk too large")
		}
		cipher := make([]byte, chunkLen)
		if _, err := io.ReadFull(src, cipher); err != nil {
			return total, err
		}
		nonce := makeNonce(baseNonce, counter)
		var plain []byte
		if counter == 0 {
			// First frame: verify header as associated data
			plain, err = aead.Open(nil, nonce, cipher, headerAD)
		} else {
			// Subsequent frames: no associated data
			plain, err = aead.Open(nil, nonce, cipher, nil)
		}
		// Clear cipher immediately after use
		for i := range cipher {
			cipher[i] = 0
		}
		if err != nil {
			return total, err
		}
		if _, err = dst.Write(plain); err != nil {
			return total, err
		}
		counter++
		total += int64(len(plain))
	}
	return total, nil
}
