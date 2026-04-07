package gravity

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/agentuity/go-common/logger"
)

// mockDNSLookup returns a dnsLookupMulti function backed by a mutable IP map.
type mockDNSLookup struct {
	mu      sync.Mutex
	results map[string][]net.IP
	calls   atomic.Int32
}

func newMockDNSLookup() *mockDNSLookup {
	return &mockDNSLookup{results: make(map[string][]net.IP)}
}

func (m *mockDNSLookup) setIPs(hostname string, ips ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	parsed := make([]net.IP, len(ips))
	for i, ip := range ips {
		parsed[i] = net.ParseIP(ip)
	}
	m.results[hostname] = parsed
}

func (m *mockDNSLookup) lookupMulti(_ context.Context, hostname string) (bool, []net.IP, error) {
	m.calls.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	ips, ok := m.results[hostname]
	if !ok || len(ips) == 0 {
		return false, nil, nil
	}
	cp := make([]net.IP, len(ips))
	copy(cp, ips)
	return true, cp, nil
}

func (m *mockDNSLookup) callCount() int {
	return int(m.calls.Load())
}

// newReResolveTestClient creates a minimal GravityClient for re-resolution tests.
func newReResolveTestClient(endpoints []*GravityEndpoint, mock *mockDNSLookup) *GravityClient {
	ctx, cancel := context.WithCancel(context.Background())
	g := &GravityClient{
		ctx:    ctx,
		cancel: cancel,
		logger: logger.NewTestLogger(),
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 3,
		},
		dnsLookupMulti: mock.lookupMulti,
		healthProbe:    func(host, port string) bool { return true }, // skip real HTTP probes in tests
	}
	g.endpoints = endpoints
	// In multi-endpoint mode, overlapping IPs are forbidden
	if len(endpoints) > 1 {
		g.multiEndpointMode.Store(true)
	}
	return g
}

// --- reResolveEndpointURL unit tests ---

func TestReResolveEndpointURL_ReturnsNewIPWhenChanged(t *testing.T) {
	mock := newMockDNSLookup()
	mock.setIPs("gravity.example.com", "10.0.0.2")

	ep := &GravityEndpoint{
		URL:           "grpc://10.0.0.1:443",
		TLSServerName: "gravity.example.com",
	}
	g := newReResolveTestClient([]*GravityEndpoint{ep}, mock)
	defer g.cancel()

	newURL := g.reResolveEndpointURL(0, "grpc://10.0.0.1:443")

	if newURL != "grpc://10.0.0.2:443" {
		t.Fatalf("expected grpc://10.0.0.2:443, got %q", newURL)
	}
	// Verify endpoint URL updated in-place
	if ep.URL != "grpc://10.0.0.2:443" {
		t.Fatalf("expected endpoint URL updated, got %q", ep.URL)
	}
	if mock.callCount() != 1 {
		t.Fatalf("expected 1 DNS call, got %d", mock.callCount())
	}
}

func TestReResolveEndpointURL_ReturnsEmptyWhenIPUnchanged(t *testing.T) {
	mock := newMockDNSLookup()
	mock.setIPs("gravity.example.com", "10.0.0.1") // same IP

	ep := &GravityEndpoint{
		URL:           "grpc://10.0.0.1:443",
		TLSServerName: "gravity.example.com",
	}
	g := newReResolveTestClient([]*GravityEndpoint{ep}, mock)
	defer g.cancel()

	newURL := g.reResolveEndpointURL(0, "grpc://10.0.0.1:443")

	if newURL != "" {
		t.Fatalf("expected empty when IP unchanged, got %q", newURL)
	}
	// Endpoint URL should NOT have changed
	if ep.URL != "grpc://10.0.0.1:443" {
		t.Fatalf("endpoint URL should not change, got %q", ep.URL)
	}
}

func TestReResolveEndpointURL_SkipsWhenNoTLSServerName(t *testing.T) {
	mock := newMockDNSLookup()
	mock.setIPs("gravity.example.com", "10.0.0.2")

	// No TLSServerName — endpoint uses hostname URL directly
	ep := &GravityEndpoint{
		URL:           "grpc://gravity.example.com:443",
		TLSServerName: "",
	}
	g := newReResolveTestClient([]*GravityEndpoint{ep}, mock)
	defer g.cancel()

	newURL := g.reResolveEndpointURL(0, "grpc://gravity.example.com:443")

	if newURL != "" {
		t.Fatalf("expected empty for non-IP endpoint, got %q", newURL)
	}
	if mock.callCount() != 0 {
		t.Fatal("DNS should not be called when TLSServerName is empty")
	}
}

