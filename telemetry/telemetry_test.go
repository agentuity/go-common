package telemetry

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGenerateOTLPBearerTokenWithExpiration(t *testing.T) {
	sharedSecret := "test-shared-secret"
	tokenVal := "test-token"
	expiration := time.Now().Add(time.Hour)
	token, err := GenerateOTLPBearerTokenWithExpiration(sharedSecret, tokenVal, expiration)
	assert.NoError(t, err)
	assert.Len(t, strings.Split(token, "."), 3)
	assert.True(t, strings.HasPrefix(token, "1h."))
}

func TestGenerateOTLPBearerTokenWithExpirationLong(t *testing.T) {
	sharedSecret := "test-shared-secret"
	tokenVal := "test-token"
	expiration := time.Now().Add(time.Hour * 24 * 30)
	token, err := GenerateOTLPBearerTokenWithExpiration(sharedSecret, tokenVal, expiration)
	assert.NoError(t, err)
	t.Logf("token: %s", token)
	assert.Len(t, strings.Split(token, "."), 3)
	assert.True(t, strings.HasPrefix(token, "4w"))
}

func TestGenerateOTLPBearerTokenWithNoExpiration(t *testing.T) {
	sharedSecret := "test-shared-secret"
	tokenVal := "test-token"
	token, err := GenerateOTLPBearerToken(sharedSecret, tokenVal)
	assert.NoError(t, err)
	t.Logf("token: %s", token)
	assert.Len(t, strings.Split(token, "."), 2)
}
