package sys

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLocalIP(t *testing.T) {
	ip, err := LocalIP()

	if err == nil {
		assert.NotEmpty(t, ip)

		parsedIP := net.ParseIP(ip)
		assert.NotNil(t, parsedIP)
		assert.NotNil(t, parsedIP.To4())

		assert.False(t, parsedIP.IsLoopback())
	} else {
		assert.Empty(t, ip)
	}
}