func TestReResolveEndpointURL_ReturnsEmptyWhenDNSFails(t *testing.T) {
	mock := newMockDNSLookup()
	// No IPs set → returns false

	ep := &GravityEndpoint{
		URL:           "grpc://10.0.0.1:443",
		TLSServerName: "gravity.example.com",
	}
	g := newReResolveTestClient([]*GravityEndpoint{ep}, mock)
	defer g.cancel()

	newURL := g.reResolveEndpointURL(0, "grpc://10.0.0.1:443")

	if newURL != "" {
		t.Fatalf("expected empty when DNS fails, got %q", newURL)
	}
}

func TestReResolveEndpointURL_PicksDifferentIPFromMultiple(t *testing.T) {
	mock := newMockDNSLookup()
	// First IP matches current, second is new
	mock.setIPs("gravity.example.com", "10.0.0.1", "10.0.0.2", "10.0.0.3")

	ep := &GravityEndpoint{
		URL:           "grpc://10.0.0.1:443",
		TLSServerName: "gravity.example.com",
	}
	g := newReResolveTestClient([]*GravityEndpoint{ep}, mock)
	defer g.cancel()

	newURL := g.reResolveEndpointURL(0, "grpc://10.0.0.1:443")

	if newURL != "grpc://10.0.0.2:443" {
		t.Fatalf("expected first different IP (10.0.0.2), got %q", newURL)
	}
}

func TestReResolveEndpointURL_PreservesCustomPort(t *testing.T) {
	mock := newMockDNSLookup()
	mock.setIPs("gravity.example.com", "10.0.0.2")

	ep := &GravityEndpoint{
		URL:           "grpc://10.0.0.1:8443",
		TLSServerName: "gravity.example.com",
	}
	g := newReResolveTestClient([]*GravityEndpoint{ep}, mock)
	defer g.cancel()

	newURL := g.reResolveEndpointURL(0, "grpc://10.0.0.1:8443")

	if newURL != "grpc://10.0.0.2:8443" {
		t.Fatalf("expected port 8443 preserved, got %q", newURL)
	}
}

func TestReResolveEndpointURL_HandlesIPv6(t *testing.T) {
	mock := newMockDNSLookup()
	mock.setIPs("gravity.example.com", "fd15::2")

	ep := &GravityEndpoint{
		URL:           "grpc://[fd15::1]:443",
		TLSServerName: "gravity.example.com",
	}
	g := newReResolveTestClient([]*GravityEndpoint{ep}, mock)
	defer g.cancel()

	newURL := g.reResolveEndpointURL(0, "grpc://[fd15::1]:443")

	if newURL != "grpc://[fd15::2]:443" {
		t.Fatalf("expected grpc://[fd15::2]:443, got %q", newURL)
	}
}

func TestReResolveEndpointURL_OutOfBoundsIndex(t *testing.T) {
	mock := newMockDNSLookup()

	g := newReResolveTestClient([]*GravityEndpoint{}, mock)
	defer g.cancel()

	// Should not panic
	newURL := g.reResolveEndpointURL(5, "grpc://10.0.0.1:443")

	if newURL != "" {
		t.Fatalf("expected empty for out-of-bounds index, got %q", newURL)
	}
	if mock.callCount() != 0 {
		t.Fatal("DNS should not be called for out-of-bounds endpoint")
	}
}

func TestReResolveEndpointURL_NegativeIndex(t *testing.T) {
	mock := newMockDNSLookup()

	ep := &GravityEndpoint{
		URL:           "grpc://10.0.0.1:443",
		TLSServerName: "gravity.example.com",
	}
	g := newReResolveTestClient([]*GravityEndpoint{ep}, mock)
	defer g.cancel()

	newURL := g.reResolveEndpointURL(-1, "grpc://10.0.0.1:443")

	if newURL != "" {
		t.Fatalf("expected empty for negative index, got %q", newURL)
	}
}

// --- Verify reResolveAfter interval ---

