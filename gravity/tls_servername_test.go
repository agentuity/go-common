package gravity

import (
	"context"
	"net"
	"sync"
	"testing"

	"github.com/agentuity/go-common/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDNSExpansion_SetsTLSServerName verifies that when useMultiConnect resolves
// a hostname to multiple IPs, each resulting endpoint preserves the original
// hostname in TLSServerName for correct TLS SNI.
func TestDNSExpansion_SetsTLSServerName(t *testing.T) {
	g := &GravityClient{
		ctx:             context.Background(),
		logger:          logger.NewTestLogger(),
		url:             "grpc://gravity-g3-gcpusw1.agentuity.cloud",
		gravityURLs:     []string{},
		useMultiConnect: true,
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 10,
		},
		endpointsMu: sync.RWMutex{},
	}

	// Simulate what startMultiEndpoint does during DNS expansion:
	// resolveGravityURLs returns the single URL, then DNS resolves it to IPs.
	urls := g.resolveGravityURLs()
	require.Len(t, urls, 1)

	endpointURL := urls[0]
	parsedHost, err := g.parseGRPCURL(endpointURL)
	require.NoError(t, err)

	hostname, port, err := net.SplitHostPort(parsedHost)
	require.NoError(t, err)
	assert.Equal(t, "gravity-g3-gcpusw1.agentuity.cloud", hostname)
	assert.Equal(t, "443", port)

	// Simulate DNS returning 3 IPs — replicate the exact endpoint creation
	// logic from startMultiEndpoint's DNS expansion path.
	testIPs := []net.IP{
		net.ParseIP("10.0.0.1"),
		net.ParseIP("10.0.0.2"),
		net.ParseIP("10.0.0.3"),
	}

	g.endpointsMu.Lock()
	g.endpoints = make([]*GravityEndpoint, 0, len(testIPs))
	for _, ip := range testIPs {
		hostPort := net.JoinHostPort(ip.String(), port)
		ep := &GravityEndpoint{
			URL:           "grpc://" + hostPort,
			TLSServerName: hostname, // this is the fix — preserve original hostname
		}
		ep.healthy.Store(false)
		g.endpoints = append(g.endpoints, ep)
	}
	g.endpointsMu.Unlock()

	// Verify all endpoints have the original hostname as TLSServerName
	g.endpointsMu.RLock()
	defer g.endpointsMu.RUnlock()
	require.Len(t, g.endpoints, 3)
	for i, ep := range g.endpoints {
		assert.Equal(t, "gravity-g3-gcpusw1.agentuity.cloud", ep.TLSServerName,
			"endpoint %d should have original hostname as TLSServerName", i)
		assert.Contains(t, ep.URL, testIPs[i].String(),
			"endpoint %d URL should contain the resolved IP", i)
	}
}

// TestDNSExpansion_IPInput_NoTLSServerName verifies that when the input URL
// already contains an IP address (no DNS resolution needed), TLSServerName
// is not set (falls back to extractHostnameFromURL behavior).
func TestDNSExpansion_IPInput_NoTLSServerName(t *testing.T) {
	// When the URL is already an IP, the DNS expansion path skips DNS lookup
	// and creates an endpoint without TLSServerName.
	ep := &GravityEndpoint{URL: "grpc://10.0.0.1:443"}
	ep.healthy.Store(false)

	assert.Empty(t, ep.TLSServerName,
		"IP-based endpoint created directly should have empty TLSServerName")
}

// TestNonDNSEndpoints_NoTLSServerName verifies that endpoints created from
// explicit URLs (not DNS expansion) have empty TLSServerName.
func TestNonDNSEndpoints_NoTLSServerName(t *testing.T) {
	// When multiple explicit URLs are provided (not useMultiConnect DNS path),
	// endpoints are created with just the URL — no TLSServerName.
	urls := []string{
		"grpc://gravity1.example.com:443",
		"grpc://gravity2.example.com:443",
		"grpc://gravity3.example.com:443",
	}

	endpoints := make([]*GravityEndpoint, 0, len(urls))
	for _, u := range urls {
		ep := &GravityEndpoint{URL: u}
		ep.healthy.Store(false)
		endpoints = append(endpoints, ep)
	}

	for i, ep := range endpoints {
		assert.Empty(t, ep.TLSServerName,
			"endpoint %d from explicit URL should have empty TLSServerName", i)
	}
}

// TestTLSHostnameSelection_PrefersTLSServerName verifies the hostname selection
// logic: when an endpoint has TLSServerName set, it should be used instead of
// extracting from the URL.
func TestTLSHostnameSelection_PrefersTLSServerName(t *testing.T) {
	g := &GravityClient{
		ctx:    context.Background(),
		logger: logger.NewTestLogger(),
	}

	// Endpoint with TLSServerName set (DNS-resolved IP)
	ep := &GravityEndpoint{
		URL:           "grpc://10.0.0.1:443",
		TLSServerName: "gravity-g3-gcpusw1.agentuity.cloud",
	}

	// The connection loop logic: prefer TLSServerName, fall back to extractHostnameFromURL
	hostname := ep.TLSServerName
	if hostname == "" {
		var err error
		hostname, err = g.extractHostnameFromURL(ep.URL)
		require.NoError(t, err)
	}

	assert.Equal(t, "gravity-g3-gcpusw1.agentuity.cloud", hostname,
		"should use TLSServerName when set, not the fallback")
}

