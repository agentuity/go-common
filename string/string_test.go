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

func TestClearEmptyStringPointer(t *testing.T) {
	assert.Equal(t, "hi", *StringPointer("hi"))
	assert.Equal(t, "hi", *StringPointer(" hi"))
	assert.Equal(t, "hi", *StringPointer("hi "))
	assert.Equal(t, "hi", *StringPointer(" hi "))
	assert.Equal(t, "hi", *StringPointer("hi"))
	assert.Equal(t, "hi", *StringPointer("hi"))
	assert.Equal(t, "hi", *StringPointer("hi"))
}