func TestReResolve_CalledAtCorrectInterval(t *testing.T) {
	mock := newMockDNSLookup()
	mock.setIPs("gravity.example.com", "10.0.0.2")

	ep := &GravityEndpoint{
		URL:           "grpc://10.0.0.1:443",
		TLSServerName: "gravity.example.com",
	}
	g := newReResolveTestClient([]*GravityEndpoint{ep}, mock)
	defer g.cancel()

	// Simulate the reconnectEndpoint modulo logic
	endpointURL := "grpc://10.0.0.1:443"
	for attempt := 1; attempt <= 9; attempt++ {
		if attempt%3 == 0 {
			if newURL := g.reResolveEndpointURL(0, endpointURL); newURL != "" {
				endpointURL = newURL
			}
		}
	}

	// Should have been called 3 times (attempt 3, 6, 9)
	if mock.callCount() != 3 {
		t.Fatalf("expected 3 DNS lookups (at attempt 3, 6, 9), got %d", mock.callCount())
	}

	// URL should have been updated on first re-resolve
	if endpointURL != "grpc://10.0.0.2:443" {
		t.Fatalf("expected URL updated to grpc://10.0.0.2:443, got %q", endpointURL)
	}
}

func TestReResolve_URLUpdatesOnSecondReResolve(t *testing.T) {
	mock := newMockDNSLookup()

	ep := &GravityEndpoint{
		URL:           "grpc://10.0.0.1:443",
		TLSServerName: "gravity.example.com",
	}
	g := newReResolveTestClient([]*GravityEndpoint{ep}, mock)
	defer g.cancel()

	// First re-resolve: same IP (no change)
	mock.setIPs("gravity.example.com", "10.0.0.1")
	endpointURL := "grpc://10.0.0.1:443"
	newURL := g.reResolveEndpointURL(0, endpointURL)
	if newURL != "" {
		t.Fatalf("expected no change on first resolve, got %q", newURL)
	}

	// Second re-resolve: IP changed (rollout happened)
	mock.setIPs("gravity.example.com", "10.0.0.99")
	newURL = g.reResolveEndpointURL(0, endpointURL)
	if newURL != "grpc://10.0.0.99:443" {
		t.Fatalf("expected grpc://10.0.0.99:443 after IP change, got %q", newURL)
	}

	// Endpoint should be updated
	if ep.URL != "grpc://10.0.0.99:443" {
		t.Fatalf("endpoint URL not updated, got %q", ep.URL)
	}
}

// --- Concurrency ---

func TestReResolve_ConcurrentSafe(t *testing.T) {
	mock := newMockDNSLookup()
	mock.setIPs("gravity.example.com", "10.0.0.2", "10.0.0.3", "10.0.0.4")

	ep := &GravityEndpoint{
		URL:           "grpc://10.0.0.1:443",
		TLSServerName: "gravity.example.com",
	}
	g := newReResolveTestClient([]*GravityEndpoint{ep}, mock)
	defer g.cancel()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g.reResolveEndpointURL(0, "grpc://10.0.0.1:443")
		}()
	}
	wg.Wait()

	// No panics, no races (run with -race)
	if mock.callCount() != 20 {
		t.Fatalf("expected 20 DNS calls, got %d", mock.callCount())
	}
}

// --- Edge cases ---

func TestReResolve_AllResolvedIPsMatchCurrent(t *testing.T) {
	mock := newMockDNSLookup()
	// All resolved IPs are the same as current
	mock.setIPs("gravity.example.com", "10.0.0.1", "10.0.0.1")

	ep := &GravityEndpoint{
		URL:           "grpc://10.0.0.1:443",
		TLSServerName: "gravity.example.com",
	}
	g := newReResolveTestClient([]*GravityEndpoint{ep}, mock)
	defer g.cancel()

	newURL := g.reResolveEndpointURL(0, "grpc://10.0.0.1:443")

	if newURL != "" {
		t.Fatalf("expected empty when all IPs match current, got %q", newURL)
	}
}

