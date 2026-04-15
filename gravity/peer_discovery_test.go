package gravity

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/agentuity/go-common/logger"
)

// newTestGravityClient creates a minimal GravityClient suitable for testing
// peer discovery and cycling logic without real gRPC connections.
func newTestGravityClient(urls []string, connected []string) *GravityClient {
	ctx, cancel := context.WithCancel(context.Background())
	g := &GravityClient{
		ctx:    ctx,
		cancel: cancel,
		logger: logger.NewTestLogger(),
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 3,
		},
	}
	g.gravityURLs = append([]string(nil), urls...)

	for _, u := range connected {
		ep := &GravityEndpoint{URL: u}
		ep.healthy.Store(true)
		g.endpoints = append(g.endpoints, ep)
	}

	return g
}

// ---------- resolveGravityURLs ----------

func TestResolveGravityURLs_SingleURL(t *testing.T) {
	g := &GravityClient{
		url: "grpc://gravity1.example.com",
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 3,
		},
	}
	// No gravityURLs set, should fall back to g.url
	got := g.resolveGravityURLs()
	if len(got) != 1 {
		t.Fatalf("expected 1 URL, got %d", len(got))
	}
	if got[0] != "grpc://gravity1.example.com" {
		t.Fatalf("expected fallback URL, got %s", got[0])
	}
}

func TestResolveGravityURLs_MultipleURLs(t *testing.T) {
	g := &GravityClient{
		gravityURLs: []string{
			"grpc://g1.example.com",
			"grpc://g2.example.com",
			"grpc://g3.example.com",
		},
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 3,
		},
	}
	got := g.resolveGravityURLs()
	if len(got) != 3 {
		t.Fatalf("expected 3 URLs, got %d", len(got))
	}
	for i, u := range g.gravityURLs {
		if got[i] != u {
			t.Errorf("URL[%d]: expected %s, got %s", i, u, got[i])
		}
	}
}

func TestResolveGravityURLs_IncludesAllCandidates(t *testing.T) {
	// resolveGravityURLs no longer caps at MaxGravityPeers — all candidates
	// pass through so establishControlStreamsMulti can race them in parallel.
	urls := make([]string, 10)
	for i := range urls {
		urls[i] = fmt.Sprintf("grpc://g%d.example.com", i)
	}
	g := &GravityClient{
		gravityURLs: urls,
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 3,
		},
	}
	got := g.resolveGravityURLs()
	if len(got) != 10 {
		t.Fatalf("expected all 10 URLs (cap at connection time), got %d", len(got))
	}
}

func TestResolveGravityURLs_DeduplicatesURLs(t *testing.T) {
	g := &GravityClient{
		gravityURLs: []string{
			"grpc://g1.example.com",
			"grpc://g1.example.com",
			"grpc://g2.example.com",
			"grpc://g2.example.com",
		},
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 5,
		},
	}
	got := g.resolveGravityURLs()
	if len(got) != 2 {
		t.Fatalf("expected 2 unique URLs, got %d: %v", len(got), got)
	}
	if got[0] != "grpc://g1.example.com" || got[1] != "grpc://g2.example.com" {
		t.Fatalf("unexpected URLs: %v", got)
	}
}

func TestResolveGravityURLs_EmptyStringsSkipped(t *testing.T) {
	g := &GravityClient{
		gravityURLs: []string{
			"",
			"  ",
			"grpc://g1.example.com",
			"",
			"grpc://g2.example.com",
		},
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 5,
		},
	}
	got := g.resolveGravityURLs()
	if len(got) != 2 {
		t.Fatalf("expected 2 URLs (empty/whitespace filtered), got %d: %v", len(got), got)
	}
	if got[0] != "grpc://g1.example.com" || got[1] != "grpc://g2.example.com" {
		t.Fatalf("unexpected URLs: %v", got)
	}
}

func TestResolveGravityURLs_EmptyFallback(t *testing.T) {
	g := &GravityClient{
		url: "",
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 3,
		},
	}
	got := g.resolveGravityURLs()
	if got != nil {
		t.Fatalf("expected nil for empty fallback, got %v", got)
	}
}

func TestResolveGravityURLs_AllEmptyGravityURLsFallsBackToURL(t *testing.T) {
	g := &GravityClient{
		url:         "grpc://fallback.example.com",
		gravityURLs: []string{"", "  ", ""},
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 3,
		},
	}
	got := g.resolveGravityURLs()
	if len(got) != 1 || got[0] != "grpc://fallback.example.com" {
		t.Fatalf("expected single fallback URL, got %v", got)
	}
}

