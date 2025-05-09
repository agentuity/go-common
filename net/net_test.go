package net

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDialerWithDefaultDNS(t *testing.T) {
	dialer := New()
	assert.NotNil(t, dialer)
	assert.Equal(t, DefaultDNSServers, dialer.dnsServers)
}

func TestDefaultDialer(t *testing.T) {
	assert.NotNil(t, DefaultDialer)
	assert.Equal(t, DefaultDNSServers, DefaultDialer.dnsServers)
}

func TestDialerWithCustomDNS(t *testing.T) {
	customDNS := []string{"8.8.8.8:53", "1.1.1.1:53"}
	dialer := New(WithDNS(customDNS...))
	assert.NotNil(t, dialer)
	assert.Equal(t, customDNS, dialer.dnsServers)
}

func TestDialerRoundRobin(t *testing.T) {
	customDNS := []string{"8.8.8.8:53", "1.1.1.1:53"}
	dialer := New(WithDNS(customDNS...))

	var dialedAddresses []string

	for i := 0; i < 5; i++ {
		idx := dialer.counter.Add(1) % uint64(len(dialer.dnsServers))
		dnsServer := dialer.dnsServers[idx]
		dialedAddresses = append(dialedAddresses, dnsServer)
	}

	assert.Contains(t, dialedAddresses, "8.8.8.8:53")
	assert.Contains(t, dialedAddresses, "1.1.1.1:53")
}
