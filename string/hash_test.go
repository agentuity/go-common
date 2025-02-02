package string

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewHash(t *testing.T) {
	assert.Equal(t, "b7b41276360564d4", NewHash("1"))
	assert.Equal(t, "d7c9b97948142e4a", NewHash(true))
	assert.Equal(t, "ea8842e9ea2638fa", NewHash("hi"))
	assert.Equal(t, "2e1472b57af294d1", NewHash(map[string]any{}))
	assert.Equal(t, "02817afd559a4122", NewHash(1, true, "hi", map[string]any{}))
	assert.Equal(t, "3ec9e10063179f3a", NewHash(nil))
	assert.Equal(t, "985b4bad3b2d15ee", NewHash("order_number", "6520692e92cb3f002353975c"))

	assert.Equal(t, "54789c6c18ea9933", NewHash("order_number", "627c06f38433f70025baba37"))
	assert.Equal(t, "cf1ed9ce4383e878", NewHash("order_number", "627c08068433f70025baba84"))
	assert.Equal(t, "4e0cb433f75e37a4", NewHash("order_number", "637e6de64cf72300244423d3"))
	assert.Equal(t, "398e48e3cea51ab3", NewHash("order_number", "63f79ea5b233d000252414a5"))
	assert.Equal(t, "680eea2ab6c7f3b1", NewHash("order_number", "6435790607b87d002407783e"))
	assert.Equal(t, "0ee9fc3c7d732f71", NewHash("order_number", "64399fcc07b87d0024079ff9"))
	assert.Equal(t, "8daa95f03762b484", NewHash("order_number", "64b82027ee46e20024b0050b"))
	assert.Equal(t, "e725148afb79ec95", NewHash("order_number", "64b84f58ee46e20024b00b54"))
	assert.Equal(t, "582060db3bdc366a", NewHash("order_number", "63ec0cefb233d0002523b2b9"))
}

func TestNewHash64(t *testing.T) {
	assert.Equal(t, uint64(0xb7b41276360564d4), NewHash64("1"))
	assert.Equal(t, uint64(0xd7c9b97948142e4a), NewHash64(true))
	assert.Equal(t, uint64(0xea8842e9ea2638fa), NewHash64("hi"))
	assert.Equal(t, uint64(0x2e1472b57af294d1), NewHash64(map[string]any{}))
	assert.Equal(t, uint64(0x2817afd559a4122), NewHash64(1, true, "hi", map[string]any{}))
	assert.Equal(t, uint64(0x3ec9e10063179f3a), NewHash64(nil))
	assert.Equal(t, uint64(0x985b4bad3b2d15ee), NewHash64("order_number", "6520692e92cb3f002353975c"))
	assert.Equal(t, uint64(0x54789c6c18ea9933), NewHash64("order_number", "627c06f38433f70025baba37"))
	assert.Equal(t, uint64(0xcf1ed9ce4383e878), NewHash64("order_number", "627c08068433f70025baba84"))
}

func TestFNV1Hash(t *testing.T) {
	assert.Equal(t, uint32(0x42f53a8d), FNV1Hash("order_number"))
}

func TestModulo(t *testing.T) {
	assert.Equal(t, 7, Modulo("order_number", 10))
	assert.Equal(t, 5, Modulo("order_number", 11))
	assert.Equal(t, 0, Modulo("order_number", 1))
}