func TestReResolve_SimulatesRollout(t *testing.T) {
	// Simulates a full rollout scenario:
	// 1. Three endpoints connected to old IPs
	// 2. Rollout replaces all machines → new IPs in DNS
	// 3. After 5 failed attempts each, re-resolution discovers new IPs
	mock := newMockDNSLookup()

	endpoints := []*GravityEndpoint{
		{URL: "grpc://10.0.0.1:443", TLSServerName: "gravity.example.com"},
		{URL: "grpc://10.0.0.2:443", TLSServerName: "gravity.example.com"},
		{URL: "grpc://10.0.0.3:443", TLSServerName: "gravity.example.com"},
	}
	g := newReResolveTestClient(endpoints, mock)
	defer g.cancel()

	// Rollout: DNS now returns completely new IPs
	mock.setIPs("gravity.example.com", "10.0.1.1", "10.0.1.2", "10.0.1.3")

	// Each endpoint re-resolves and gets a new IP
	newURL0 := g.reResolveEndpointURL(0, "grpc://10.0.0.1:443")
	newURL1 := g.reResolveEndpointURL(1, "grpc://10.0.0.2:443")
	newURL2 := g.reResolveEndpointURL(2, "grpc://10.0.0.3:443")

	if newURL0 == "" || newURL1 == "" || newURL2 == "" {
		t.Fatalf("expected all endpoints to get new URLs, got %q %q %q", newURL0, newURL1, newURL2)
	}

	// Verify endpoints were updated
	for i, ep := range endpoints {
		if ep.URL == "grpc://10.0.0."+string(rune('1'+i))+":443" {
			t.Fatalf("endpoint %d still has old URL: %s", i, ep.URL)
		}
	}

	// All should point to one of the new IPs
	newIPs := map[string]bool{
		"grpc://10.0.1.1:443": true,
		"grpc://10.0.1.2:443": true,
		"grpc://10.0.1.3:443": true,
	}
	for i, ep := range endpoints {
		if !newIPs[ep.URL] {
			t.Fatalf("endpoint %d has unexpected URL: %s", i, ep.URL)
		}
	}

	if mock.callCount() != 3 {
		t.Fatalf("expected 3 DNS calls, got %d", mock.callCount())
	}
}

// --- Stale DNS pool: mix of live and dead IPs ---

func TestReResolve_AvoidsIPsUsedByOtherEndpoints(t *testing.T) {
	// 3 endpoints, all failed. DNS returns 4 IPs. Two are already in use
	// by other endpoints. Each re-resolve should pick an IP not used by others.
	mock := newMockDNSLookup()
	mock.setIPs("gravity.example.com", "10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4")

	endpoints := []*GravityEndpoint{
		{URL: "grpc://10.0.0.1:443", TLSServerName: "gravity.example.com"},
		{URL: "grpc://10.0.0.2:443", TLSServerName: "gravity.example.com"},
		{URL: "grpc://10.0.0.3:443", TLSServerName: "gravity.example.com"},
	}
	g := newReResolveTestClient(endpoints, mock)
	defer g.cancel()

	// Endpoint 0 re-resolves: should skip 10.0.0.1 (self), 10.0.0.2 (ep1), 10.0.0.3 (ep2) → pick 10.0.0.4
	newURL := g.reResolveEndpointURL(0, "grpc://10.0.0.1:443")
	if newURL != "grpc://10.0.0.4:443" {
		t.Fatalf("expected 10.0.0.4 (only unused IP), got %q", newURL)
	}
}

func TestReResolve_RejectsOverlapInMultiEndpointMode(t *testing.T) {
	// All resolved IPs are in use by other endpoints. In multi-endpoint mode,
	// overlapping is forbidden — connecting two endpoints to the same ion
	// provides no redundancy. Should return empty (wait for DNS to change).
	mock := newMockDNSLookup()
	mock.setIPs("gravity.example.com", "10.0.0.1", "10.0.0.2", "10.0.0.3")

	endpoints := []*GravityEndpoint{
		{URL: "grpc://10.0.0.1:443", TLSServerName: "gravity.example.com"},
		{URL: "grpc://10.0.0.2:443", TLSServerName: "gravity.example.com"},
		{URL: "grpc://10.0.0.3:443", TLSServerName: "gravity.example.com"},
	}
	g := newReResolveTestClient(endpoints, mock)
	defer g.cancel()

	// All IPs taken — multi-endpoint mode should NOT overlap
	newURL := g.reResolveEndpointURL(0, "grpc://10.0.0.1:443")
	if newURL != "" {
		t.Fatalf("expected empty (no unique IP available in multi-endpoint mode), got %q", newURL)
	}
}

