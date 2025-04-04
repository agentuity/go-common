package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWaitForAnyKeyMessage(t *testing.T) {
	originalHasTTY := HasTTY
	defer func() { HasTTY = originalHasTTY }()

	HasTTY = false
	WaitForAnyKeyMessage("Press a key") // Just ensure no panic
}

func TestAskForConfirm(t *testing.T) {
	originalHasTTY := HasTTY
	defer func() { HasTTY = originalHasTTY }()

	HasTTY = false
	result := AskForConfirm("Confirm?", 'y')
	assert.Equal(t, byte(0), result)
}
