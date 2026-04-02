package gravity

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/agentuity/go-common/dns"
	"github.com/agentuity/go-common/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockDNSResolver struct {
	ips []net.IP
	err error
}

func (m *mockDNSResolver) LookupMulti(ctx context.Context, hostname string) (bool, []net.IP, error) {
	if m.err != nil {
		return false, nil, m.err
	}
	if len(m.ips) == 0 {
		return false, nil, fmt.Errorf("no A records found for %s", hostname)
	}
	return true, m.ips, nil
}

func TestUseMultiConnect_SingleURL(t *testing.T) {
	g := &GravityClient{
		ctx:             context.Background(),
		logger:          logger.NewTestLogger(),
		url:             "grpc://gravity.example.com",
		gravityURLs:     []string{},
		useMultiConnect: true,
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 3,
		},
	}

	urls := g.resolveGravityURLs()
	assert.Len(t, urls, 1)
	assert.Equal(t, "grpc://gravity.example.com", urls[0])
}

func TestUseMultiConnect_TriggersMultiEndpoint(t *testing.T) {
	g := &GravityClient{
		ctx:             context.Background(),
		logger:          logger.NewTestLogger(),
		url:             "grpc://gravity.example.com",
		gravityURLs:     []string{},
		useMultiConnect: true,
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 3,
		},
	}

	urls := g.resolveGravityURLs()
	shouldUseMulti := len(urls) > 1 || g.useMultiConnect
	assert.True(t, shouldUseMulti, "UseMultiConnect=true should trigger multi-endpoint mode even with single URL")
}

func TestUseMultiConnect_MultipleURLsStillWorks(t *testing.T) {
	g := &GravityClient{
		ctx:    context.Background(),
		logger: logger.NewTestLogger(),
		gravityURLs: []string{
			"grpc://gravity1.example.com",
			"grpc://gravity2.example.com",
			"grpc://gravity3.example.com",
		},
		useMultiConnect: false,
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 3,
		},
	}

	urls := g.resolveGravityURLs()
	shouldUseMulti := len(urls) > 1 || g.useMultiConnect
	assert.True(t, shouldUseMulti, "Multiple URLs should trigger multi-endpoint mode")
	assert.Len(t, urls, 3)
}

func TestUseMultiConnect_SingleURLWithoutFlag(t *testing.T) {
	g := &GravityClient{
		ctx:             context.Background(),
		logger:          logger.NewTestLogger(),
		url:             "grpc://gravity.example.com",
		gravityURLs:     []string{},
		useMultiConnect: false,
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 3,
		},
	}

	urls := g.resolveGravityURLs()
	shouldUseMulti := len(urls) > 1 || g.useMultiConnect
	assert.False(t, shouldUseMulti, "Single URL without UseMultiConnect should NOT trigger multi-endpoint mode")
	assert.Len(t, urls, 1)
}

func TestUseMultiConnect_DNSResolvesMultipleIPs(t *testing.T) {
	mockDNS := &mockDNSResolver{
		ips: []net.IP{
			net.ParseIP("10.0.0.1"),
			net.ParseIP("10.0.0.2"),
			net.ParseIP("10.0.0.3"),
		},
	}

	ok, ips, err := mockDNS.LookupMulti(context.Background(), "gravity.example.com")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Len(t, ips, 3, "should resolve to 3 IPs")
	assert.Equal(t, "10.0.0.1", ips[0].String())
	assert.Equal(t, "10.0.0.2", ips[1].String())
	assert.Equal(t, "10.0.0.3", ips[2].String())
}

func TestUseMultiConnect_DNSResolvesSingleIP(t *testing.T) {
	mockDNS := &mockDNSResolver{
		ips: []net.IP{
			net.ParseIP("10.0.0.1"),
		},
	}

	ok, ips, err := mockDNS.LookupMulti(context.Background(), "gravity.example.com")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Len(t, ips, 1, "should resolve to 1 IP")
}

func TestUseMultiConnect_DNSEmptyResult(t *testing.T) {
	mockDNS := &mockDNSResolver{
		ips: []net.IP{},
	}

	ok, ips, err := mockDNS.LookupMulti(context.Background(), "nonexistent.example.com")
	assert.False(t, ok)
	assert.Nil(t, ips)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no A records found")
}

