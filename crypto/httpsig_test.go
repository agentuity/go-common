package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSignAndVerifyHTTPRequest(t *testing.T) {
	body := []byte(`{"ayyy":123}`)
	r, err := http.NewRequest("GET", "https://hayy.hoo/my-sick/path_", nil)
	assert.NoError(t, err)

	testKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	assert.NoError(t, err)

	err = SignHTTPRequest(testKey, r, body)
	assert.NoError(t, err)

	err = VerifyHTTPRequest(&testKey.PublicKey, r, body, func() error {
		return nil
	})
	assert.NoError(t, err)
}
