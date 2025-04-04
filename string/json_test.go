package string

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJSONStringify(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		pretty   bool
		expected string
	}{
		{
			name:     "simple string",
			input:    "test",
			pretty:   false,
			expected: `"test"`,
		},
		{
			name:     "simple number",
			input:    42,
			pretty:   false,
			expected: `42`,
		},
		{
			name:     "simple boolean",
			input:    true,
			pretty:   false,
			expected: `true`,
		},
		{
			name:     "simple map",
			input:    map[string]string{"key": "value"},
			pretty:   false,
			expected: `{"key":"value"}`,
		},
		{
			name:     "nested map",
			input:    map[string]interface{}{"key": "value", "nested": map[string]int{"num": 123}},
			pretty:   false,
			expected: `{"key":"value","nested":{"num":123}}`,
		},
		{
			name:     "array",
			input:    []int{1, 2, 3},
			pretty:   false,
			expected: `[1,2,3]`,
		},
		{
			name:     "pretty simple map",
			input:    map[string]string{"key": "value"},
			pretty:   true,
			expected: "{\n  \"key\": \"value\"\n}",
		},
		{
			name: "pretty nested map",
			input: map[string]interface{}{
				"key":    "value",
				"nested": map[string]int{"num": 123},
			},
			pretty:   true,
			expected: "{\n  \"key\": \"value\",\n  \"nested\": {\n    \"num\": 123\n  }\n}",
		},
		{
			name:     "nil value",
			input:    nil,
			pretty:   false,
			expected: `null`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result string
			if tt.pretty {
				result = JSONStringify(tt.input, true)
			} else {
				result = JSONStringify(tt.input)
			}
			assert.Equal(t, tt.expected, result)
		})
	}
}

type jsonPanicStruct struct {
	Ch chan int
}

func TestJSONStringifyPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("The code did not panic as expected")
		}
	}()

	JSONStringify(jsonPanicStruct{Ch: make(chan int)})
}
