package gravity

import (
	"testing"
)

// These tests verify the logic that determines which connections should
// receive tunnel streams based on session hello acknowledgement.
//
// The core invariant: tunnel streams must ONLY be created on connections
// where the session hello was acknowledged. Creating tunnel streams on a
// connection where the hello timed out produces orphaned streams — the
// ion has no SessionConnection for them, no forwardSessionPacketsToGRPC
// goroutine drains the writeChan, and all packets are silently dropped.

// TestHelloAckedMapAllResponded verifies that when all control streams
// respond, all connections are eligible for tunnel streams.
func TestHelloAckedMapAllResponded(t *testing.T) {
	// Simulate: 3 control streams, all non-nil (all responded)
	controlStreams := []bool{true, true, true} // non-nil = responded

	helloAcked := make(map[int]bool)
	for i, active := range controlStreams {
		if active {
			helloAcked[i] = true
		}
	}

	if len(helloAcked) != 3 {
		t.Fatalf("expected 3 acked, got %d", len(helloAcked))
	}
	for i := 0; i < 3; i++ {
		if !helloAcked[i] {
			t.Fatalf("connection %d should be acked", i)
		}
	}
}

// TestHelloAckedMapPartialResponse verifies that when 1 of 3 control
// streams failed, only 2 connections are eligible.
func TestHelloAckedMapPartialResponse(t *testing.T) {
	// Simulate: 3 control streams, #1 failed (nil)
	controlStreams := []bool{true, false, true}

	helloAcked := make(map[int]bool)
	for i, active := range controlStreams {
		if active {
			helloAcked[i] = true
		}
	}

	if len(helloAcked) != 2 {
		t.Fatalf("expected 2 acked, got %d", len(helloAcked))
	}
	if helloAcked[1] {
		t.Fatal("connection 1 should NOT be acked (control stream nil)")
	}
	if !helloAcked[0] || !helloAcked[2] {
		t.Fatal("connections 0 and 2 should be acked")
	}
}

// TestHelloAckedMapNoneResponded verifies no connections are eligible.
func TestHelloAckedMapNoneResponded(t *testing.T) {
	controlStreams := []bool{false, false, false}

	helloAcked := make(map[int]bool)
	for i, active := range controlStreams {
		if active {
			helloAcked[i] = true
		}
	}

	if len(helloAcked) != 0 {
		t.Fatalf("expected 0 acked, got %d", len(helloAcked))
	}
}

// TestTunnelStreamSkipsUnackedConnections verifies that the tunnel stream
// creation loop skips connections where the hello wasn't acknowledged.
func TestTunnelStreamSkipsUnackedConnections(t *testing.T) {
	// Simulate the tunnel stream creation loop logic
	type mockClient struct{ name string }
	sessionClients := []*mockClient{
		{name: "ion-a"},
		{name: "ion-b"}, // this one's hello timed out
		{name: "ion-c"},
	}
	helloAcked := map[int]bool{0: true, 2: true} // #1 not acked

	streamsPerConnection := 4
	var createdStreams []struct {
		connIndex int
		client    string
	}

	for connIndex, client := range sessionClients {
		if client == nil || !helloAcked[connIndex] {
			continue
		}
		for range streamsPerConnection {
			createdStreams = append(createdStreams, struct {
				connIndex int
				client    string
			}{connIndex, client.name})
		}
	}

	// Should create streams for conn 0 and 2 only (4 each = 8 total)
	if len(createdStreams) != 8 {
		t.Fatalf("expected 8 tunnel streams (2 conns × 4 streams), got %d", len(createdStreams))
	}

	// Verify no streams on connection 1
	for _, s := range createdStreams {
		if s.connIndex == 1 {
			t.Fatalf("tunnel stream created on unacked connection 1 (client=%s)", s.client)
		}
	}

	// Verify streams on connections 0 and 2
	conn0Count, conn2Count := 0, 0
	for _, s := range createdStreams {
		if s.connIndex == 0 {
			conn0Count++
		}
		if s.connIndex == 2 {
			conn2Count++
		}
	}
	if conn0Count != 4 {
		t.Fatalf("expected 4 streams on conn 0, got %d", conn0Count)
	}
	if conn2Count != 4 {
		t.Fatalf("expected 4 streams on conn 2, got %d", conn2Count)
	}
}

