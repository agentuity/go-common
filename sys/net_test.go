package sys

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetFreePort(t *testing.T) {
	port, err := GetFreePort()
	assert.NoError(t, err)
	assert.Greater(t, port, 0)
	
	addr := net.JoinHostPort("localhost", string(port))
	listener, err := net.Listen("tcp", addr)
	if err == nil {
		listener.Close()
	}
}

func TestIsLocalhost(t *testing.T) {
	testCases := []struct {
		url      string
		expected bool
	}{
		{"http://localhost:8080", true},
		{"https://localhost", true},
		{"http://127.0.0.1:3000", true},
		{"https://127.0.0.1", true},
		{"http://0.0.0.0:8000", true},
		{"https://0.0.0.0", true},
		{"http://example.com", false},
		{"https://192.168.1.1", false},
		{"", false},
	}
	
	for _, tc := range testCases {
		t.Run(tc.url, func(t *testing.T) {
			result := IsLocalhost(tc.url)
			assert.Equal(t, tc.expected, result)
		})
	}
}
