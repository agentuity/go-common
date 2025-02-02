package slice

import "strings"

type withOpts struct {
	caseInsensitive bool
}

type withOptsFunc func(opts *withOpts)

// WithCaseInsensitive will make the contains functions case insensitive.
func WithCaseInsensitive() withOptsFunc {
	return func(opts *withOpts) {
		opts.caseInsensitive = true
	}
}

// Contains returns true if the slice contains the value.
func Contains(slice []string, val string, opts ...withOptsFunc) bool {
	var withOpts withOpts
	for _, opt := range opts {
		opt(&withOpts)
	}
	for _, s := range slice {
		if withOpts.caseInsensitive {
			if strings.EqualFold(s, val) {
				return true
			}
		} else {
			if s == val {
				return true
			}
		}
	}
	return false
}

// ContainsAny returns true if the slice contain any of the values [val].
func ContainsAny(slice []string, val ...string) bool {
	for _, s := range slice {
		if Contains(val, s) {
			return true
		}
	}
	return false
}

// Omit returns a new slice with the values [val] omitted.
func Omit(slice []string, val ...string) []string {
	var result []string
	for _, s := range slice {
		if !Contains(val, s) {
			result = append(result, s)
		}
	}
	return result
}
