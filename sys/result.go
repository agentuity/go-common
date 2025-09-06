package sys

import (
	"errors"
	"strings"
)

// Result represents a value that can be either successful (Ok) or an error (Err),
// similar to Rust's Result type.
type Result[T any] struct {
	Ok  T
	Err error
}

// IsOk returns true if the Result contains a successful value (no error).
func (r Result[T]) IsOk() bool {
	return r.Err == nil
}

// IsErr returns true if the Result contains an error.
func (r Result[T]) IsErr(checks ...error) bool {
	if len(checks) == 0 {
		return r.Err != nil
	}
	for _, err := range checks {
		if errors.Is(r.Err, err) {
			return true
		}
	}
	return false
}

// IsErrMatches returns true if the Result contains an error containing any of the given string values.
func (r Result[T]) IsErrMatches(checks ...string) bool {
	if len(checks) == 0 {
		return r.Err != nil
	}
	val := r.Err.Error()
	for _, err := range checks {
		if strings.Contains(val, err) {
			return true
		}
	}
	return false
}

// Ok creates a new Result with a successful value.
func Ok[T any](value T) Result[T] {
	return Result[T]{Ok: value, Err: nil}
}

// Err creates a new Result with an error.
func Err[T any](err error) Result[T] {
	var zero T
	return Result[T]{Ok: zero, Err: err}
}