func TestResolveGravityURLs_MaxPeersZeroIncludesAll(t *testing.T) {
	urls := make([]string, 10)
	for i := range urls {
		urls[i] = "grpc://g" + string(rune('a'+i)) + ".example.com"
	}
	g := &GravityClient{
		gravityURLs: urls,
		poolConfig: ConnectionPoolConfig{
			MaxGravityPeers: 0,
		},
	}
	got := g.resolveGravityURLs()
	if len(got) != 10 {
		t.Fatalf("expected all 10 URLs, got %d", len(got))
	}
}

// ---------- pickRandomURL ----------

func TestPickRandomURL_SingleElement(t *testing.T) {
	got := pickRandomURL([]string{"grpc://only.example.com"})
	if got != "grpc://only.example.com" {
		t.Fatalf("expected the only element, got %s", got)
	}
}

func TestPickRandomURL_EmptySlice(t *testing.T) {
	got := pickRandomURL(nil)
	if got != "" {
		t.Fatalf("expected empty string for nil slice, got %s", got)
	}
	got = pickRandomURL([]string{})
	if got != "" {
		t.Fatalf("expected empty string for empty slice, got %s", got)
	}
}

func TestPickRandomURL_MultipleElements(t *testing.T) {
	urls := []string{
		"grpc://a.example.com",
		"grpc://b.example.com",
		"grpc://c.example.com",
	}
	valid := make(map[string]bool, len(urls))
	for _, u := range urls {
		valid[u] = true
	}
	// Call multiple times to confirm it always returns a valid element.
	for i := 0; i < 50; i++ {
		got := pickRandomURL(urls)
		if !valid[got] {
			t.Fatalf("pickRandomURL returned %q which is not in the input slice", got)
		}
	}
}

// ---------- randIndex ----------

func TestRandIndex_ZeroOrOne(t *testing.T) {
	if randIndex(0) != 0 {
		t.Fatal("expected 0 for n=0")
	}
	if randIndex(1) != 0 {
		t.Fatal("expected 0 for n=1")
	}
}

func TestRandIndex_BoundsCheck(t *testing.T) {
	for i := 0; i < 100; i++ {
		idx := randIndex(5)
		if idx < 0 || idx >= 5 {
			t.Fatalf("randIndex(5) returned out-of-bounds value %d", idx)
		}
	}
}

// ---------- checkPeerDiscovery ----------

func TestCheckPeerDiscovery_NoResolver(t *testing.T) {
	g := newTestGravityClient(nil, nil)
	defer g.cancel()
	g.discoveryResolveFunc = nil
	// Should be a no-op, not panic.
	g.checkPeerDiscovery(2 * time.Hour)
}

func TestCheckPeerDiscovery_ResolverReturnsEmpty(t *testing.T) {
	g := newTestGravityClient(nil, []string{"grpc://g1.example.com"})
	defer g.cancel()
	g.discoveryResolveFunc = func() []string { return nil }
	// No-op: resolver returned nothing.
	g.checkPeerDiscovery(2 * time.Hour)
	// Endpoints should remain unchanged.
	g.endpointsMu.RLock()
	count := len(g.endpoints)
	g.endpointsMu.RUnlock()
	if count != 1 {
		t.Fatalf("expected 1 endpoint unchanged, got %d", count)
	}
}

