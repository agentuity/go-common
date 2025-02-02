package string

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateRandomString(t *testing.T) {
	str, err := GenerateRandomString(1)
	assert.NoError(t, err)
	assert.Len(t, str, 1)
	t.Log(str)
	str, err = GenerateRandomString(10)
	assert.NoError(t, err)
	assert.Len(t, str, 10)
	t.Log(str)
}

func TestGenerateRandomBytes(t *testing.T) {
	str, err := GenerateRandomBytes(1)
	assert.NoError(t, err)
	assert.Len(t, str, 1)
	str, err = GenerateRandomBytes(10)
	assert.NoError(t, err)
	assert.Len(t, str, 10)
}