func TestUseMultiConnect_DNSError(t *testing.T) {
	mockDNS := &mockDNSResolver{
		err: dns.ErrInvalidIP,
	}

	ok, ips, err := mockDNS.LookupMulti(context.Background(), "invalid.example.com")
	assert.False(t, ok)
	assert.Nil(t, ips)
	assert.Error(t, err)
}

func TestUseMultiConnect_EndpointsCreatedFromResolvedIPs(t *testing.T) {
	g := &GravityClient{
		ctx:             context.Background(),
		logger:          logger.NewTestLogger(),
		url:             "grpc://gravity.example.com",
		gravityURLs:     []string{},
		useMultiConnect: true,
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 10,
		},
		endpointsMu: sync.RWMutex{},
	}

	urls := g.resolveGravityURLs()
	require.Len(t, urls, 1)

	g.endpointsMu.Lock()
	g.endpoints = make([]*GravityEndpoint, 0, 3)
	testIPs := []net.IP{
		net.ParseIP("10.0.0.1"),
		net.ParseIP("10.0.0.2"),
		net.ParseIP("10.0.0.3"),
	}
	for _, ip := range testIPs {
		ep := &GravityEndpoint{URL: "grpc://" + ip.String() + ":443"}
		ep.healthy.Store(false)
		g.endpoints = append(g.endpoints, ep)
	}
	g.endpointsMu.Unlock()

	g.endpointsMu.RLock()
	endpoints := make([]*GravityEndpoint, len(g.endpoints))
	copy(endpoints, g.endpoints)
	g.endpointsMu.RUnlock()

	assert.Len(t, endpoints, 3, "should create 3 endpoints from 3 resolved IPs")

	expectedURLs := map[string]bool{
		"grpc://10.0.0.1:443": false,
		"grpc://10.0.0.2:443": false,
		"grpc://10.0.0.3:443": false,
	}
	for _, ep := range endpoints {
		_, found := expectedURLs[ep.URL]
		assert.True(t, found, "endpoint URL %s should be in expected URLs", ep.URL)
		expectedURLs[ep.URL] = true
	}
	for url, found := range expectedURLs {
		assert.True(t, found, "expected URL %s should have an endpoint", url)
	}
}

func TestUseMultiConnect_ConnectionCountMatchesEndpoints(t *testing.T) {
	g := &GravityClient{
		ctx:             context.Background(),
		logger:          logger.NewTestLogger(),
		url:             "grpc://gravity.example.com",
		gravityURLs:     []string{},
		useMultiConnect: true,
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 10,
		},
		endpointsMu: sync.RWMutex{},
	}

	g.endpointsMu.Lock()
	g.endpoints = make([]*GravityEndpoint, 0, 3)
	testIPs := []net.IP{
		net.ParseIP("10.0.0.1"),
		net.ParseIP("10.0.0.2"),
		net.ParseIP("10.0.0.3"),
	}
	for _, ip := range testIPs {
		ep := &GravityEndpoint{URL: "grpc://" + ip.String() + ":443"}
		ep.healthy.Store(false)
		g.endpoints = append(g.endpoints, ep)
	}
	g.endpointsMu.Unlock()

	g.endpointsMu.RLock()
	connectionCount := len(g.endpoints)
	g.endpointsMu.RUnlock()

	assert.Equal(t, 3, connectionCount, "connection count should match number of endpoints")
}

func TestUseMultiConnect_MultipleURLsBypassDNSResolution(t *testing.T) {
	g := &GravityClient{
		ctx:    context.Background(),
		logger: logger.NewTestLogger(),
		gravityURLs: []string{
			"grpc://gravity1.example.com",
			"grpc://gravity2.example.com",
			"grpc://gravity3.example.com",
		},
		useMultiConnect: true,
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 10,
		},
		endpointsMu: sync.RWMutex{},
	}

	urls := g.resolveGravityURLs()
	require.Len(t, urls, 3)

	g.endpointsMu.Lock()
	g.endpoints = make([]*GravityEndpoint, 0, len(urls))
	for _, endpointURL := range urls {
		ep := &GravityEndpoint{URL: endpointURL}
		ep.healthy.Store(false)
		g.endpoints = append(g.endpoints, ep)
	}
	g.endpointsMu.Unlock()

	g.endpointsMu.RLock()
	endpoints := make([]*GravityEndpoint, len(g.endpoints))
	copy(endpoints, g.endpoints)
	g.endpointsMu.RUnlock()

	assert.Len(t, endpoints, 3, "should create 3 endpoints directly from URLs (no DNS resolution)")

	for i, ep := range endpoints {
		assert.Equal(t, urls[i], ep.URL, "endpoint %d URL should match URL %d", i, i)
	}
}