func TestReResolve_StaleDNSPoolCycling(t *testing.T) {
	// Realistic scenario: 3 endpoints (MaxGravityPeers=3), DNS returns 12 IPs.
	// Only 3 IPs are live. Each endpoint starts with a dead IP. After
	// re-resolution, each should find a unique live IP.
	mock := newMockDNSLookup()
	mock.setIPs("gravity.example.com",
		"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4",
		"10.0.0.5", "10.0.0.6", "10.0.0.7", "10.0.0.8",
		"10.0.0.9", "10.0.1.1", "10.0.1.2", "10.0.1.3",
	)

	endpoints := []*GravityEndpoint{
		{URL: "grpc://10.0.0.1:443", TLSServerName: "gravity.example.com"},
		{URL: "grpc://10.0.0.2:443", TLSServerName: "gravity.example.com"},
		{URL: "grpc://10.0.0.3:443", TLSServerName: "gravity.example.com"},
	}
	g := newReResolveTestClient(endpoints, mock)
	defer g.cancel()

	// Simulate re-resolve for endpoint 0 — should cycle through unused IPs
	currentURL := "grpc://10.0.0.1:443"
	seen := map[string]bool{currentURL: true}
	for round := 0; round < 10; round++ {
		newURL := g.reResolveEndpointURL(0, currentURL)
		if newURL != "" {
			seen[newURL] = true
			currentURL = newURL
		}
	}

	// With 3 endpoints using 3 IPs and 12 in DNS, there are 9 unused IPs.
	// Re-resolution should find different IPs each time.
	if len(seen) < 2 {
		t.Fatalf("expected endpoint to cycle through at least 2 IPs, only saw %d: %v", len(seen), seen)
	}
}

func TestReResolve_EachEndpointGetsDifferentIP(t *testing.T) {
	// When 3 endpoints all re-resolve simultaneously, they should each
	// pick a different IP from the pool (avoiding each other's IPs).
	mock := newMockDNSLookup()
	mock.setIPs("gravity.example.com", "10.0.1.1", "10.0.1.2", "10.0.1.3", "10.0.1.4", "10.0.1.5")

	endpoints := []*GravityEndpoint{
		{URL: "grpc://10.0.0.1:443", TLSServerName: "gravity.example.com"}, // dead
		{URL: "grpc://10.0.0.2:443", TLSServerName: "gravity.example.com"}, // dead
		{URL: "grpc://10.0.0.3:443", TLSServerName: "gravity.example.com"}, // dead
	}
	g := newReResolveTestClient(endpoints, mock)
	defer g.cancel()

	url0 := g.reResolveEndpointURL(0, "grpc://10.0.0.1:443")
	url1 := g.reResolveEndpointURL(1, "grpc://10.0.0.2:443")
	url2 := g.reResolveEndpointURL(2, "grpc://10.0.0.3:443")

	// All should be different from each other (first pass avoids in-use IPs)
	urls := map[string]bool{url0: true, url1: true, url2: true}
	if len(urls) != 3 {
		t.Fatalf("expected 3 unique URLs, got %d: %q %q %q", len(urls), url0, url1, url2)
	}
}

// --- Health probe filtering ---

func TestReResolve_HealthProbeFiltersDeadIPs(t *testing.T) {
	// DNS returns 4 IPs. Health probe only passes for 10.0.1.1.
	// reResolveEndpointURL should skip dead IPs and pick the healthy one.
	mock := newMockDNSLookup()
	mock.setIPs("gravity.example.com", "10.0.0.2", "10.0.0.3", "10.0.1.1", "10.0.0.4")

	liveIPs := map[string]bool{"10.0.1.1": true}

	ep := &GravityEndpoint{
		URL:           "grpc://10.0.0.1:443",
		TLSServerName: "gravity.example.com",
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := &GravityClient{
		ctx:    ctx,
		cancel: cancel,
		logger: logger.NewTestLogger(),
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 3,
		},
		dnsLookupMulti: mock.lookupMulti,
		healthProbe: func(host, port string) bool {
			return liveIPs[host]
		},
	}
	g.endpoints = []*GravityEndpoint{ep}

	newURL := g.reResolveEndpointURL(0, "grpc://10.0.0.1:443")

	if newURL != "grpc://10.0.1.1:443" {
		t.Fatalf("expected healthy IP 10.0.1.1, got %q", newURL)
	}
}

func TestReResolve_HealthProbeAllDeadFallsBack(t *testing.T) {
	// DNS returns 3 IPs. All health probes fail.
	// Should fall back to picking any different IP without probe.
	mock := newMockDNSLookup()
	mock.setIPs("gravity.example.com", "10.0.0.2", "10.0.0.3", "10.0.0.4")

	ep := &GravityEndpoint{
		URL:           "grpc://10.0.0.1:443",
		TLSServerName: "gravity.example.com",
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := &GravityClient{
		ctx:    ctx,
		cancel: cancel,
		logger: logger.NewTestLogger(),
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 3,
		},
		dnsLookupMulti: mock.lookupMulti,
		healthProbe: func(host, port string) bool {
			return false // all dead
		},
	}
	g.endpoints = []*GravityEndpoint{ep}

	newURL := g.reResolveEndpointURL(0, "grpc://10.0.0.1:443")

	// Should still pick a different IP (fallback without probe)
	if newURL == "" || newURL == "grpc://10.0.0.1:443" {
		t.Fatalf("expected fallback to any different IP, got %q", newURL)
	}
}

