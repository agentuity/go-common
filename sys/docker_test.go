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
	originalExists := Exists
	defer func() { Exists = originalExists }()
	
	Exists = func(path string) bool {
		if path == "/.dockerenv" {
			return true
		}
		return false
	}
	assert.True(t, IsRunningInsideDocker())
	
	Exists = func(path string) bool {
		if path == "/.dockerenv" {
			return false
		}
		if path == "/proc/1/cgroup" {
			return true
		}
		return false
	}
	
	tmpFile, err := os.CreateTemp("", "cgroup")
	if err == nil {
		defer os.Remove(tmpFile.Name())
		
		content := "12:memory:/docker/123456789"
		os.WriteFile(tmpFile.Name(), []byte(content), 0644)
		
		originalReadFile := os.ReadFile
		defer func() { os.ReadFile = originalReadFile }()
		
		os.ReadFile = func(name string) ([]byte, error) {
			if name == "/proc/1/cgroup" {
				return os.ReadFile(tmpFile.Name())
			}
			return originalReadFile(name)
		}
		
		assert.True(t, IsRunningInsideDocker())
	}
	
	Exists = func(path string) bool {
		return false
	}
	assert.False(t, IsRunningInsideDocker())
}
