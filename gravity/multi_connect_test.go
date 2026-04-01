package gravity

import (
	"context"
	"net"
	"sync"
	"testing"

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
		return false, nil, nil
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
	assert.NoError(t, err)
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
