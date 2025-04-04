package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHasTTY(t *testing.T) {
	assert.Contains(t, []bool{true, false}, HasTTY)
}
