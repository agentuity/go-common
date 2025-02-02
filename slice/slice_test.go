package slice

import (
	"testing"
)

func TestContains(t *testing.T) {
	slice := []string{"a", "b", "c"}
	if !Contains(slice, "a") {
		t.Errorf("expected true, got false")
	}
	if Contains(slice, "d") {
		t.Errorf("expected false, got true")
	}
}

func TestContainsCaseInsensitive(t *testing.T) {
	slice := []string{"a", "b", "c"}
	if !Contains(slice, "a", WithCaseInsensitive()) {
		t.Errorf("expected true, got false")
	}
	if !Contains(slice, "A", WithCaseInsensitive()) {
		t.Errorf("expected true, got false")
	}
	if Contains(slice, "d") {
		t.Errorf("expected false, got true")
	}
}

func TestContainsAny(t *testing.T) {
	slice := []string{"a", "b", "c"}
	if !ContainsAny(slice, "a", "d") {
		t.Errorf("expected true, got false")
	}
	if ContainsAny(slice, "d", "e") {
		t.Errorf("expected false, got true")
	}
}

func TestOmit(t *testing.T) {
	slice := []string{"a", "b", "c"}
	result := Omit(slice, "a", "c")
	if len(result) != 1 || result[0] != "b" {
		t.Errorf(`expected ["b"], got %v`, result)
	}
}
