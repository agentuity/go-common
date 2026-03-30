package crypto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeriveKeyDeterministic(t *testing.T) {
	secret := []byte("my-master-secret")
	key1, err := DeriveKey(secret, ContextBearerToken)
	require.NoError(t, err)

	key2, err := DeriveKey(secret, ContextBearerToken)
	require.NoError(t, err)

	assert.Equal(t, key1, key2, "DeriveKey should produce consistent output for same inputs")
}

func TestDeriveKeyDomainSeparation(t *testing.T) {
	secret := []byte("my-master-secret")

	key1, err := DeriveKey(secret, ContextBearerToken)
	require.NoError(t, err)

	key2, err := DeriveKey(secret, ContextStickySession)
	require.NoError(t, err)

	assert.NotEqual(t, key1, key2, "DeriveKey should produce different output for different contexts")
}

func TestDeriveKeyDifferentSecrets(t *testing.T) {
	key1, err := DeriveKey([]byte("secret-one"), ContextBearerToken)
	require.NoError(t, err)

	key2, err := DeriveKey([]byte("secret-two"), ContextBearerToken)
	require.NoError(t, err)

	assert.NotEqual(t, key1, key2, "DeriveKey should produce different output for different master secrets")
}

func TestDeriveKeyEmptyMasterSecret(t *testing.T) {
	key, err := DeriveKey([]byte{}, ContextBearerToken)
	assert.Error(t, err)
	assert.Nil(t, key)
	assert.Contains(t, err.Error(), "master secret cannot be empty")
}

func TestDeriveKeyNilMasterSecret(t *testing.T) {
	key, err := DeriveKey(nil, ContextBearerToken)
	assert.Error(t, err)
	assert.Nil(t, key)
	assert.Contains(t, err.Error(), "master secret cannot be empty")
}

func TestDeriveKeyEmptyContext(t *testing.T) {
	key, err := DeriveKey([]byte("secret"), "")
	assert.Error(t, err)
	assert.Nil(t, key)
	assert.Contains(t, err.Error(), "context cannot be empty")
}

func TestDeriveKeyOutputLength(t *testing.T) {
	key, err := DeriveKey([]byte("my-master-secret"), ContextBearerToken)
	require.NoError(t, err)
	assert.Len(t, key, 32, "DeriveKey output should be exactly 32 bytes")
}

func TestDetectTokenVersionV2(t *testing.T) {
	version, context, payload := DetectTokenVersion("v2.bearer-token.somePayload")
	assert.Equal(t, "v2", version)
	assert.Equal(t, "bearer-token", context)
	assert.Equal(t, "somePayload", payload)
}

func TestDetectTokenVersionV1(t *testing.T) {
	version, context, payload := DetectTokenVersion("oldStyleToken.hash")
	assert.Equal(t, "v1", version)
	assert.Equal(t, "", context)
	assert.Equal(t, "oldStyleToken.hash", payload)
}

func TestDetectTokenVersionMalformedV2(t *testing.T) {
	// "v2.incomplete" has the v2 prefix but only 2 parts, not 3
	version, context, payload := DetectTokenVersion("v2.incomplete")
	assert.Equal(t, "v1", version)
	assert.Equal(t, "", context)
	assert.Equal(t, "v2.incomplete", payload)
}

func TestFormatV2Token(t *testing.T) {
	result := FormatV2Token("bearer-token", "payload123")
	assert.Equal(t, "v2.bearer-token.payload123", result)
}

func TestDeriveKeyAllContexts(t *testing.T) {
	secret := []byte("test-secret")
	contexts := []string{
		ContextBearerToken,
		ContextStickySession,
		ContextPostgresInternal,
		ContextGravityJWT,
		ContextS3Webhook,
	}

	keys := make(map[string][]byte)
	for _, ctx := range contexts {
		key, err := DeriveKey(secret, ctx)
		require.NoError(t, err)
		assert.Len(t, key, 32)
		keys[ctx] = key
	}

	// Verify all keys are unique
	for i, ctx1 := range contexts {
		for _, ctx2 := range contexts[i+1:] {
			assert.NotEqual(t, keys[ctx1], keys[ctx2],
				"keys for %q and %q should be different", ctx1, ctx2)
		}
	}
}
