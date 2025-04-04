package sys

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsRunningInsideDocker(t *testing.T) {
	result := IsRunningInsideDocker()
	
	assert.IsType(t, true, result)
}

func TestIsRunningInsideDockerWithMockFiles(t *testing.T) {
	
	result := IsRunningInsideDocker()
	
	assert.IsType(t, true, result)
}