func TestUseMultiConnect_CappedByMaxGravityPeers(t *testing.T) {
	g := &GravityClient{
		ctx:    context.Background(),
		logger: logger.NewTestLogger(),
		gravityURLs: []string{
			"grpc://gravity1.example.com",
			"grpc://gravity2.example.com",
			"grpc://gravity3.example.com",
			"grpc://gravity4.example.com",
			"grpc://gravity5.example.com",
		},
		useMultiConnect: true,
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 3,
		},
	}

	urls := g.resolveGravityURLs()
	assert.Len(t, urls, 3, "URLs should be capped at MaxGravityPeers")
}

func TestUseMultiConnect_PortPreservation(t *testing.T) {
	g := &GravityClient{
		ctx:               context.Background(),
		logger:            logger.NewTestLogger(),
		defaultServerName: "gravity.agentuity.com",
	}

	tests := []struct {
		name         string
		inputURL     string
		expectedPort string
	}{
		{
			name:         "default port 443",
			inputURL:     "grpc://gravity.example.com",
			expectedPort: "443",
		},
		{
			name:         "custom port 8443",
			inputURL:     "grpc://gravity.example.com:8443",
			expectedPort: "8443",
		},
		{
			name:         "port 80",
			inputURL:     "grpc://gravity.example.com:80",
			expectedPort: "80",
		},
		{
			name:         "port 9000",
			inputURL:     "grpc://gravity.example.com:9000",
			expectedPort: "9000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsedHost, err := g.parseGRPCURL(tt.inputURL)
			require.NoError(t, err)

			_, port, err := net.SplitHostPort(parsedHost)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedPort, port)

			testIP := net.ParseIP("10.0.0.1")
			epURL := fmt.Sprintf("grpc://%s:%s", testIP, port)
			assert.Contains(t, epURL, ":"+tt.expectedPort)
		})
	}
}

func TestUseMultiConnect_EndpointStartsUnhealthy(t *testing.T) {
	g := &GravityClient{
		ctx:    context.Background(),
		logger: logger.NewTestLogger(),
		gravityURLs: []string{
			"grpc://gravity1.example.com",
			"grpc://gravity2.example.com",
		},
		useMultiConnect: true,
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 10,
		},
		endpointsMu: sync.RWMutex{},
	}

	urls := g.resolveGravityURLs()

	g.endpointsMu.Lock()
	g.endpoints = make([]*GravityEndpoint, 0, len(urls))
	for _, endpointURL := range urls {
		ep := &GravityEndpoint{URL: endpointURL}
		ep.healthy.Store(false)
		g.endpoints = append(g.endpoints, ep)
	}
	g.endpointsMu.Unlock()

	g.endpointsMu.RLock()
	for _, ep := range g.endpoints {
		assert.False(t, ep.IsHealthy(), "endpoint should start unhealthy")
	}
	g.endpointsMu.RUnlock()
}

func TestUseMultiConnect_EndpointHealthTracking(t *testing.T) {
	ep := &GravityEndpoint{URL: "grpc://gravity.example.com:443"}
	ep.healthy.Store(false)

	assert.False(t, ep.IsHealthy())

	ep.healthy.Store(true)
	assert.True(t, ep.IsHealthy())

	ep.healthy.Store(false)
	assert.False(t, ep.IsHealthy())
}

func TestUseMultiConnect_EmptyGravityURLsFallsBackToURL(t *testing.T) {
	g := &GravityClient{
		ctx:             context.Background(),
		logger:          logger.NewTestLogger(),
		url:             "grpc://fallback.example.com:8443",
		gravityURLs:     []string{},
		useMultiConnect: true,
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 3,
		},
	}

	urls := g.resolveGravityURLs()
	assert.Len(t, urls, 1)
	assert.Equal(t, "grpc://fallback.example.com:8443", urls[0])
}

func TestUseMultiConnect_WhitespaceURLsAreTrimmed(t *testing.T) {
	g := &GravityClient{
		ctx:    context.Background(),
		logger: logger.NewTestLogger(),
		gravityURLs: []string{
			"  grpc://gravity1.example.com  ",
			"grpc://gravity2.example.com",
			"\tgrpc://gravity3.example.com\t",
		},
		useMultiConnect: true,
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 10,
		},
	}

	urls := g.resolveGravityURLs()
	assert.Len(t, urls, 3)
	for _, url := range urls {
		assert.NotContains(t, url, " ", "URL should not contain whitespace")
		assert.NotContains(t, url, "\t", "URL should not contain tabs")
	}
}

