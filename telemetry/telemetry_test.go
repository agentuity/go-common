package telemetry

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGenerateOTLPBearerTokenWithExpiration(t *testing.T) {
	sharedSecret := "test-shared-secret"
	expiration := time.Now().Add(time.Hour)
	token, err := GenerateOTLPBearerTokenWithExpiration(sharedSecret, expiration)
	assert.NoError(t, err)
	t.Logf("token: %s", token)
	assert.Len(t, strings.Split(token, "."), 3)
	assert.True(t, strings.HasPrefix(token, "1h."+strconv.FormatInt(time.Now().Unix(), 10)+"."))
}

func TestGenerateOTLPBearerTokenWithExpirationLong(t *testing.T) {
	sharedSecret := "test-shared-secret"
	expiration := time.Now().Add(time.Hour * 24 * 30)
	token, err := GenerateOTLPBearerTokenWithExpiration(sharedSecret, expiration)
	assert.NoError(t, err)
	t.Logf("token: %s", token)
	assert.Len(t, strings.Split(token, "."), 3)
	assert.True(t, strings.HasPrefix(token, "4w"))
	assert.Contains(t, token, strconv.FormatInt(time.Now().Unix(), 10))
}

func TestGenerateOTLPBearerTokenWithNoExpiration(t *testing.T) {
	sharedSecret := "test-shared-secret"
	tokenVal := "test-token"
	token, err := GenerateOTLPBearerToken(sharedSecret, tokenVal)
	assert.NoError(t, err)
	t.Logf("token: %s", token)
	assert.Len(t, strings.Split(token, "."), 2)
}
