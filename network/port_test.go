package network

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectListeningTCPPorts(t *testing.T) {
	ports, err := DetectListeningTCPPorts()
	require.NoError(t, err)
	assert.NotNil(t, ports)
	assert.GreaterOrEqual(t, len(ports), 0)

	if len(ports) > 1 {
		for i := 1; i < len(ports); i++ {
			assert.Greater(t, ports[i], ports[i-1], "ports should be sorted in ascending order")
		}
	}
}

func TestDetectListeningTCPPortsWithExclusion(t *testing.T) {
	allPorts, err := DetectListeningTCPPorts()
	require.NoError(t, err)

	if len(allPorts) == 0 {
		t.Skip("no listening ports detected, skipping exclusion test")
	}

	portToExclude := allPorts[0]
	filteredPorts, err := DetectListeningTCPPorts(portToExclude)
	require.NoError(t, err)

	for _, port := range filteredPorts {
		assert.NotEqual(t, portToExclude, port, "excluded port should not be in result")
	}
	assert.Equal(t, len(allPorts)-1, len(filteredPorts), "should have one less port after exclusion")
}

func TestDetectListeningTCPPortsWithMultipleExclusions(t *testing.T) {
	allPorts, err := DetectListeningTCPPorts()
	require.NoError(t, err)

	if len(allPorts) < 3 {
		t.Skip("not enough listening ports detected, skipping multiple exclusion test")
	}

	excludePorts := []int{allPorts[0], allPorts[1], allPorts[2]}
	filteredPorts, err := DetectListeningTCPPorts(excludePorts...)
	require.NoError(t, err)

	for _, port := range filteredPorts {
		for _, excluded := range excludePorts {
			assert.NotEqual(t, excluded, port, "excluded port should not be in result")
		}
	}
	assert.Equal(t, len(allPorts)-len(excludePorts), len(filteredPorts), "should have three fewer ports after exclusion")
}

func TestDetectListeningTCPPortsWithNonExistentExclusion(t *testing.T) {
	allPorts, err := DetectListeningTCPPorts()
	require.NoError(t, err)

	nonExistentPort := 65534
	filteredPorts, err := DetectListeningTCPPorts(nonExistentPort)
	require.NoError(t, err)

	assert.Equal(t, len(allPorts), len(filteredPorts), "excluding non-existent port should not change result")
}

func TestDetectListeningTCPPortsWithActualListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	boundPort := addr.Port

	ports, err := DetectListeningTCPPorts()
	require.NoError(t, err)

	found := false
	for _, port := range ports {
		if port == boundPort {
			found = true
			break
		}
	}
	assert.True(t, found, "should detect the port we just bound to")
}

func TestDetectListeningTCPPortsWithActualListenerAndExclude(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	boundPort := addr.Port

	ports, err := DetectListeningTCPPorts(boundPort)
	require.NoError(t, err)

	for _, port := range ports {
		assert.NotEqual(t, boundPort, port, "excluded port should not appear even though it's listening")
	}
}

func TestDetectListeningTCPPortsNoDuplicates(t *testing.T) {
	ports, err := DetectListeningTCPPorts()
	require.NoError(t, err)

	seen := make(map[int]bool)
	for _, port := range ports {
		assert.False(t, seen[port], "port %d appears multiple times in result", port)
		seen[port] = true
	}
}

func TestDetectListeningTCPPortsValidRange(t *testing.T) {
	ports, err := DetectListeningTCPPorts()
	require.NoError(t, err)

	for _, port := range ports {
		assert.GreaterOrEqual(t, port, 1, "port should be >= 1")
		assert.LessOrEqual(t, port, 65535, "port should be <= 65535")
	}
}
