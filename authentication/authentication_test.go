package authentication

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewBearerTokenNoExpiry(t *testing.T) {
	sharedSecret := "test-secret"

	token, err := NewBearerToken(sharedSecret)
	assert.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.Contains(t, token, ".")

}

func TestGenerateBearerTokenWithExpirationInPast(t *testing.T) {
	sharedSecret := "test-secret"
	expiration := time.Now().Add(-time.Hour) // 1 hour in the past

	token, err := NewBearerToken(sharedSecret, WithExpiration(expiration))
	assert.Error(t, err)
	assert.Empty(t, token)
	assert.Contains(t, err.Error(), "expiration time is in the past")
}

func TestGenerateBearerTokenWithExpiration(t *testing.T) {
	sharedSecret := "test-secret"
	expiration := time.Now().Add(1 * time.Hour) // 1 hour in the future

	token, err := NewBearerToken(sharedSecret, WithExpiration(expiration))
	assert.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.Contains(t, token, ".")
}

func TestGenerateBearerToken(t *testing.T) {
	sharedSecret := "test-secret"
	token := "test-token"

	token, err := NewBearerToken(sharedSecret, WithNonce(token))
	assert.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.Contains(t, token, ".")
}

func TestValidateToken(t *testing.T) {
	sharedSecret := "test-secret"
	token := "test-token"

	token, err := NewBearerToken(sharedSecret, WithNonce(token))
	assert.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.Contains(t, token, ".")

	err = ValidateToken(sharedSecret, token)
	assert.NoError(t, err)

	err = ValidateToken(sharedSecret, "invalid-token")
	assert.Error(t, err)
}

func TestValidateTokenExpired(t *testing.T) {
	sharedSecret := "test-secret"
	token := "test-token"

	token, err := NewBearerToken(sharedSecret, WithExpiration(time.Now().Add(1*time.Second)))
	assert.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.Contains(t, token, ".")

	time.Sleep(2 * time.Second)

	err = ValidateToken(sharedSecret, token)
	assert.ErrorIs(t, err, ErrTokenExpired)
}

func TestValidateTokenNotExpired(t *testing.T) {
	sharedSecret := "test-secret"
	token := "test-token"

	token, err := NewBearerToken(sharedSecret, WithExpiration(time.Now().Add(2*time.Hour)))
	assert.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.Contains(t, token, ".")
	parts := strings.Split(token, ".")
	assert.Len(t, parts, 3)

	err = ValidateToken(sharedSecret, token)
	assert.NoError(t, err)
}
