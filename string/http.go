package string

import (
	"net/http"
	"strings"
)

// MaskHeaders will return a stringified version of headers
// masking the headers passed in by name
func MaskHeaders(h http.Header, maskHeaders []string) map[string]string {
	hh := make(map[string]string, len(h))
	for k, v := range h {
		if Contains(maskHeaders, k, true) {
			hh[k] = Mask(strings.Join(v, ", "))
			continue
		}
		hh[k] = strings.Join(v, ", ")
	}
	return hh
}