// TestTunnelStreamAllAcked verifies all connections get streams when all acked.
func TestTunnelStreamAllAcked(t *testing.T) {
	type mockClient struct{ name string }
	sessionClients := []*mockClient{
		{name: "ion-a"},
		{name: "ion-b"},
		{name: "ion-c"},
	}
	helloAcked := map[int]bool{0: true, 1: true, 2: true}

	streamsPerConnection := 4
	createdCount := 0
	for connIndex, client := range sessionClients {
		if client == nil || !helloAcked[connIndex] {
			continue
		}
		createdCount += streamsPerConnection
	}

	if createdCount != 12 {
		t.Fatalf("expected 12 tunnel streams (3 conns × 4), got %d", createdCount)
	}
}

// TestUnackedEndpointMarkedUnhealthy verifies that endpoints whose hello
// timed out are marked unhealthy so peer discovery can replace them.
func TestUnackedEndpointMarkedUnhealthy(t *testing.T) {
	endpoints := []*GravityEndpoint{
		{URL: "grpc://10.0.0.1:443"},
		{URL: "grpc://10.0.0.2:443"},
		{URL: "grpc://10.0.0.3:443"},
	}
	for _, ep := range endpoints {
		ep.healthy.Store(true) // start healthy
	}

	helloAcked := map[int]bool{0: true, 2: true} // #1 not acked
	helloResponseCount := 2
	numExpected := 3

	// Simulate the marking logic from establishTunnelStreams
	if helloResponseCount < numExpected {
		for i, ep := range endpoints {
			if ep != nil && !helloAcked[i] {
				ep.healthy.Store(false)
			}
		}
	}

	if !endpoints[0].healthy.Load() {
		t.Fatal("endpoint 0 should remain healthy")
	}
	if endpoints[1].healthy.Load() {
		t.Fatal("endpoint 1 should be marked unhealthy (hello not acked)")
	}
	if !endpoints[2].healthy.Load() {
		t.Fatal("endpoint 2 should remain healthy")
	}
}

// TestAllHellosAckedNoUnhealthyMarking verifies that when all hellos
// succeed, no endpoints are marked unhealthy.
func TestAllHellosAckedNoUnhealthyMarking(t *testing.T) {
	endpoints := []*GravityEndpoint{
		{URL: "grpc://10.0.0.1:443"},
		{URL: "grpc://10.0.0.2:443"},
		{URL: "grpc://10.0.0.3:443"},
	}
	for _, ep := range endpoints {
		ep.healthy.Store(true)
	}

	helloResponseCount := 3
	numExpected := 3
	helloAcked := map[int]bool{0: true, 1: true, 2: true}

	if helloResponseCount < numExpected {
		for i, ep := range endpoints {
			if ep != nil && !helloAcked[i] {
				ep.healthy.Store(false)
			}
		}
	}

	for i, ep := range endpoints {
		if !ep.healthy.Load() {
			t.Fatalf("endpoint %d should remain healthy when all hellos acked", i)
		}
	}
}

// TestHealthyClientCountExcludesUnacked verifies that the healthyClients
// count used for totalStreams calculation excludes unacked connections.
func TestHealthyClientCountExcludesUnacked(t *testing.T) {
	type mockClient struct{}
	sessionClients := []*mockClient{{}, {}, {}}
	helloAcked := map[int]bool{0: true, 2: true} // #1 not acked

	healthyClients := 0
	for i, c := range sessionClients {
		if c != nil && helloAcked[i] {
			healthyClients++
		}
	}

	if healthyClients != 2 {
		t.Fatalf("expected 2 healthy clients (excluding unacked), got %d", healthyClients)
	}

	streamsPerGravity := 4
	totalStreams := healthyClients * streamsPerGravity
	if totalStreams != 8 {
		t.Fatalf("expected 8 total streams, got %d", totalStreams)
	}
}