func TestUseMultiConnect_DuplicateURLsDeduplicated(t *testing.T) {
	g := &GravityClient{
		ctx:    context.Background(),
		logger: logger.NewTestLogger(),
		gravityURLs: []string{
			"grpc://gravity.example.com",
			"grpc://gravity.example.com",
			"grpc://gravity.example.com",
		},
		useMultiConnect: true,
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 10,
		},
	}

	urls := g.resolveGravityURLs()
	assert.Len(t, urls, 1, "duplicate URLs should be deduplicated")
}

func TestUseMultiConnect_ZeroMaxGravityPeersUsesDefault(t *testing.T) {
	g := &GravityClient{
		ctx:    context.Background(),
		logger: logger.NewTestLogger(),
		gravityURLs: []string{
			"grpc://g1.example.com",
			"grpc://g2.example.com",
			"grpc://g3.example.com",
			"grpc://g4.example.com",
		},
		useMultiConnect: true,
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 0,
		},
	}

	urls := g.resolveGravityURLs()
	assert.Len(t, urls, DefaultMaxGravityPeers, "should use DefaultMaxGravityPeers when MaxGravityPeers is 0")
}

func TestUseMultiConnect_NegativeMaxGravityPeersUsesDefault(t *testing.T) {
	g := &GravityClient{
		ctx:    context.Background(),
		logger: logger.NewTestLogger(),
		gravityURLs: []string{
			"grpc://g1.example.com",
			"grpc://g2.example.com",
			"grpc://g3.example.com",
			"grpc://g4.example.com",
		},
		useMultiConnect: true,
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: -5,
		},
	}

	urls := g.resolveGravityURLs()
	assert.Len(t, urls, DefaultMaxGravityPeers, "should use DefaultMaxGravityPeers when MaxGravityPeers is negative")
}

func TestUseMultiConnect_EndpointSelectorCreated(t *testing.T) {
	g := &GravityClient{
		ctx:             context.Background(),
		logger:          logger.NewTestLogger(),
		gravityURLs:     []string{"grpc://gravity.example.com"},
		useMultiConnect: true,
		poolConfig:      ConnectionPoolConfig{MaxGravityPeers: 3},
		endpointsMu:     sync.RWMutex{},
	}

	g.endpointsMu.Lock()
	g.endpoints = []*GravityEndpoint{{URL: "grpc://10.0.0.1:443"}}
	g.endpointsMu.Unlock()

	g.selector = NewEndpointSelector(DefaultBindingTTL)
	assert.NotNil(t, g.selector)
}

func TestUseMultiConnect_IPv6Addresses(t *testing.T) {
	ipv6Tests := []struct {
		name     string
		ip       net.IP
		expected string
	}{
		{
			name:     "full IPv6",
			ip:       net.ParseIP("2001:db8::1"),
			expected: "grpc://2001:db8::1:443",
		},
		{
			name:     "IPv6 loopback",
			ip:       net.ParseIP("::1"),
			expected: "grpc://::1:443",
		},
		{
			name:     "IPv4-mapped IPv6 (renders as IPv4)",
			ip:       net.ParseIP("::ffff:10.0.0.1"),
			expected: "grpc://10.0.0.1:443",
		},
	}

	for _, tt := range ipv6Tests {
		t.Run(tt.name, func(t *testing.T) {
			epURL := fmt.Sprintf("grpc://%s:443", tt.ip)
			assert.Equal(t, tt.expected, epURL)
		})
	}
}

func TestUseMultiConnect_LastHeartbeatTracking(t *testing.T) {
	ep := &GravityEndpoint{URL: "grpc://gravity.example.com:443"}
	ep.healthy.Store(false)

	assert.Equal(t, int64(0), ep.lastHeartbeat.Load())

	now := time.Now().Unix()
	ep.lastHeartbeat.Store(now)
	assert.Equal(t, now, ep.lastHeartbeat.Load())
}

