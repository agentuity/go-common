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

	token, err := NewBearerToken(sharedSecret, WithExpiration(time.Now().Add(61*time.Second)))
	assert.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.Contains(t, token, ".")

	time.Sleep(62 * time.Second)

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

func TestWithExpirationMinimumTruncation(t *testing.T) {
	sharedSecret := "test-secret"
	expiration := time.Now().Add(30 * time.Second)

	token, err := NewBearerToken(sharedSecret, WithExpiration(expiration))
	assert.NoError(t, err)
	assert.NotEmpty(t, token)

	err = ValidateToken(sharedSecret, token)
	assert.NoError(t, err)
}

func TestWithExpirationMaximumValidation(t *testing.T) {
	sharedSecret := "test-secret"
	expiration := time.Now().Add(366 * 24 * time.Hour)

	token, err := NewBearerToken(sharedSecret, WithExpiration(expiration))
	assert.Error(t, err)
	assert.Empty(t, token)
	assert.Contains(t, err.Error(), "expiration time exceeds maximum of 1 year")
}

func TestWithExpirationOneYearBoundary(t *testing.T) {
	sharedSecret := "test-secret"
	expiration := time.Now().Add(365 * 24 * time.Hour)

	token, err := NewBearerToken(sharedSecret, WithExpiration(expiration))
	assert.NoError(t, err)
	assert.NotEmpty(t, token)
}

func TestWithExpirationValidRanges(t *testing.T) {
	sharedSecret := "test-secret"

	testCases := []struct {
		name     string
		duration time.Duration
	}{
		{"1 minute", 1 * time.Minute},
		{"1 hour", 1 * time.Hour},
		{"1 day", 24 * time.Hour},
		{"1 month", 30 * 24 * time.Hour},
		{"6 months", 180 * 24 * time.Hour},
		{"almost 1 year", 364 * 24 * time.Hour},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			expiration := time.Now().Add(tc.duration)
			token, err := NewBearerToken(sharedSecret, WithExpiration(expiration))
			assert.NoError(t, err)
			assert.NotEmpty(t, token)

			err = ValidateToken(sharedSecret, token)
			assert.NoError(t, err)
		})
	}
}
