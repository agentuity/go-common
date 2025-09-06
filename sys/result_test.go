package sys

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResult_IsOk(t *testing.T) {
	tests := []struct {
		name     string
		result   Result[string]
		expected bool
	}{
		{
			name:     "Ok result",
			result:   Result[string]{Ok: "success", Err: nil},
			expected: true,
		},
		{
			name:     "Error result",
			result:   Result[string]{Ok: "", Err: errors.New("error")},
			expected: false,
		},
		{
			name:     "Empty result with nil error",
			result:   Result[string]{Ok: "", Err: nil},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.result.IsOk())
		})
	}
}

func TestResult_IsErr(t *testing.T) {
	tests := []struct {
		name     string
		result   Result[int]
		expected bool
	}{
		{
			name:     "Ok result",
			result:   Result[int]{Ok: 42, Err: nil},
			expected: false,
		},
		{
			name:     "Error result",
			result:   Result[int]{Ok: 0, Err: errors.New("error")},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.result.IsErr())
		})
	}
}

// Helper functions for creating Results
func TestOk(t *testing.T) {
	result := Ok(42)
	assert.True(t, result.IsOk())
	assert.Equal(t, 42, result.Ok)
	assert.Nil(t, result.Err)
}

func TestErr(t *testing.T) {
	err := errors.New("test error")
	result := Err[string](err)
	assert.True(t, result.IsErr())
	assert.Equal(t, "", result.Ok)
	assert.Equal(t, err, result.Err)
}

func TestErrMultiple(t *testing.T) {
	err := errors.New("test error")
	err2 := errors.New("test error2")
	result := Err[string](err)
	assert.True(t, result.IsErr(err2, err))
	assert.Equal(t, "", result.Ok)
	assert.Equal(t, err, result.Err)
	result2 := Err[string](err2)
	assert.True(t, result2.IsErr(err, err2))
	assert.Equal(t, "", result2.Ok)
	assert.Equal(t, err2, result2.Err)
}

func TestErrMatch(t *testing.T) {
	err := errors.New("test error")
	result := Err[string](err)
	assert.True(t, result.IsErrMatches("test"))
	assert.Equal(t, "", result.Ok)
	assert.Equal(t, err, result.Err)
}

func TestErrMatchMultiple(t *testing.T) {
	err := errors.New("test error")
	result := Err[string](err)
	assert.True(t, result.IsErrMatches("foo", "error"))
	assert.Equal(t, "", result.Ok)
	assert.Equal(t, err, result.Err)
}

// Test with different types
func TestResult_WithDifferentTypes(t *testing.T) {
	// Test with struct
	type Person struct {
		Name string
		Age  int
	}

	person := Person{Name: "John", Age: 30}
	result := Ok(person)
	assert.True(t, result.IsOk())
	assert.Equal(t, person, result.Ok)

	// Test with slice
	numbers := []int{1, 2, 3}
	sliceResult := Ok(numbers)
	assert.True(t, sliceResult.IsOk())
	assert.Equal(t, numbers, sliceResult.Ok)

	// Test with pointer
	str := "hello"
	ptrResult := Ok(&str)
	assert.True(t, ptrResult.IsOk())
	assert.Equal(t, &str, ptrResult.Ok)
}