func TestCheckPeerDiscovery_FullCoverage(t *testing.T) {
	connected := []string{
		"grpc://g1.example.com",
		"grpc://g2.example.com",
		"grpc://g3.example.com",
	}
	g := newTestGravityClient(connected, connected)
	defer g.cancel()
	g.discoveryResolveFunc = func() []string {
		return []string{
			"grpc://g1.example.com",
			"grpc://g2.example.com",
			"grpc://g3.example.com",
		}
	}
	// All DNS URLs are already connected -- should not cycle.
	g.checkPeerDiscovery(2 * time.Hour)
	g.endpointsMu.RLock()
	for _, ep := range g.endpoints {
		found := false
		for _, c := range connected {
			if ep.URL == c {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("unexpected endpoint %s after full coverage check", ep.URL)
		}
	}
	g.endpointsMu.RUnlock()
}

func TestCheckPeerDiscovery_StaleURLReplaced(t *testing.T) {
	// Connected to g1, g2, g3 but DNS returns g1, g2, g4, g5.
	// The stale replacement path requires len(allURLs) > currentCount,
	// so DNS must expose more hosts than we are connected to.
	// g3 is stale (not in DNS) and should be replaced immediately.
	connected := []string{
		"grpc://g1.example.com",
		"grpc://g2.example.com",
		"grpc://g3.example.com",
	}
	g := newTestGravityClient(nil, connected)
	defer g.cancel()
	g.discoveryResolveFunc = func() []string {
		return []string{
			"grpc://g1.example.com",
			"grpc://g2.example.com",
			"grpc://g4.example.com",
			"grpc://g5.example.com",
		}
	}
	g.poolConfig.MaxGravityPeers = 3

	g.checkPeerDiscovery(2 * time.Hour)

	// g3 should be replaced by one of the new URLs (g4 or g5).
	g.endpointsMu.RLock()
	urls := make(map[string]bool)
	for _, ep := range g.endpoints {
		urls[ep.URL] = true
	}
	g.endpointsMu.RUnlock()

	if urls["grpc://g3.example.com"] {
		t.Fatal("stale endpoint g3 was not replaced")
	}
	if !urls["grpc://g4.example.com"] && !urls["grpc://g5.example.com"] {
		t.Fatal("expected one of g4 or g5 to be added as replacement")
	}
}

func TestCheckPeerDiscovery_NewURLDiscovered(t *testing.T) {
	// Connected to g1, g2, g3. DNS returns g1, g2, g3, g4.
	// More URLs available than connected -- should cycle one endpoint.
	connected := []string{
		"grpc://g1.example.com",
		"grpc://g2.example.com",
		"grpc://g3.example.com",
	}
	g := newTestGravityClient(nil, connected)
	defer g.cancel()
	g.discoveryResolveFunc = func() []string {
		return []string{
			"grpc://g1.example.com",
			"grpc://g2.example.com",
			"grpc://g3.example.com",
			"grpc://g4.example.com",
		}
	}
	g.poolConfig.MaxGravityPeers = 3

	// lastCycleTime is zero so the interval check should pass.
	g.checkPeerDiscovery(2 * time.Hour)

	// One endpoint should have been replaced with g4.
	g.endpointsMu.RLock()
	urls := make(map[string]bool)
	for _, ep := range g.endpoints {
		urls[ep.URL] = true
	}
	g.endpointsMu.RUnlock()

	if !urls["grpc://g4.example.com"] {
		t.Fatal("expected g4 to be added via cycling")
	}
	if len(urls) != 3 {
		t.Fatalf("expected 3 endpoints after cycling, got %d", len(urls))
	}
}

func TestCheckPeerDiscovery_CycleIntervalRespected(t *testing.T) {
	// Connected to g1, g2, g3. DNS returns g1, g2, g3, g4.
	// Last cycle was recent, so no cycling should occur.
	connected := []string{
		"grpc://g1.example.com",
		"grpc://g2.example.com",
		"grpc://g3.example.com",
	}
	g := newTestGravityClient(nil, connected)
	defer g.cancel()
	g.discoveryResolveFunc = func() []string {
		return []string{
			"grpc://g1.example.com",
			"grpc://g2.example.com",
			"grpc://g3.example.com",
			"grpc://g4.example.com",
		}
	}
	g.poolConfig.MaxGravityPeers = 3

	// Pretend we cycled 10 minutes ago, with a 2 hour interval.
	g.lastCycleTime.Store(time.Now().Add(-10 * time.Minute).Unix())

	g.checkPeerDiscovery(2 * time.Hour)

	// g4 should NOT have been added because cycle interval hasn't elapsed.
	g.endpointsMu.RLock()
	urls := make(map[string]bool)
	for _, ep := range g.endpoints {
		urls[ep.URL] = true
	}
	g.endpointsMu.RUnlock()

	if urls["grpc://g4.example.com"] {
		t.Fatal("cycling should not occur before cycle interval elapses")
	}
	for _, c := range connected {
		if !urls[c] {
			t.Fatalf("expected original endpoint %s to remain, not found", c)
		}
	}
}

func TestCheckPeerDiscovery_CycleAfterIntervalElapses(t *testing.T) {
	connected := []string{
		"grpc://g1.example.com",
		"grpc://g2.example.com",
		"grpc://g3.example.com",
	}
	g := newTestGravityClient(nil, connected)
	defer g.cancel()
	g.discoveryResolveFunc = func() []string {
		return []string{
			"grpc://g1.example.com",
			"grpc://g2.example.com",
			"grpc://g3.example.com",
			"grpc://g4.example.com",
		}
	}
	g.poolConfig.MaxGravityPeers = 3

	// Pretend we cycled 3 hours ago, with a 2 hour interval.
	g.lastCycleTime.Store(time.Now().Add(-3 * time.Hour).Unix())

	g.checkPeerDiscovery(2 * time.Hour)

	// g4 should now appear because cycle interval elapsed.
	g.endpointsMu.RLock()
	urls := make(map[string]bool)
	for _, ep := range g.endpoints {
		urls[ep.URL] = true
	}
	g.endpointsMu.RUnlock()

	if !urls["grpc://g4.example.com"] {
		t.Fatal("expected cycling to occur after interval elapsed")
	}
}

// ---------- cycleEndpoint ----------

func TestCycleEndpoint_ReplacesEndpoint(t *testing.T) {
	g := newTestGravityClient(nil, []string{
		"grpc://g1.example.com",
		"grpc://g2.example.com",
		"grpc://g3.example.com",
	})
	defer g.cancel()

	g.cycleEndpoint("grpc://g2.example.com", "grpc://g4.example.com")

	g.endpointsMu.RLock()
	defer g.endpointsMu.RUnlock()

	if len(g.endpoints) != 3 {
		t.Fatalf("expected 3 endpoints, got %d", len(g.endpoints))
	}

	urls := make(map[string]bool)
	for _, ep := range g.endpoints {
		urls[ep.URL] = true
	}

	if urls["grpc://g2.example.com"] {
		t.Fatal("old endpoint g2 should have been replaced")
	}
	if !urls["grpc://g4.example.com"] {
		t.Fatal("new endpoint g4 should be present")
	}
	if !urls["grpc://g1.example.com"] || !urls["grpc://g3.example.com"] {
		t.Fatal("unaffected endpoints should remain")
	}
}

func TestCycleEndpoint_NewEndpointStartsUnhealthy(t *testing.T) {
	g := newTestGravityClient(nil, []string{"grpc://g1.example.com"})
	defer g.cancel()

	g.cycleEndpoint("grpc://g1.example.com", "grpc://g2.example.com")

	g.endpointsMu.RLock()
	defer g.endpointsMu.RUnlock()

	for _, ep := range g.endpoints {
		if ep.URL == "grpc://g2.example.com" {
			if ep.healthy.Load() {
				t.Fatal("new endpoint should start unhealthy")
			}
			return
		}
	}
	t.Fatal("new endpoint g2 not found")
}

func TestCycleEndpoint_OldNotFound(t *testing.T) {
	g := newTestGravityClient(nil, []string{
		"grpc://g1.example.com",
		"grpc://g2.example.com",
	})
	defer g.cancel()

	// Cycling with a URL that does not exist should be a no-op (no panic).
	g.cycleEndpoint("grpc://nonexistent.example.com", "grpc://g3.example.com")

	g.endpointsMu.RLock()
	defer g.endpointsMu.RUnlock()

	if len(g.endpoints) != 2 {
		t.Fatalf("expected 2 endpoints unchanged, got %d", len(g.endpoints))
	}
	urls := make(map[string]bool)
	for _, ep := range g.endpoints {
		urls[ep.URL] = true
	}
	if !urls["grpc://g1.example.com"] || !urls["grpc://g2.example.com"] {
		t.Fatal("endpoints should be unchanged when old URL not found")
	}
}

func TestCycleEndpoint_EmptyURLsSkipped(t *testing.T) {
	g := newTestGravityClient(nil, []string{"grpc://g1.example.com"})
	defer g.cancel()

	// Empty old URL -- no-op.
	g.cycleEndpoint("", "grpc://g2.example.com")
	g.endpointsMu.RLock()
	if g.endpoints[0].URL != "grpc://g1.example.com" {
		t.Fatal("endpoint should be unchanged for empty old URL")
	}
	g.endpointsMu.RUnlock()

	// Empty new URL -- no-op.
	g.cycleEndpoint("grpc://g1.example.com", "")
	g.endpointsMu.RLock()
	if g.endpoints[0].URL != "grpc://g1.example.com" {
		t.Fatal("endpoint should be unchanged for empty new URL")
	}
	g.endpointsMu.RUnlock()
}

func TestCycleEndpoint_PreservesSliceOrder(t *testing.T) {
	g := newTestGravityClient(nil, []string{
		"grpc://g1.example.com",
		"grpc://g2.example.com",
		"grpc://g3.example.com",
	})
	defer g.cancel()

	g.cycleEndpoint("grpc://g2.example.com", "grpc://g4.example.com")

	g.endpointsMu.RLock()
	defer g.endpointsMu.RUnlock()

	expected := []string{
		"grpc://g1.example.com",
		"grpc://g4.example.com", // replaced g2 at index 1
		"grpc://g3.example.com",
	}
	for i, ep := range g.endpoints {
		if ep.URL != expected[i] {
			t.Errorf("endpoint[%d]: expected %s, got %s", i, expected[i], ep.URL)
		}
	}
}
