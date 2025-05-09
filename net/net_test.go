package net

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDialerWithDefaultDNS(t *testing.T) {
	dialer, err := New()
	assert.NotNil(t, dialer)
	assert.NoError(t, err)
}

func TestDefaultDialer(t *testing.T) {
	assert.NotNil(t, DefaultDialer)
	assert.Len(t, DefaultDialer.dnsServers, 3)
}

func TestDialerWithCustomDNS(t *testing.T) {
	customDNS := []string{"8.8.8.8:53", "1.1.1.1:53"}
	dialer, err := New(WithDNS(customDNS...))
	assert.NotNil(t, dialer)
	assert.NoError(t, err)
	sort.Strings(dialer.dnsServers)
	sort.Strings(customDNS)
	assert.Equal(t, customDNS, dialer.dnsServers)
}

func TestDialerRoundRobin(t *testing.T) {
	customDNS := []string{"8.8.8.8:53", "1.1.1.1:53"}
	dialer, err := New(WithDNS(customDNS...))
	assert.NotNil(t, dialer)
	assert.NoError(t, err)

	var dialedAddresses []string

	for i := 0; i < 5; i++ {
		idx := dialer.counter.Add(1) % uint64(len(dialer.dnsServers))
		dnsServer := dialer.dnsServers[idx]
		dialedAddresses = append(dialedAddresses, dnsServer)
	}

	assert.Contains(t, dialedAddresses, "8.8.8.8:53")
	assert.Contains(t, dialedAddresses, "1.1.1.1:53")
}

func TestDefaultClient(t *testing.T) {
	client := DefaultClient
	assert.NotNil(t, client)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "Agentuity (https://agentuity.com)" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("User-Agent header must be set to Agentuity (https://agentuity.com)"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello, World!"))
	}))
	defer server.Close()

	resp, err := client.Get(server.URL)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
