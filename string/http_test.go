package string

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMaskHeaders(t *testing.T) {
	testCases := []struct {
		name        string
		headers     http.Header
		maskHeaders []string
		expected    map[string]string
	}{
		{
			name:        "mask one header",
			headers:     http.Header{"a": []string{"b"}},
			maskHeaders: []string{"a"},
			expected:    map[string]string{"a": "*"},
		},
		{
			name:        "do not mask any headers",
			headers:     http.Header{"a": []string{"b"}},
			maskHeaders: []string{"c"},
			expected:    map[string]string{"a": "b"},
		},
		{
			name:        "mask multiple headers",
			headers:     http.Header{"a": []string{"b"}, "b": []string{"c"}},
			maskHeaders: []string{"a", "b"},
			expected:    map[string]string{"a": "*", "b": "*"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := MaskHeaders(tc.headers, tc.maskHeaders)
			assert.Equal(t, tc.expected, result)
			fmt.Println(tc.name)
		})
	}
}
