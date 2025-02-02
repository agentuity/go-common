package string

import "github.com/agentuity/go-common/slice"

// Contains returns true if the search string is found in the slice
func Contains(needle []string, haystack string, caseInsensitive bool) bool {
	if caseInsensitive {
		return slice.Contains(needle, haystack, slice.WithCaseInsensitive())
	}
	return slice.Contains(needle, haystack)
}
