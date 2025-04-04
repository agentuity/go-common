package slice

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContains(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		val      string
		expected bool
	}{
		{
			name:     "value exists in slice",
			slice:    []string{"a", "b", "c"},
			val:      "a",
			expected: true,
		},
		{
			name:     "value does not exist in slice",
			slice:    []string{"a", "b", "c"},
			val:      "d",
			expected: false,
		},
		{
			name:     "empty slice",
			slice:    []string{},
			val:      "a",
			expected: false,
		},
		{
			name:     "nil slice",
			slice:    nil,
			val:      "a",
			expected: false,
		},
		{
			name:     "empty string value",
			slice:    []string{"a", "b", "c", ""},
			val:      "",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Contains(tt.slice, tt.val)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestContainsCaseInsensitive(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		val      string
		expected bool
	}{
		{
			name:     "exact match",
			slice:    []string{"a", "b", "c"},
			val:      "a",
			expected: true,
		},
		{
			name:     "case insensitive match - uppercase search",
			slice:    []string{"a", "b", "c"},
			val:      "A",
			expected: true,
		},
		{
			name:     "case insensitive match - lowercase search",
			slice:    []string{"A", "B", "C"},
			val:      "a",
			expected: true,
		},
		{
			name:     "case insensitive match - mixed case",
			slice:    []string{"Apple", "Banana", "Cherry"},
			val:      "aPpLe",
			expected: true,
		},
		{
			name:     "no match",
			slice:    []string{"a", "b", "c"},
			val:      "d",
			expected: false,
		},
		{
			name:     "empty slice",
			slice:    []string{},
			val:      "a",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Contains(tt.slice, tt.val, WithCaseInsensitive())
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestContainsAny(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		vals     []string
		expected bool
	}{
		{
			name:     "one value exists in slice",
			slice:    []string{"a", "b", "c"},
			vals:     []string{"a", "d"},
			expected: true,
		},
		{
			name:     "multiple values exist in slice",
			slice:    []string{"a", "b", "c"},
			vals:     []string{"a", "b"},
			expected: true,
		},
		{
			name:     "no values exist in slice",
			slice:    []string{"a", "b", "c"},
			vals:     []string{"d", "e"},
			expected: false,
		},
		{
			name:     "empty slice",
			slice:    []string{},
			vals:     []string{"a", "b"},
			expected: false,
		},
		{
			name:     "nil slice",
			slice:    nil,
			vals:     []string{"a", "b"},
			expected: false,
		},
		{
			name:     "empty values",
			slice:    []string{"a", "b", "c"},
			vals:     []string{},
			expected: false,
		},
		{
			name:     "nil values",
			slice:    []string{"a", "b", "c"},
			vals:     nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ContainsAny(tt.slice, tt.vals...)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOmit(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		vals     []string
		expected []string
	}{
		{
			name:     "omit single value",
			slice:    []string{"a", "b", "c"},
			vals:     []string{"a"},
			expected: []string{"b", "c"},
		},
		{
			name:     "omit multiple values",
			slice:    []string{"a", "b", "c"},
			vals:     []string{"a", "c"},
			expected: []string{"b"},
		},
		{
			name:     "omit all values",
			slice:    []string{"a", "b", "c"},
			vals:     []string{"a", "b", "c"},
			expected: nil,
		},
		{
			name:     "omit no values",
			slice:    []string{"a", "b", "c"},
			vals:     []string{"d", "e"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "empty slice",
			slice:    []string{},
			vals:     []string{"a", "b"},
			expected: nil,
		},
		{
			name:     "nil slice",
			slice:    nil,
			vals:     []string{"a", "b"},
			expected: nil,
		},
		{
			name:     "empty values",
			slice:    []string{"a", "b", "c"},
			vals:     []string{},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "nil values",
			slice:    []string{"a", "b", "c"},
			vals:     nil,
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "duplicate values in slice",
			slice:    []string{"a", "b", "a", "c"},
			vals:     []string{"a"},
			expected: []string{"b", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Omit(tt.slice, tt.vals...)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWithCaseInsensitive(t *testing.T) {
	var opts withOpts
	optFunc := WithCaseInsensitive()
	optFunc(&opts)
	assert.True(t, opts.caseInsensitive)
}
