package string

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCrockfordHash(t *testing.T) {
	assert.Equal(t, "nbtcc7ewrq", CrockfordHash("hello", 10))
	assert.Equal(t, "nbt", CrockfordHash("hello", 3))
	assert.Equal(t, "nbtcc7ewrqma", CrockfordHash("hello", 12))

	assert.Equal(t, "fggh", CrockfordHash("world", 4))
	assert.Equal(t, "fggh8c", CrockfordHash("world", 6))

	assert.Equal(t, "n558zs", CrockfordHash("test", 6))
	assert.Equal(t, "n558zsec", CrockfordHash("test", 8))
}

func TestCrockfordHashCaseInsensitive(t *testing.T) {
	hash1 := CrockfordHash("Hello", 10)
	hash2 := CrockfordHash("HELLO", 10)
	hash3 := CrockfordHash("hello", 10)

	assert.Equal(t, hash1, hash2)
	assert.Equal(t, hash2, hash3)
}

func TestCrockfordHashDeterministic(t *testing.T) {
	input := "deterministic-test"
	hash1 := CrockfordHash(input, 8)
	hash2 := CrockfordHash(input, 8)

	assert.Equal(t, hash1, hash2)
}

func TestCrockfordHashEmptyString(t *testing.T) {
	hash := CrockfordHash("", 5)
	assert.Len(t, hash, 5)
	assert.Equal(t, "v8wt7", hash)
}

func TestCrockfordHashDifferentInputs(t *testing.T) {
	hash1 := CrockfordHash("input1", 10)
	hash2 := CrockfordHash("input2", 10)

	assert.NotEqual(t, hash1, hash2)
}

func TestCrockfordHashUsesValidAlphabet(t *testing.T) {
	hash := CrockfordHash("test-alphabet", 20)
	validChars := "0123456789abcdefghjkmnpqrstvwxyz"

	for _, char := range hash {
		assert.Contains(t, validChars, string(char))
	}
}
