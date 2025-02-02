package string

import (
	"testing"
)

func TestContains(t *testing.T) {
	tests := []struct {
		haystack        []string
		needle          string
		caseInsensitive bool
		expected        bool
	}{
		{[]string{"A", "B", "C"}, "A", false, true},
		{[]string{"A", "B", "C"}, "D", false, false},
		{[]string{"A", "B", "C"}, "a", true, true},
		{[]string{"A", "B", "C"}, "f", true, false},
	}

	for _, test := range tests {
		result := Contains(test.haystack, test.needle, test.caseInsensitive)
		if result != test.expected {
			t.Errorf("Contains(%v, %v, %v) returned %v, expected %v",
				test.haystack, test.needle, test.caseInsensitive, result, test.expected)
		}
	}
}
