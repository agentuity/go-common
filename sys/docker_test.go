package sys

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsRunningInsideDocker(t *testing.T) {
	result := IsRunningInsideDocker()

	assert.IsType(t, true, result)
}