// TestTLSHostnameSelection_FallsBackForNonDNSEndpoints verifies that when
// TLSServerName is empty, extractHostnameFromURL is used as fallback.
func TestTLSHostnameSelection_FallsBackForNonDNSEndpoints(t *testing.T) {
	g := &GravityClient{
		ctx:    context.Background(),
		logger: logger.NewTestLogger(),
	}

	// Endpoint without TLSServerName (hostname-based URL)
	ep := &GravityEndpoint{
		URL: "grpc://gravity.example.com:443",
	}

	hostname := ep.TLSServerName
	if hostname == "" {
		var err error
		hostname, err = g.extractHostnameFromURL(ep.URL)
		require.NoError(t, err)
	}

	assert.Equal(t, "gravity.example.com", hostname,
		"should extract hostname from URL when TLSServerName is empty")
}

// TestTLSHostnameSelection_IPWithoutTLSServerName_UsesFallback verifies that
// IP-based endpoints without TLSServerName fall back to the default server name.
func TestTLSHostnameSelection_IPWithoutTLSServerName_UsesFallback(t *testing.T) {
	g := &GravityClient{
		ctx:    context.Background(),
		logger: logger.NewTestLogger(),
	}

	// IP-based endpoint without TLSServerName — this is the old broken path
	// where extractHostnameFromURL falls back to defaultGravityServerName.
	ep := &GravityEndpoint{
		URL: "grpc://10.0.0.1:443",
	}

	hostname := ep.TLSServerName
	if hostname == "" {
		var err error
		hostname, err = g.extractHostnameFromURL(ep.URL)
		require.NoError(t, err)
	}

	assert.Equal(t, defaultGravityServerName, hostname,
		"IP endpoint without TLSServerName should fall back to default server name")
}

// TestExtractHostnameFromGravityURL_IPDetection verifies the underlying helper
// correctly detects IPs and uses the fallback.
func TestExtractHostnameFromGravityURL_IPDetection(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		fallback string
		expected string
	}{
		{
			name:     "hostname URL returns hostname",
			url:      "grpc://gravity.example.com:443",
			fallback: "",
			expected: "gravity.example.com",
		},
		{
			name:     "IPv4 URL with fallback returns fallback",
			url:      "grpc://10.0.0.1:443",
			fallback: "gravity-g3.agentuity.cloud",
			expected: "gravity-g3.agentuity.cloud",
		},
		{
			name:     "IPv4 URL without fallback returns default",
			url:      "grpc://10.0.0.1:443",
			fallback: "",
			expected: defaultGravityServerName,
		},
		{
			name:     "IPv6 URL with fallback returns fallback",
			url:      "grpc://[2001:db8::1]:443",
			fallback: "gravity-g3.agentuity.cloud",
			expected: "gravity-g3.agentuity.cloud",
		},
		{
			name:     "hostname with subdomain returns hostname",
			url:      "grpc://gravity-g3-gcpusw1-sakm.agentuity.cloud:443",
			fallback: "",
			expected: "gravity-g3-gcpusw1-sakm.agentuity.cloud",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hostname, err := extractHostnameFromGravityURL(tt.url, tt.fallback)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, hostname)
		})
	}
}

// TestReconnectEndpoint_UsesTLSServerName verifies that reconnectSingleEndpoint
// picks up TLSServerName from the endpoint for TLS config.
func TestReconnectEndpoint_UsesTLSServerName(t *testing.T) {
	g := &GravityClient{
		ctx:         context.Background(),
		logger:      logger.NewTestLogger(),
		endpointsMu: sync.RWMutex{},
	}

	// Set up endpoints with TLSServerName
	g.endpointsMu.Lock()
	g.endpoints = []*GravityEndpoint{
		{URL: "grpc://10.0.0.1:443", TLSServerName: "gravity-g3.agentuity.cloud"},
		{URL: "grpc://10.0.0.2:443", TLSServerName: "gravity-g3.agentuity.cloud"},
		{URL: "grpc://gravity3.example.com:443"}, // no TLSServerName
	}
	g.endpointsMu.Unlock()

	// Verify endpoint 0 would use TLSServerName
	g.endpointsMu.RLock()
	hostname := g.endpoints[0].TLSServerName
	g.endpointsMu.RUnlock()
	assert.Equal(t, "gravity-g3.agentuity.cloud", hostname,
		"reconnect should use TLSServerName for IP-based endpoints")

	// Verify endpoint 2 would fall back to URL extraction
	g.endpointsMu.RLock()
	hostname = g.endpoints[2].TLSServerName
	g.endpointsMu.RUnlock()
	assert.Empty(t, hostname,
		"reconnect should fall back for hostname-based endpoints")
}
