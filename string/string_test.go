package string

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStringPointer(t *testing.T) {
	assert.Equal(t, "hi", *StringPointer("hi"))
	assert.Equal(t, "hi", *StringPointer(" hi"))
	assert.Equal(t, "hi", *StringPointer("hi "))
	assert.Equal(t, "hi", *StringPointer(" hi "))
	assert.Equal(t, "hi", *StringPointer("hi"))
	assert.Equal(t, "hi", *StringPointer("hi"))
	assert.Equal(t, "hi", *StringPointer("hi"))
}

func TestPtr(t *testing.T) {
	// Test with string
	str := "hello"
	strPtr := Ptr(str)
	assert.Equal(t, &str, strPtr)
	assert.Equal(t, str, *strPtr)

	// Test with int
	num := 42
	numPtr := Ptr(num)
	assert.Equal(t, &num, numPtr)
	assert.Equal(t, num, *numPtr)

	// Test with bool
	flag := true
	flagPtr := Ptr(flag)
	assert.Equal(t, &flag, flagPtr)
	assert.Equal(t, flag, *flagPtr)

	// Test with struct
	type testStruct struct {
		Name string
		Age  int
	}
	s := testStruct{Name: "test", Age: 25}
	sPtr := Ptr(s)
	assert.Equal(t, &s, sPtr)
	assert.Equal(t, s, *sPtr)

	// Test with slice
	slice := []int{1, 2, 3}
	slicePtr := Ptr(slice)
	assert.Equal(t, &slice, slicePtr)
	assert.Equal(t, slice, *slicePtr)

	// Test with nil interface
	var nilInterface interface{}
	nilPtr := Ptr(nilInterface)
	assert.Equal(t, &nilInterface, nilPtr)
	assert.Equal(t, nilInterface, *nilPtr)
}

func TestClearEmptyStringPointer(t *testing.T) {
	assert.Equal(t, "hi", *StringPointer("hi"))
	assert.Equal(t, "hi", *StringPointer(" hi"))
	assert.Equal(t, "hi", *StringPointer("hi "))
	assert.Equal(t, "hi", *StringPointer(" hi "))
	assert.Equal(t, "hi", *StringPointer("hi"))
	assert.Equal(t, "hi", *StringPointer("hi"))
	assert.Equal(t, "hi", *StringPointer("hi"))
}
