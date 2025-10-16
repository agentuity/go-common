package string

import (
	"crypto/sha1"
	"strings"
)

var crockfordAlphabet = []rune("0123456789abcdefghjkmnpqrstvwxyz")

// CrockfordHash computes a short Crockford hash of the given value.
func CrockfordHash(value string, length int) string {
	if length <= 0 {
		panic("invalid length")
	}
	// Compute SHA1
	h := sha1.Sum([]byte(strings.ToLower(value)))

	// Convert first bytes to base32 Crockford
	var out []rune
	bits := 0
	acc := 0
	for _, b := range h[:] {
		acc = (acc << 8) | int(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out = append(out, crockfordAlphabet[(acc>>bits)&31])
			if len(out) >= length {
				return string(out)
			}
		}
	}
	if bits > 0 && len(out) < length {
		out = append(out, crockfordAlphabet[(acc<<uint(5-bits))&31])
	}
	return string(out)
}