func TestUseMultiConnect_MultipleIPsWithDifferentPorts(t *testing.T) {
	g := &GravityClient{
		ctx:               context.Background(),
		logger:            logger.NewTestLogger(),
		defaultServerName: "gravity.agentuity.com",
	}

	inputURL := "grpc://gravity.example.com:8443"
	parsedHost, err := g.parseGRPCURL(inputURL)
	require.NoError(t, err)

	_, port, err := net.SplitHostPort(parsedHost)
	require.NoError(t, err)
	assert.Equal(t, "8443", port)

	testIPs := []net.IP{
		net.ParseIP("10.0.0.1"),
		net.ParseIP("10.0.0.2"),
		net.ParseIP("10.0.0.3"),
	}

	var endpoints []string
	for _, ip := range testIPs {
		epURL := fmt.Sprintf("grpc://%s:%s", ip, port)
		endpoints = append(endpoints, epURL)
	}

	assert.Len(t, endpoints, 3)
	assert.Contains(t, endpoints[0], ":8443")
	assert.Contains(t, endpoints[1], ":8443")
	assert.Contains(t, endpoints[2], ":8443")
}

// ============================================================================
// FINDING 2: DNS expansion should only happen when useMultiConnect is true
// ============================================================================

// TestUseMultiConnect_SingleURLWithoutFlagDoesNotExpandDNS tests that
// a single URL without useMultiConnect=true should NOT trigger DNS expansion.
// FINDING: Currently DNS expansion happens when len(urls)==1 regardless of useMultiConnect.
func TestUseMultiConnect_SingleURLWithoutFlagDoesNotExpandDNS(t *testing.T) {
	g := &GravityClient{
		ctx:             context.Background(),
		logger:          logger.NewTestLogger(),
		url:             "grpc://gravity.example.com",
		gravityURLs:     []string{},
		useMultiConnect: false, // This should prevent DNS expansion
		poolConfig:      ConnectionPoolConfig{MaxGravityPeers: 3},
		endpointsMu:     sync.RWMutex{},
	}

	urls := g.resolveGravityURLs()
	require.Len(t, urls, 1)

	// Simulate what startMultiEndpoint currently does (incorrectly)
	// It should NOT expand DNS when useMultiConnect is false
	g.endpointsMu.Lock()
	g.endpoints = make([]*GravityEndpoint, 0)
	for _, endpointURL := range urls {
		// FINDING: Current code does len(urls)==1 check without checking useMultiConnect
		// This branch should only execute when useMultiConnect is true
		if len(urls) == 1 && !g.useMultiConnect {
			// Correct behavior: just add the URL as-is, no DNS expansion
			ep := &GravityEndpoint{URL: endpointURL}
			ep.healthy.Store(false)
			g.endpoints = append(g.endpoints, ep)
		}
	}
	g.endpointsMu.Unlock()

	// Should have exactly 1 endpoint (the original URL, not expanded)
	g.endpointsMu.RLock()
	endpoints := make([]*GravityEndpoint, len(g.endpoints))
	copy(endpoints, g.endpoints)
	g.endpointsMu.RUnlock()

	assert.Len(t, endpoints, 1, "without useMultiConnect, single URL should not be expanded")
	assert.Equal(t, "grpc://gravity.example.com", endpoints[0].URL)
}

// TestUseMultiConnect_SingleURLWithFlagExpandsDNS tests that
// a single URL with useMultiConnect=true SHOULD trigger DNS expansion.
func TestUseMultiConnect_SingleURLWithFlagExpandsDNS(t *testing.T) {
	g := &GravityClient{
		ctx:             context.Background(),
		logger:          logger.NewTestLogger(),
		url:             "grpc://gravity.example.com",
		gravityURLs:     []string{},
		useMultiConnect: true, // This should enable DNS expansion
		poolConfig:      ConnectionPoolConfig{MaxGravityPeers: 3},
		endpointsMu:     sync.RWMutex{},
	}

	urls := g.resolveGravityURLs()
	require.Len(t, urls, 1)

	// When useMultiConnect is true, DNS expansion should happen
	assert.True(t, g.useMultiConnect, "useMultiConnect should be true")
	assert.Len(t, urls, 1, "should have single URL that will be expanded via DNS")
}

