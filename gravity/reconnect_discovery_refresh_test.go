package gravity

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/agentuity/go-common/logger"
)

func newReconnectRefreshTestClient(endpoints []*GravityEndpoint, urls []string, discover func() []string) *GravityClient {
	ctx, cancel := context.WithCancel(context.Background())
	g := &GravityClient{
		ctx:                  ctx,
		cancel:               cancel,
		logger:               logger.NewTestLogger(),
		defaultServerName:    "gravity-usw.agentuity.cloud",
		endpoints:            endpoints,
		connectionURLs:       urls,
		endpointFailCount:    make([]atomic.Int32, len(endpoints)),
		discoveryResolveFunc: discover,
	}
	return g
}

func TestGravityClient_ReResolvesAfterFailures(t *testing.T) {
	endpoints := []*GravityEndpoint{{URL: "grpc://10.0.0.1:443"}}
	urls := []string{"grpc://10.0.0.1:443"}
	g := newReconnectRefreshTestClient(endpoints, urls, func() []string {
		return []string{"grpc://10.0.0.1:443", "grpc://10.0.0.99:443"}
	})
	t.Cleanup(g.cancel)

	current := "grpc://10.0.0.1:443"
	for i := int32(1); i < endpointDiscoveryRefreshFailureThreshold; i++ {
		updated := g.handleEndpointReconnectFailure(0, current)
		if updated != current {
			t.Fatalf("unexpected URL change before threshold at attempt %d: %s", i, updated)
		}
	}

	updated := g.handleEndpointReconnectFailure(0, current)
	if updated != "grpc://10.0.0.99:443" {
		t.Fatalf("expected URL to refresh at threshold, got %s", updated)
	}
	if g.endpoints[0].URL != "grpc://10.0.0.99:443" {
		t.Fatalf("expected endpoint URL updated, got %s", g.endpoints[0].URL)
	}
	if g.connectionURLs[0] != "grpc://10.0.0.99:443" {
		t.Fatalf("expected connection URL updated, got %s", g.connectionURLs[0])
	}
}

func TestGravityClient_MixedEndpoints_OnlyReResolveFailing(t *testing.T) {
	endpoints := []*GravityEndpoint{
		{URL: "grpc://10.0.0.1:443"},
		{URL: "grpc://10.0.0.2:443"},
	}
	urls := []string{"grpc://10.0.0.1:443", "grpc://10.0.0.2:443"}
	g := newReconnectRefreshTestClient(endpoints, urls, func() []string {
		return []string{"grpc://10.0.0.1:443", "grpc://10.0.0.2:443", "grpc://10.0.0.77:443"}
	})
	t.Cleanup(g.cancel)

	current := "grpc://10.0.0.1:443"
	for i := int32(0); i < endpointDiscoveryRefreshFailureThreshold; i++ {
		current = g.handleEndpointReconnectFailure(0, current)
	}

	if g.endpoints[0].URL != "grpc://10.0.0.77:443" {
		t.Fatalf("expected failing endpoint to refresh, got %s", g.endpoints[0].URL)
	}
	if g.endpoints[1].URL != "grpc://10.0.0.2:443" {
		t.Fatalf("expected healthy endpoint unchanged, got %s", g.endpoints[1].URL)
	}
	if g.connectionURLs[1] != "grpc://10.0.0.2:443" {
		t.Fatalf("expected healthy endpoint connection URL unchanged, got %s", g.connectionURLs[1])
	}
	if g.endpointFailCount[1].Load() != 0 {
		t.Fatalf("expected non-failing endpoint failure count untouched, got %d", g.endpointFailCount[1].Load())
	}
}