func TestReResolve_HealthProbeSimulatesRollout(t *testing.T) {
	// 12 IPs from DNS: 9 dead (old rollouts), 3 live (new ions).
	// Health probes should find the 3 live ones and each endpoint
	// should connect to a healthy IP.
	mock := newMockDNSLookup()
	mock.setIPs("gravity.example.com",
		"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4",
		"10.0.0.5", "10.0.0.6", "10.0.0.7", "10.0.0.8",
		"10.0.0.9", "10.0.1.1", "10.0.1.2", "10.0.1.3",
	)

	liveIPs := map[string]bool{"10.0.1.1": true, "10.0.1.2": true, "10.0.1.3": true}

	endpoints := []*GravityEndpoint{
		{URL: "grpc://10.0.0.1:443", TLSServerName: "gravity.example.com"},
		{URL: "grpc://10.0.0.2:443", TLSServerName: "gravity.example.com"},
		{URL: "grpc://10.0.0.3:443", TLSServerName: "gravity.example.com"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := &GravityClient{
		ctx:    ctx,
		cancel: cancel,
		logger: logger.NewTestLogger(),
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 3,
		},
		dnsLookupMulti: mock.lookupMulti,
		healthProbe: func(host, port string) bool {
			return liveIPs[host]
		},
	}
	g.endpoints = endpoints

	url0 := g.reResolveEndpointURL(0, "grpc://10.0.0.1:443")
	url1 := g.reResolveEndpointURL(1, "grpc://10.0.0.2:443")
	url2 := g.reResolveEndpointURL(2, "grpc://10.0.0.3:443")

	// All 3 should connect to live IPs
	validURLs := map[string]bool{
		"grpc://10.0.1.1:443": true,
		"grpc://10.0.1.2:443": true,
		"grpc://10.0.1.3:443": true,
	}
	for i, u := range []string{url0, url1, url2} {
		if !validURLs[u] {
			t.Fatalf("endpoint %d got %q, expected one of the 3 live IPs", i, u)
		}
	}

	// All 3 should be different (avoid-in-use logic)
	urls := map[string]bool{url0: true, url1: true, url2: true}
	if len(urls) != 3 {
		t.Fatalf("expected 3 unique URLs, got %d: %q %q %q", len(urls), url0, url1, url2)
	}
}

// --- Adaptive peer discovery ---

func TestCountHealthyEndpoints(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ep0 := &GravityEndpoint{URL: "grpc://10.0.0.1:443"}
	ep1 := &GravityEndpoint{URL: "grpc://10.0.0.2:443"}
	ep2 := &GravityEndpoint{URL: "grpc://10.0.0.3:443"}
	ep0.healthy.Store(true)
	ep1.healthy.Store(false)
	ep2.healthy.Store(true)

	g := &GravityClient{
		ctx:       ctx,
		cancel:    cancel,
		endpoints: []*GravityEndpoint{ep0, ep1, ep2},
	}

	if g.countHealthyEndpoints() != 2 {
		t.Fatalf("expected 2 healthy, got %d", g.countHealthyEndpoints())
	}
}

func TestCountHealthyEndpoints_NoneHealthy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eps := make([]*GravityEndpoint, 3)
	for i := range eps {
		eps[i] = &GravityEndpoint{URL: fmt.Sprintf("grpc://10.0.0.%d:443", i+1)}
	}

	g := &GravityClient{ctx: ctx, cancel: cancel, endpoints: eps}
	if g.countHealthyEndpoints() != 0 {
		t.Fatal("expected 0")
	}
}

func TestCountHealthyEndpoints_Empty(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := &GravityClient{ctx: ctx, cancel: cancel}
	if g.countHealthyEndpoints() != 0 {
		t.Fatal("expected 0 for no endpoints")
	}
}

func TestCountHealthyEndpoints_WithNilEntries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ep := &GravityEndpoint{URL: "grpc://10.0.0.1:443"}
	ep.healthy.Store(true)

	g := &GravityClient{
		ctx:       ctx,
		cancel:    cancel,
		endpoints: []*GravityEndpoint{nil, ep, nil},
	}
	if g.countHealthyEndpoints() != 1 {
		t.Fatal("expected 1 with nil entries")
	}
}
