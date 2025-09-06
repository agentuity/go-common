package sys

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Ptr returns a pointer to the given value.
func Ptr[T any](v T) *T {
	return &v
}

func TestPtr(t *testing.T) {
	// Test with string
	str := "hello"
	strPtr := Ptr(str)
	assert.NotNil(t, strPtr)
	assert.Equal(t, str, *strPtr)

	// Test with int
	num := 42
	numPtr := Ptr(num)
	assert.NotNil(t, numPtr)
	assert.Equal(t, num, *numPtr)

	// Test with bool
	flag := true
	flagPtr := Ptr(flag)
	assert.NotNil(t, flagPtr)
	assert.Equal(t, flag, *flagPtr)

	// Test with struct
	type testStruct struct {
		Name string
		Age  int
	}
	s := testStruct{Name: "test", Age: 25}
	sPtr := Ptr(s)
	assert.NotNil(t, sPtr)
	assert.Equal(t, s, *sPtr)

	// Test with slice
	slice := []int{1, 2, 3}
	slicePtr := Ptr(slice)
	assert.NotNil(t, slicePtr)
	assert.Equal(t, slice, *slicePtr)

	// Test with nil interface
	var nilInterface interface{}
	nilPtr := Ptr(nilInterface)
	assert.NotNil(t, nilPtr)
	assert.Equal(t, nilInterface, *nilPtr)
}
