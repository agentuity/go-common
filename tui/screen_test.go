package tui

import (
	"testing"
)

func TestClearScreen(t *testing.T) {
	originalHasTTY := HasTTY
	defer func() { HasTTY = originalHasTTY }()

	HasTTY = false
	ClearScreen() // Just ensure no panic

}
