package crypto

import (
	"crypto/sha256"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/hkdf"
)

const (
	// KeyDerivationVersion is the current version of key derivation.
	KeyDerivationVersion = "v2"

	// keyDerivationSalt includes the version to ensure different versions produce different keys.
	keyDerivationSalt = "agentuity-key-derivation-" + KeyDerivationVersion

	// Context constants for different key derivation purposes.
	// Context strings MUST NOT contain '.' characters, as this would break
	// DetectTokenVersion's parsing which uses SplitN(token, ".", 3) to separate
	// the version prefix, context, and payload.
	ContextBearerToken      = "bearer-token"
	ContextStickySession    = "sticky-session"
	ContextPostgresInternal = "postgres-internal"
	ContextGravityJWT       = "gravity-jwt"
	ContextS3Webhook        = "s3-webhook"
)

// DeriveKey derives a purpose-specific 32-byte key from a master secret using HKDF-SHA256.
// The context parameter provides domain separation so the same master secret produces
// different keys for different purposes.
func DeriveKey(masterSecret []byte, context string) ([]byte, error) {
	if len(masterSecret) == 0 {
		return nil, fmt.Errorf("master secret cannot be empty")
	}
	if context == "" {
		return nil, fmt.Errorf("context cannot be empty")
	}
	reader := hkdf.New(sha256.New, masterSecret, []byte(keyDerivationSalt), []byte(context))
	key := make([]byte, 32)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, fmt.Errorf("failed to derive key for context %q: %w", context, err)
	}
	return key, nil
}

// DetectTokenVersion inspects a token string and returns its version.
// v2 tokens have the format "v2.<context>.<payload>".
// All other tokens are assumed to be v1 (legacy format).
func DetectTokenVersion(token string) (version string, context string, payload string) {
	if strings.HasPrefix(token, "v2.") {
		parts := strings.SplitN(token, ".", 3)
		if len(parts) == 3 {
			return parts[0], parts[1], parts[2]
		}
	}
	return "v1", "", token
}

// FormatV2Token creates a v2 prefixed token string: "v2.<context>.<payload>"
func FormatV2Token(context string, payload string) string {
	return fmt.Sprintf("v2.%s.%s", context, payload)
}