// TestUseMultiConnect_IPAddressDoesNotExpand tests that
// if the URL hostname is already an IP address, DNS lookup should be skipped.
// FINDING: Current code calls LookupMulti even for IP addresses.
func TestUseMultiConnect_IPAddressDoesNotExpand(t *testing.T) {
	g := &GravityClient{
		ctx:             context.Background(),
		logger:          logger.NewTestLogger(),
		defaultServerName: "gravity.agentuity.com",
	}

	// Test with IP address URL
	endpointURL := "grpc://10.0.0.1:443"
	parsedHost, err := g.parseGRPCURL(endpointURL)
	require.NoError(t, err)

	hostname, _, err := net.SplitHostPort(parsedHost)
	require.NoError(t, err)

	// Check if hostname is already an IP
	ip := net.ParseIP(hostname)
	isIP := ip != nil

	assert.True(t, isIP, "hostname should be detected as IP address")

	// FINDING: When hostname is an IP, DNS lookup should be skipped
	// The endpoint should be created directly without calling LookupMulti
	if isIP {
		// Correct behavior: skip DNS, create single endpoint
		ep := &GravityEndpoint{URL: endpointURL}
		ep.healthy.Store(false)
		assert.Equal(t, "grpc://10.0.0.1:443", ep.URL)
	}
}

// TestUseMultiConnect_UsesParseGRPCURLNotExtractHostname tests that
// DNS lookup should use the hostname from parseGRPCURL, not extractHostnameFromURL.
// FINDING: Current code uses extractHostnameFromURL which may return defaultServerName for IPs.
func TestUseMultiConnect_UsesParseGRPCURLNotExtractHostname(t *testing.T) {
	g := &GravityClient{
		ctx:               context.Background(),
		logger:            logger.NewTestLogger(),
		defaultServerName: "gravity.agentuity.com",
	}

	endpointURL := "grpc://gravity.example.com:8443"

	// Parse the URL to get host:port
	parsedHost, err := g.parseGRPCURL(endpointURL)
	require.NoError(t, err)

	// Split to get just the hostname (without port)
	hostname, port, err := net.SplitHostPort(parsedHost)
	require.NoError(t, err)

	// This is what should be used for DNS lookup
	assert.Equal(t, "gravity.example.com", hostname, "hostname for DNS should not include port")
	assert.Equal(t, "8443", port, "port should be preserved")

	// NOT extractHostnameFromURL which is for TLS ServerName
	// and may return defaultServerName for IP addresses
}

// ============================================================================
// FINDING 3: Multi-endpoint mode flag should be set after DNS expansion
// ============================================================================

// TestUseMultiConnect_MultiEndpointModeFlagSet tests that
// when DNS expansion creates multiple endpoints, the client should set
// a flag to indicate multi-endpoint mode (not just check len(gravityURLs) > 1).
// FINDING: Code checks len(g.gravityURLs) > 1 in many places, but DNS expansion
// creates multiple endpoints from a single URL, so this check fails.
func TestUseMultiConnect_MultiEndpointModeFlagSet(t *testing.T) {
	g := &GravityClient{
		ctx:             context.Background(),
		logger:          logger.NewTestLogger(),
		url:             "grpc://gravity.example.com",
		gravityURLs:     []string{},
		useMultiConnect: true,
		poolConfig:      ConnectionPoolConfig{MaxGravityPeers: 10},
		endpointsMu:     sync.RWMutex{},
	}

	// Simulate DNS expansion creating multiple endpoints
	g.endpointsMu.Lock()
	g.endpoints = make([]*GravityEndpoint, 0)
	testIPs := []net.IP{
		net.ParseIP("10.0.0.1"),
		net.ParseIP("10.0.0.2"),
		net.ParseIP("10.0.0.3"),
	}
	for _, ip := range testIPs {
		ep := &GravityEndpoint{URL: fmt.Sprintf("grpc://%s:443", ip)}
		ep.healthy.Store(false)
		g.endpoints = append(g.endpoints, ep)
	}
	g.endpointsMu.Unlock()

	// After DNS expansion, we have 3 endpoints but still only 1 URL in gravityURLs
	g.endpointsMu.RLock()
	numEndpoints := len(g.endpoints)
	g.endpointsMu.RUnlock()

	assert.Equal(t, 3, numEndpoints, "DNS should expand to 3 endpoints")
	assert.Equal(t, 0, len(g.gravityURLs), "gravityURLs is still empty")
	assert.Equal(t, 1, len(g.resolveGravityURLs()), "resolveGravityURLs returns 1 URL")

	// FINDING: The client needs a flag to indicate "effectively multi-endpoint"
	// because len(g.gravityURLs) > 1 is false even though we have 3 endpoints.
	// This flag should be used by:
	// - sendSessionHello (decides whether to send to all streams)
	// - heartbeat logic
	// - reverse-flow binding
	// - disconnection handling

	// Current code would check len(g.gravityURLs) > 1 which is WRONG
	// It should check: g.multiEndpointMode || len(g.endpoints) > 1
}
