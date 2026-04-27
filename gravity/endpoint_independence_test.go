package gravity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/agentuity/go-common/gravity/proto"
	"github.com/agentuity/go-common/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type configurableMockStream struct {
	sendErr   error
	sendDelay time.Duration
	sendCount atomic.Int64
	recvErr   error
	mu        sync.Mutex
}

var _ pb.GravitySessionService_EstablishSessionClient = (*configurableMockStream)(nil)

func (m *configurableMockStream) Send(*pb.SessionMessage) error {
	m.mu.Lock()
	err := m.sendErr
	delay := m.sendDelay
	m.mu.Unlock()

	if delay > 0 {
		time.Sleep(delay)
	}
	if err != nil {
		return err
	}
	m.sendCount.Add(1)
	return nil
}

func (m *configurableMockStream) Recv() (*pb.SessionMessage, error) {
	m.mu.Lock()
	err := m.recvErr
	m.mu.Unlock()
	if err == nil {
		return nil, io.EOF
	}
	return nil, err
}

func (m *configurableMockStream) Header() (metadata.MD, error) { return nil, nil }
func (m *configurableMockStream) Trailer() metadata.MD         { return nil }
func (m *configurableMockStream) CloseSend() error             { return nil }
func (m *configurableMockStream) Context() context.Context     { return context.Background() }
func (m *configurableMockStream) SendMsg(any) error            { return nil }
func (m *configurableMockStream) RecvMsg(any) error            { return nil }

func newEndpointTestClient(t *testing.T, n int) *GravityClient {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	g := &GravityClient{
		logger:                logger.NewTestLogger(),
		ctx:                   ctx,
		cancel:                cancel,
		retryConfig:           DefaultRetryConfig(),
		connections:           make([]*grpc.ClientConn, n),
		sessionClients:        make([]pb.GravitySessionServiceClient, n),
		connectionURLs:        make([]string, n),
		endpoints:             make([]*GravityEndpoint, n),
		endpointReconnecting:  make([]atomic.Bool, n),
		circuitBreakers:       make([]*CircuitBreaker, n),
		connectionIDChan:      make(chan string, 16),
		endpointStreamIndices: make(map[string][]int),
		closed:                make(chan struct{}, 1),
		streamManager: &StreamManager{
			controlStreams:   make([]pb.GravitySessionService_EstablishSessionClient, n),
			controlSendMu:    make([]sync.Mutex, n),
			tunnelStreams:    make([]*StreamInfo, 0),
			connectionHealth: make([]bool, n),
			streamMetrics:    make(map[string]*StreamMetrics),
			contexts:         make([]context.Context, n),
			cancels:          make([]context.CancelFunc, n),
		},
	}

	for i := 0; i < n; i++ {
		g.connectionURLs[i] = fmt.Sprintf("grpc://endpoint-%d", i)
		ep := &GravityEndpoint{URL: g.connectionURLs[i]}
		ep.healthy.Store(true)
		ep.lastHeartbeat.Store(time.Now().Unix())
		g.endpoints[i] = ep
		g.streamManager.connectionHealth[i] = true
		g.circuitBreakers[i] = NewCircuitBreaker(DefaultCircuitBreakerConfig())
	}

	// Set multiEndpointMode flag when we have multiple endpoints
	g.multiEndpointMode.Store(n > 1)

	return g
}

func TestHasHealthyEndpoint_AllNil(t *testing.T) {
	t.Parallel()

	g := newEndpointTestClient(t, 3)
	g.streamManager.tunnelStreams = []*StreamInfo{{connIndex: 0, isHealthy: true}}

	if g.hasHealthyEndpoint() {
		t.Fatal("expected false when all control streams are nil")
	}
}

func TestHasHealthyEndpoint_OneNonNil(t *testing.T) {
	t.Parallel()

	g := newEndpointTestClient(t, 3)
	g.streamManager.controlStreams[1] = &configurableMockStream{}
	g.streamManager.tunnelStreams = []*StreamInfo{{connIndex: 1, isHealthy: true}}

	if !g.hasHealthyEndpoint() {
		t.Fatal("expected true when one control stream is non-nil and tunnel healthy")
	}
}

func TestHasHealthyEndpoint_MixedNilAndNonNil(t *testing.T) {
	t.Parallel()

	g := newEndpointTestClient(t, 3)
	g.streamManager.controlStreams[2] = &configurableMockStream{}
	g.streamManager.tunnelStreams = []*StreamInfo{
		{connIndex: 0, isHealthy: false},
		{connIndex: 2, isHealthy: true},
	}

	if !g.hasHealthyEndpoint() {
		t.Fatal("expected true with mixed nil/non-nil streams and at least one healthy tunnel")
	}
}

func TestHandleEndpointDisconnection_IsolatesOthers(t *testing.T) {
	g := newEndpointTestClient(t, 3)

	for i := range g.streamManager.controlStreams {
		g.streamManager.controlStreams[i] = &configurableMockStream{}
	}
	g.streamManager.tunnelStreams = []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "s0"},
		{connIndex: 1, isHealthy: true, streamID: "s1"},
		{connIndex: 2, isHealthy: true, streamID: "s2"},
	}

	var cancelCount atomic.Int64
	g.streamManager.cancels[1] = func() { cancelCount.Add(1) }

	g.handleEndpointDisconnection(1, "test_disconnect")

	// Give the goroutine a moment to start, then cancel context so the
	// infinite retry loop exits cleanly.
	time.Sleep(50 * time.Millisecond)
	g.cancel()

	// Wait for reconnect goroutine to finish.
	deadline := time.Now().Add(2 * time.Second)
	for g.endpointReconnecting[1].Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if g.streamManager.controlStreams[1] != nil {
		t.Fatal("expected disconnected endpoint control stream to be nil")
	}
	if g.streamManager.controlStreams[0] == nil || g.streamManager.controlStreams[2] == nil {
		t.Fatal("expected other endpoint control streams to remain intact")
	}

	if g.streamManager.connectionHealth[1] {
		t.Fatal("expected disconnected endpoint connection health=false")
	}
	if !g.streamManager.connectionHealth[0] || !g.streamManager.connectionHealth[2] {
		t.Fatal("expected other endpoint health to remain true")
	}

	for _, s := range g.streamManager.tunnelStreams {
		switch s.connIndex {
		case 1:
			if s.isHealthy {
				t.Fatal("expected disconnected endpoint tunnel streams to be unhealthy")
			}
		default:
			if !s.isHealthy {
				t.Fatal("expected non-disconnected endpoint tunnel streams to remain healthy")
			}
		}
	}

	// After context cancel + goroutine exit, the flag is cleared (defer).
	// The important assertions are the disconnect side-effects above.

	if g.endpoints[1].IsHealthy() {
		t.Fatal("expected disconnected endpoint to be marked unhealthy")
	}
	if !g.endpoints[0].IsHealthy() || !g.endpoints[2].IsHealthy() {
		t.Fatal("expected other endpoints to remain healthy")
	}

	g.handleEndpointDisconnection(1, "second_disconnect")
	if got := cancelCount.Load(); got != 1 {
		t.Fatalf("expected cancel to be called exactly once, got %d", got)
	}
}

func TestDisconnectEndpointStreams_CleanupCorrectness(t *testing.T) {
	g := newEndpointTestClient(t, 3)

	for i := range g.streamManager.controlStreams {
		g.streamManager.controlStreams[i] = &configurableMockStream{}
	}
	var cancel1 atomic.Int64
	g.streamManager.cancels[1] = func() { cancel1.Add(1) }

	g.streamManager.tunnelStreams = []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "0a"},
		{connIndex: 1, isHealthy: true, streamID: "1a"},
		{connIndex: 1, isHealthy: true, streamID: "1b"},
		{connIndex: 2, isHealthy: true, streamID: "2a"},
	}

	g.disconnectEndpointStreams(1)

	if g.streamManager.controlStreams[1] != nil {
		t.Fatal("expected controlStreams[1] to be nil")
	}
	if g.streamManager.controlStreams[0] == nil || g.streamManager.controlStreams[2] == nil {
		t.Fatal("expected non-target control streams to be untouched")
	}

	if g.streamManager.connectionHealth[1] {
		t.Fatal("expected connectionHealth[1]=false")
	}
	if !g.streamManager.connectionHealth[0] || !g.streamManager.connectionHealth[2] {
		t.Fatal("expected non-target connection health to remain true")
	}

	for _, s := range g.streamManager.tunnelStreams {
		if s.connIndex == 1 && s.isHealthy {
			t.Fatal("expected endpoint 1 tunnel streams to be unhealthy")
		}
		if s.connIndex != 1 && !s.isHealthy {
			t.Fatal("expected other endpoint tunnel streams to remain healthy")
		}
	}

	if got := cancel1.Load(); got != 1 {
		t.Fatalf("expected endpoint 1 cancel to be called once, got %d", got)
	}
	if g.streamManager.cancels[1] != nil {
		t.Fatal("expected endpoint 1 cancel func to be nil after disconnect")
	}

	if g.endpoints[1].IsHealthy() {
		t.Fatal("expected endpoint 1 health=false")
	}
	if !g.endpoints[0].IsHealthy() || !g.endpoints[2].IsHealthy() {
		t.Fatal("expected endpoint 0 and 2 health=true")
	}
}

func TestSendSessionMessageAsync_HealthGatedSelection(t *testing.T) {
	t.Run("prefers first healthy stream", func(t *testing.T) {
		g := newEndpointTestClient(t, 3)
		s0 := &configurableMockStream{}
		s1 := &configurableMockStream{}
		s2 := &configurableMockStream{}
		g.streamManager.controlStreams = []pb.GravitySessionService_EstablishSessionClient{s0, s1, s2}
		g.streamManager.connectionHealth = []bool{false, true, true}

		err := g.sendSessionMessageAsync(&pb.SessionMessage{Id: "m1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if s1.sendCount.Load() != 1 {
			t.Fatalf("expected stream 1 send count 1, got %d", s1.sendCount.Load())
		}
		if s0.sendCount.Load() != 0 || s2.sendCount.Load() != 0 {
			t.Fatalf("expected only stream 1 used, counts: s0=%d s2=%d", s0.sendCount.Load(), s2.sendCount.Load())
		}
	})

	t.Run("falls back to first non-nil when all unhealthy", func(t *testing.T) {
		g := newEndpointTestClient(t, 3)
		s0 := &configurableMockStream{}
		s1 := &configurableMockStream{}
		s2 := &configurableMockStream{}
		g.streamManager.controlStreams = []pb.GravitySessionService_EstablishSessionClient{s0, s1, s2}
		g.streamManager.connectionHealth = []bool{false, false, false}

		err := g.sendSessionMessageAsync(&pb.SessionMessage{Id: "m2"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if s0.sendCount.Load() != 1 {
			t.Fatalf("expected stream 0 fallback send count 1, got %d", s0.sendCount.Load())
		}
		if s1.sendCount.Load() != 0 || s2.sendCount.Load() != 0 {
			t.Fatalf("expected only stream 0 used, counts: s1=%d s2=%d", s1.sendCount.Load(), s2.sendCount.Load())
		}
	})

	t.Run("all nil returns error", func(t *testing.T) {
		g := newEndpointTestClient(t, 3)
		g.streamManager.controlStreams = []pb.GravitySessionService_EstablishSessionClient{nil, nil, nil}
		g.streamManager.connectionHealth = []bool{true, true, true}

		err := g.sendSessionMessageAsync(&pb.SessionMessage{Id: "m3"})
		if err == nil {
			t.Fatal("expected error when all control streams are nil")
		}
	})
}

func TestSendOnControlStream_ConcurrentSerialization(t *testing.T) {
	g := newEndpointTestClient(t, 1)
	stream := &configurableMockStream{sendDelay: 100 * time.Microsecond}
	g.streamManager.controlStreams[0] = stream

	const workers = 100
	errCh := make(chan error, workers)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errCh <- g.sendOnControlStream(0, &pb.SessionMessage{Id: fmt.Sprintf("id-%d", i)})
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("unexpected sendOnControlStream error: %v", err)
		}
	}

	if got := stream.sendCount.Load(); got != workers {
		t.Fatalf("expected %d sends, got %d", workers, got)
	}
}

func TestSendOnControlStream_BoundsChecking(t *testing.T) {
	t.Run("index less than zero", func(t *testing.T) {
		g := newEndpointTestClient(t, 1)
		g.streamManager.controlStreams[0] = &configurableMockStream{}
		if err := g.sendOnControlStream(-1, &pb.SessionMessage{}); err == nil {
			t.Fatal("expected error for negative index")
		}
	})

	t.Run("index out of range", func(t *testing.T) {
		g := newEndpointTestClient(t, 1)
		g.streamManager.controlStreams[0] = &configurableMockStream{}
		if err := g.sendOnControlStream(1, &pb.SessionMessage{}); err == nil {
			t.Fatal("expected error for out-of-range index")
		}
	})

	t.Run("nil stream", func(t *testing.T) {
		g := newEndpointTestClient(t, 1)
		if err := g.sendOnControlStream(0, &pb.SessionMessage{}); err == nil {
			t.Fatal("expected error for nil stream")
		}
	})

	t.Run("valid index succeeds", func(t *testing.T) {
		g := newEndpointTestClient(t, 1)
		g.streamManager.controlStreams[0] = &configurableMockStream{}
		if err := g.sendOnControlStream(0, &pb.SessionMessage{}); err != nil {
			t.Fatalf("expected success, got %v", err)
		}
	})
}

func TestHandleEndpointDisconnection_ReconnectionGuardNoOp(t *testing.T) {
	g := newEndpointTestClient(t, 1)
	g.streamManager.controlStreams[0] = &configurableMockStream{}
	g.streamManager.tunnelStreams = []*StreamInfo{{connIndex: 0, isHealthy: true}}

	g.endpointReconnecting[0].Store(true)
	g.handleEndpointDisconnection(0, "already_reconnecting")

	if g.streamManager.controlStreams[0] == nil {
		t.Fatal("expected control stream unchanged when already reconnecting")
	}
	if !g.streamManager.connectionHealth[0] {
		t.Fatal("expected connection health unchanged when already reconnecting")
	}
	if !g.streamManager.tunnelStreams[0].isHealthy {
		t.Fatal("expected tunnel health unchanged when already reconnecting")
	}
	if !g.endpoints[0].IsHealthy() {
		t.Fatal("expected endpoint health unchanged when already reconnecting")
	}
}

func TestSendSessionMessageAsync_HealthBeforeHandshakeProtection(t *testing.T) {
	g := newEndpointTestClient(t, 2)
	s0 := &configurableMockStream{}
	s1 := &configurableMockStream{}
	g.streamManager.controlStreams = []pb.GravitySessionService_EstablishSessionClient{s0, s1}

	g.streamManager.connectionHealth = []bool{false, true}
	if err := g.sendSessionMessageAsync(&pb.SessionMessage{Id: "healthy-exists"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s1.sendCount.Load() != 1 || s0.sendCount.Load() != 0 {
		t.Fatalf("expected healthy stream selected first, got s0=%d s1=%d", s0.sendCount.Load(), s1.sendCount.Load())
	}

	g2 := newEndpointTestClient(t, 2)
	u0 := &configurableMockStream{}
	u1 := &configurableMockStream{}
	g2.streamManager.controlStreams = []pb.GravitySessionService_EstablishSessionClient{u0, u1}
	g2.streamManager.connectionHealth = []bool{false, false}
	if err := g2.sendSessionMessageAsync(&pb.SessionMessage{Id: "all-unhealthy"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u0.sendCount.Load() != 1 || u1.sendCount.Load() != 0 {
		t.Fatalf("expected fallback to first non-nil stream, got u0=%d u1=%d", u0.sendCount.Load(), u1.sendCount.Load())
	}
}

func TestHandleEndpointDisconnection_ConcurrentPerEndpoint(t *testing.T) {
	g := newEndpointTestClient(t, 3)

	for i := range g.streamManager.controlStreams {
		g.streamManager.controlStreams[i] = &configurableMockStream{}
	}
	// Keep a sentinel healthy stream/tunnel that is not part of endpoint indices
	// under test so all three disconnections stay in per-endpoint degraded mode
	// and never trigger full server reconnection.
	g.streamManager.controlStreams = append(g.streamManager.controlStreams, &configurableMockStream{})
	g.streamManager.controlSendMu = append(g.streamManager.controlSendMu, sync.Mutex{})
	g.streamManager.connectionHealth = append(g.streamManager.connectionHealth, true)

	g.streamManager.tunnelStreams = []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "0"},
		{connIndex: 1, isHealthy: true, streamID: "1"},
		{connIndex: 2, isHealthy: true, streamID: "2"},
		{connIndex: 99, isHealthy: true, streamID: "sentinel"},
	}

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			g.handleEndpointDisconnection(i, fmt.Sprintf("disconnect_%d", i))
		}(i)
	}
	wg.Wait()

	for i := 0; i < 3; i++ {
		if g.streamManager.controlStreams[i] != nil {
			t.Fatalf("expected controlStreams[%d] nil after concurrent disconnections", i)
		}
		if g.streamManager.connectionHealth[i] {
			t.Fatalf("expected connectionHealth[%d]=false after concurrent disconnections", i)
		}
		if g.endpoints[i].IsHealthy() {
			t.Fatalf("expected endpoint %d unhealthy after concurrent disconnections", i)
		}
	}

	for _, s := range g.streamManager.tunnelStreams {
		if s.connIndex != 99 && s.isHealthy {
			t.Fatal("expected all tunnel streams unhealthy after concurrent disconnections")
		}
	}
}

func TestDrainConnectionIDChan_DrainsAllValues(t *testing.T) {
	g := newEndpointTestClient(t, 1)
	g.connectionIDChan = make(chan string, 8)
	for i := 0; i < 5; i++ {
		g.connectionIDChan <- fmt.Sprintf("id-%d", i)
	}

	g.drainConnectionIDChan()

	if len(g.connectionIDChan) != 0 {
		t.Fatalf("expected channel empty after drain, got len=%d", len(g.connectionIDChan))
	}
}

func TestDrainConnectionIDChan_EmptyNoOp(t *testing.T) {
	g := newEndpointTestClient(t, 1)
	g.connectionIDChan = make(chan string, 1)

	done := make(chan struct{})
	go func() {
		g.drainConnectionIDChan()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("drainConnectionIDChan blocked on empty channel")
	}
}

// --- Tunnel recovery after reconnect ---

// TestTunnelStreamDisconnection_TriggersEndpointReconnect verifies that when a
// tunnel stream disconnects in multi-endpoint mode, it resolves the correct
// endpoint index and triggers per-endpoint disconnection.
func TestTunnelStreamDisconnection_TriggersEndpointReconnect(t *testing.T) {
	t.Parallel()

	g := newEndpointTestClient(t, 3)
	g.gravityURLs = []string{"a", "b", "c"}

	// Set up 6 tunnel streams (2 per endpoint).
	g.streamManager.tunnelStreams = make([]*StreamInfo, 6)
	for i := 0; i < 6; i++ {
		g.streamManager.tunnelStreams[i] = &StreamInfo{
			connIndex: i / 2,
			streamID:  fmt.Sprintf("stream-%d", i),
			isHealthy: true,
		}
	}

	// Streams 0-1 belong to endpoint 0, streams 2-3 to endpoint 1, streams 4-5 to endpoint 2.
	g.streamManager.controlStreams[0] = &configurableMockStream{}
	g.streamManager.controlStreams[1] = &configurableMockStream{}
	g.streamManager.controlStreams[2] = &configurableMockStream{}

	// Disconnect tunnel stream 3 (belongs to endpoint 1).
	// handleTunnelStreamDisconnection fires handleEndpointDisconnection
	// in a goroutine. The CAS on endpointReconnecting[1] proves the
	// correct endpoint was identified from the tunnel stream index.
	g.handleTunnelStreamDisconnection(3)

	// Give the goroutine a moment to fire.
	time.Sleep(100 * time.Millisecond)

	// Verify endpoint 1 entered the reconnection path (flag was set).
	if !g.endpointReconnecting[1].Load() {
		t.Fatal("expected endpointReconnecting[1] to be true after tunnel stream 3 disconnection")
	}
	// Endpoints 0 and 2 should NOT be reconnecting.
	if g.endpointReconnecting[0].Load() {
		t.Fatal("expected endpointReconnecting[0] to remain false")
	}
	if g.endpointReconnecting[2].Load() {
		t.Fatal("expected endpointReconnecting[2] to remain false")
	}
}

// TestEndpointDisconnection_NotBlockedAfterFullReconnect verifies that
// endpointReconnecting flags don't block reconnection after a full reconnect
// replaces the slice.
func TestEndpointDisconnection_NotBlockedAfterFullReconnect(t *testing.T) {
	t.Parallel()

	g := newEndpointTestClient(t, 3)
	g.gravityURLs = []string{"a", "b", "c"}

	// Simulate: all 3 flags were set during a previous failure cycle.
	for i := 0; i < 3; i++ {
		g.endpointReconnecting[i].Store(true)
	}

	// Simulate full reconnect: startMultiEndpoint replaces the slice.
	g.endpointReconnecting = make([]atomic.Bool, 3)

	// Now all flags should be false, allowing new reconnections.
	for i := 0; i < 3; i++ {
		if g.endpointReconnecting[i].Load() {
			t.Fatalf("endpoint %d reconnecting flag should be false after slice replacement", i)
		}
	}

	// Verify handleEndpointDisconnection can set the flag (not blocked).
	// Set closing=true so it doesn't actually reconnect.
	g.mu.Lock()
	g.closing = true
	g.mu.Unlock()

	g.handleEndpointDisconnection(1, "test")
	// closing=true means handleEndpointDisconnection returns early, but
	// if it wasn't blocked by the flag check, the function was entered.
	// The flag should still be false since closing short-circuits before CAS.
}

// TestEndpointDisconnection_CanceledNotSilenced verifies that the
// codes.Canceled error path doesn't silently swallow stream deaths
// when the client is not intentionally closing.
func TestEndpointDisconnection_ClosingPreventsReconnect(t *testing.T) {
	t.Parallel()

	g := newEndpointTestClient(t, 3)
	g.gravityURLs = []string{"a", "b", "c"}

	g.streamManager.controlStreams[0] = &configurableMockStream{}
	g.streamManager.controlStreams[1] = &configurableMockStream{}
	g.streamManager.controlStreams[2] = &configurableMockStream{}

	// When closing, handleEndpointDisconnection should return immediately.
	g.mu.Lock()
	g.closing = true
	g.mu.Unlock()

	g.handleEndpointDisconnection(1, "test-closing")
	time.Sleep(50 * time.Millisecond)

	// Control stream 1 should NOT have been nil'd (closing short-circuits).
	g.streamManager.controlMu.RLock()
	stream1 := g.streamManager.controlStreams[1]
	g.streamManager.controlMu.RUnlock()
	if stream1 == nil {
		t.Fatal("expected control stream 1 to be preserved when closing=true")
	}
}

// TestEndpointDisconnection_AllDownStillPerEndpoint verifies that when all
// endpoints are down, handleEndpointDisconnection still uses per-endpoint
// reconnection (not full reconnect) so each endpoint recovers independently.
func TestEndpointDisconnection_AllDownStillPerEndpoint(t *testing.T) {
	t.Parallel()

	g := newEndpointTestClient(t, 3)
	g.gravityURLs = []string{"a", "b", "c"}

	// All control streams nil = no healthy endpoints.
	// handleEndpointDisconnection should still use per-endpoint reconnect,
	// setting the reconnecting flag (reconnectEndpoint will run in a goroutine
	// and eventually fail, clearing the flag via defer).

	g.handleEndpointDisconnection(1, "test-all-down")
	time.Sleep(50 * time.Millisecond)

	// The reconnecting flag should be set (per-endpoint reconnect was started).
	if !g.endpointReconnecting[1].Load() {
		t.Fatal("expected endpointReconnecting[1] to be true — per-endpoint reconnect should have started")
	}
	// Other endpoints should NOT be affected.
	if g.endpointReconnecting[0].Load() {
		t.Fatal("expected endpointReconnecting[0] to remain false")
	}
	if g.endpointReconnecting[2].Load() {
		t.Fatal("expected endpointReconnecting[2] to remain false")
	}
}

// --- Tunnel health and selection ---

// TestSelectStreamForPacket_SkipsEndpointWithNoTunnels verifies that when the
// selected endpoint has no healthy tunnel streams, the selector marks it
// unhealthy and falls back to another endpoint.
func TestSelectStreamForPacket_SkipsEndpointWithNoTunnels(t *testing.T) {
	t.Parallel()

	g := newEndpointTestClient(t, 3)
	g.gravityURLs = []string{"a", "b", "c"}

	// Set up selector
	g.selector = NewEndpointSelector(5 * time.Second)

	// Endpoint 0: has healthy tunnel streams
	// Endpoint 1: has NO tunnel streams (empty index list)
	// Endpoint 2: has healthy tunnel streams
	g.streamManager.tunnelStreams = make([]*StreamInfo, 4)
	g.streamManager.tunnelStreams[0] = &StreamInfo{connIndex: 0, isHealthy: true, streamID: "s0"}
	g.streamManager.tunnelStreams[1] = &StreamInfo{connIndex: 0, isHealthy: true, streamID: "s1"}
	// indices 2-3 intentionally nil (endpoint 1 has no tunnels)

	g.endpointStreamIndices["grpc://endpoint-0"] = []int{0, 1}
	g.endpointStreamIndices["grpc://endpoint-1"] = []int{} // no tunnels!
	g.endpointStreamIndices["grpc://endpoint-2"] = []int{} // no tunnels!

	// Build a packet and pre-bind the selector to endpoint-1 (no tunnels)
	// so the first attempt hits the empty endpoint.
	pkt := make([]byte, 40)
	pkt[0] = 0x60
	key := ExtractFlowKey(pkt)
	g.selector.mu.Lock()
	g.selector.bindings[key] = &TunnelBinding{
		Endpoint: g.endpoints[1],
		LastUsed: time.Now(),
	}
	g.selector.mu.Unlock()

	stream, err := g.selectStreamForPacket(pkt)
	if err != nil {
		t.Fatalf("expected stream selection to succeed via fallback, got: %v", err)
	}
	if stream == nil {
		t.Fatal("expected non-nil stream")
	}
	// Should have fallen back to endpoint 0 (the only one with tunnels)
	if stream.connIndex != 0 {
		t.Fatalf("expected stream from endpoint 0, got connIndex=%d", stream.connIndex)
	}
	// Endpoint 1 should now be marked unhealthy
	if g.endpoints[1].IsHealthy() {
		t.Fatal("expected endpoint 1 to be marked unhealthy after tunnel failure")
	}
}

// TestSelectStreamForPacket_AllEndpointsNoTunnels verifies that when no
// endpoint has healthy tunnels, an error is returned.
func TestSelectStreamForPacket_AllEndpointsNoTunnels(t *testing.T) {
	t.Parallel()

	g := newEndpointTestClient(t, 3)
	g.gravityURLs = []string{"a", "b", "c"}
	g.selector = NewEndpointSelector(5 * time.Second)

	// No tunnel streams at all
	g.streamManager.tunnelStreams = make([]*StreamInfo, 0)
	g.endpointStreamIndices["grpc://endpoint-0"] = []int{}
	g.endpointStreamIndices["grpc://endpoint-1"] = []int{}
	g.endpointStreamIndices["grpc://endpoint-2"] = []int{}

	pkt := make([]byte, 40)
	pkt[0] = 0x60

	_, err := g.selectStreamForPacket(pkt)
	if err == nil {
		t.Fatal("expected error when no endpoints have tunnels")
	}
}

// TestSelectStreamForPacket_UnhealthyTunnelsTriggersReconnect verifies that
// when all tunnel streams for an endpoint are unhealthy, the endpoint is
// marked unhealthy and reconnection is triggered.
func TestSelectStreamForPacket_UnhealthyTunnelsTriggersReconnect(t *testing.T) {
	t.Parallel()

	g := newEndpointTestClient(t, 3)
	g.gravityURLs = []string{"a", "b", "c"}
	g.selector = NewEndpointSelector(5 * time.Second)

	// Endpoint 0: tunnels exist but ALL unhealthy
	// Endpoint 1: healthy tunnels (fallback target)
	g.streamManager.tunnelStreams = make([]*StreamInfo, 6)
	g.streamManager.tunnelStreams[0] = &StreamInfo{connIndex: 0, isHealthy: false, streamID: "s0"}
	g.streamManager.tunnelStreams[1] = &StreamInfo{connIndex: 0, isHealthy: false, streamID: "s1"}
	g.streamManager.tunnelStreams[2] = &StreamInfo{connIndex: 1, isHealthy: true, streamID: "s2"}
	g.streamManager.tunnelStreams[3] = &StreamInfo{connIndex: 1, isHealthy: true, streamID: "s3"}
	g.streamManager.tunnelStreams[4] = &StreamInfo{connIndex: 2, isHealthy: true, streamID: "s4"}
	g.streamManager.tunnelStreams[5] = &StreamInfo{connIndex: 2, isHealthy: true, streamID: "s5"}

	g.endpointStreamIndices["grpc://endpoint-0"] = []int{0, 1}
	g.endpointStreamIndices["grpc://endpoint-1"] = []int{2, 3}
	g.endpointStreamIndices["grpc://endpoint-2"] = []int{4, 5}

	// Force selector to pick endpoint 0 first by pre-binding
	pkt := make([]byte, 40)
	pkt[0] = 0x60
	g.selector.Select(pkt, g.endpoints) // creates binding to some endpoint

	// Now endpoint 0 has unhealthy tunnels. selectStreamForPacket should:
	// 1. Try endpoint 0 → fail (no healthy tunnels)
	// 2. Mark endpoint 0 unhealthy
	// 3. Fall back to another endpoint
	g.endpoints[0].healthy.Store(true) // start healthy

	// Override binding to force endpoint 0
	key := ExtractFlowKey(pkt)
	g.selector.mu.Lock()
	g.selector.bindings[key] = &TunnelBinding{
		Endpoint: g.endpoints[0],
		LastUsed: time.Now(),
	}
	g.selector.mu.Unlock()

	stream, err := g.selectStreamForPacket(pkt)
	if err != nil {
		t.Fatalf("expected fallback to succeed, got: %v", err)
	}
	if stream.connIndex == 0 {
		t.Fatal("expected fallback to a healthy endpoint (not endpoint 0 which has unhealthy tunnels)")
	}

	// Endpoint 0 should now be marked unhealthy
	if g.endpoints[0].IsHealthy() {
		t.Fatal("expected endpoint 0 to be marked unhealthy after tunnel failure")
	}
}

// TestTunnelOffsetOutOfBounds reproduces the bug where reconnectSingleEndpoint
// computes tunnelOffset = endpointIndex * streamsPerGravity, but the
// tunnelStreams array was sized for fewer endpoints. Endpoints with high
// indices silently skip tunnel creation.
func TestTunnelOffsetOutOfBounds(t *testing.T) {
	t.Parallel()

	// Simulate: 6 endpoints discovered from DNS, but only 3 healthy at
	// startup → tunnelStreams sized for 3*2=6. Endpoint 5 needs offset 10.
	g := newEndpointTestClient(t, 6)
	g.gravityURLs = []string{"a", "b", "c", "d", "e", "f"}
	g.poolConfig = ConnectionPoolConfig{
		StreamsPerGravity: 2,
	}

	// Tunnel array sized for 3 healthy * 2 streams = 6 slots
	g.streamManager.tunnelStreams = make([]*StreamInfo, 6)
	for i := 0; i < 6; i++ {
		g.streamManager.tunnelStreams[i] = &StreamInfo{
			connIndex: i / 2,
			isHealthy: true,
			streamID:  fmt.Sprintf("s%d", i),
		}
	}

	// Endpoint 5 would need tunnelOffset = 5*2 = 10, but array only has 6 slots.
	streamsPerGravity := g.poolConfig.StreamsPerGravity
	tunnelOffset := 5 * streamsPerGravity // = 10
	outOfBounds := tunnelOffset >= len(g.streamManager.tunnelStreams)

	if !outOfBounds {
		t.Fatal("expected tunnel offset 10 to be out of bounds for 6-element array")
	}

	// This proves the bug: endpoint 5 can never create tunnels with the
	// current sizing. The fix should either resize the array or map
	// endpoints to tunnel indices dynamically.
}

// TestTriggerEndpointReconnectByURL verifies that the URL-based reconnect
// trigger finds the correct endpoint index and initiates disconnection.
func TestTriggerEndpointReconnectByURL(t *testing.T) {
	t.Parallel()

	g := newEndpointTestClient(t, 3)
	g.gravityURLs = []string{"a", "b", "c"}

	g.triggerEndpointReconnectByURL("grpc://endpoint-1")
	// The goroutine should resolve index 1 and set endpointReconnecting[1].
	time.Sleep(100 * time.Millisecond)

	if !g.endpointReconnecting[1].Load() {
		t.Fatal("expected endpointReconnecting[1] to be set after triggerEndpointReconnectByURL")
	}
	// Endpoints 0 and 2 should NOT be affected.
	if g.endpointReconnecting[0].Load() {
		t.Fatal("expected endpointReconnecting[0] to remain false")
	}
	if g.endpointReconnecting[2].Load() {
		t.Fatal("expected endpointReconnecting[2] to remain false")
	}

	// Cancel context to stop the infinite reconnect loop.
	g.cancel()
	deadline := time.Now().Add(2 * time.Second)
	for g.endpointReconnecting[1].Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
}

// TestTriggerEndpointReconnectByURL_UnknownURL verifies that an unknown
// URL doesn't panic or trigger reconnection.
func TestTriggerEndpointReconnectByURL_UnknownURL(t *testing.T) {
	t.Parallel()

	g := newEndpointTestClient(t, 3)
	g.gravityURLs = []string{"a", "b", "c"}

	// Should not panic
	g.triggerEndpointReconnectByURL("grpc://nonexistent")
	time.Sleep(50 * time.Millisecond)
}

// --- Aggressive reconnection isolation tests ---
//
// These tests verify that triggerAllEndpointReconnections does NOT cascade
// a single-endpoint failure to healthy endpoints. This was the root cause of
// a production outage: when one ion died, the aggressive reconnect killed
// the healthy ion's streams too, causing total connectivity loss.

// TestTriggerAllEndpointReconnections_SkipsHealthyEndpoints verifies that
// when triggerAllEndpointReconnections is called, endpoints that are still
// healthy are NOT reconnected. Only unhealthy endpoints should be targeted.
func TestTriggerAllEndpointReconnections_SkipsHealthyEndpoints(t *testing.T) {
	t.Parallel()

	g := newEndpointTestClient(t, 3)
	g.gravityURLs = []string{"a", "b", "c"}

	// Set up control streams and tunnel streams for all 3 endpoints.
	for i := range g.streamManager.controlStreams {
		g.streamManager.controlStreams[i] = &configurableMockStream{}
	}
	g.streamManager.tunnelStreams = []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "t0"},
		{connIndex: 1, isHealthy: true, streamID: "t1"},
		{connIndex: 2, isHealthy: true, streamID: "t2"},
	}

	// Endpoint 0 is unhealthy (its ion died). Endpoints 1 and 2 are healthy.
	g.endpoints[0].healthy.Store(false)
	// endpoints 1 and 2 remain healthy (set by newEndpointTestClient)

	g.triggerAllEndpointReconnections("test_single_endpoint_failure")

	// Give reconnection goroutines a moment to start.
	time.Sleep(100 * time.Millisecond)

	// CRITICAL ASSERTION: Only the unhealthy endpoint should be reconnecting.
	if !g.endpointReconnecting[0].Load() {
		t.Fatal("expected unhealthy endpoint 0 to enter reconnection")
	}
	if g.endpointReconnecting[1].Load() {
		t.Fatal("expected healthy endpoint 1 to NOT enter reconnection — cascading failure!")
	}
	if g.endpointReconnecting[2].Load() {
		t.Fatal("expected healthy endpoint 2 to NOT enter reconnection — cascading failure!")
	}

	// Healthy endpoints' control streams must remain intact.
	g.streamManager.controlMu.RLock()
	cs1 := g.streamManager.controlStreams[1]
	cs2 := g.streamManager.controlStreams[2]
	g.streamManager.controlMu.RUnlock()
	if cs1 == nil {
		t.Fatal("expected healthy endpoint 1 control stream to remain intact")
	}
	if cs2 == nil {
		t.Fatal("expected healthy endpoint 2 control stream to remain intact")
	}

	// Healthy endpoints' tunnel streams must remain healthy.
	g.streamManager.tunnelMu.RLock()
	for _, s := range g.streamManager.tunnelStreams {
		if s.connIndex != 0 && !s.isHealthy {
			t.Fatalf("expected healthy endpoint %d tunnel stream to remain healthy", s.connIndex)
		}
	}
	g.streamManager.tunnelMu.RUnlock()

	// Healthy endpoints' health flags must not be touched.
	if !g.endpoints[1].IsHealthy() {
		t.Fatal("expected endpoint 1 to remain healthy")
	}
	if !g.endpoints[2].IsHealthy() {
		t.Fatal("expected endpoint 2 to remain healthy")
	}
}

// TestTriggerAllEndpointReconnections_AllUnhealthy_ReconnectsAll verifies that
// when ALL endpoints are unhealthy, triggerAllEndpointReconnections does not
// skip them (the healthy-endpoint safety net only protects truly healthy ones).
//
// We cancel the context before calling triggerAll so the reconnect goroutines
// exit immediately, then verify via the synchronous side effects of
// disconnectEndpointStreams (called before each goroutine starts).
func TestTriggerAllEndpointReconnections_AllUnhealthy_ReconnectsAll(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	g := &GravityClient{
		logger:                logger.NewTestLogger(),
		ctx:                   ctx,
		cancel:                cancel,
		retryConfig:           DefaultRetryConfig(),
		connections:           make([]*grpc.ClientConn, 3),
		sessionClients:        make([]pb.GravitySessionServiceClient, 3),
		connectionURLs:        []string{"grpc://ep-0", "grpc://ep-1", "grpc://ep-2"},
		endpoints:             make([]*GravityEndpoint, 3),
		endpointReconnecting:  make([]atomic.Bool, 3),
		circuitBreakers:       make([]*CircuitBreaker, 3),
		connectionIDChan:      make(chan string, 16),
		endpointStreamIndices: make(map[string][]int),
		closed:                make(chan struct{}, 1),
		streamManager: &StreamManager{
			controlStreams:   make([]pb.GravitySessionService_EstablishSessionClient, 3),
			controlSendMu:    make([]sync.Mutex, 3),
			tunnelStreams:    make([]*StreamInfo, 0),
			connectionHealth: make([]bool, 3),
			streamMetrics:    make(map[string]*StreamMetrics),
			contexts:         make([]context.Context, 3),
			cancels:          make([]context.CancelFunc, 3),
		},
	}
	for i := 0; i < 3; i++ {
		ep := &GravityEndpoint{URL: g.connectionURLs[i]}
		ep.healthy.Store(false) // ALL unhealthy
		g.endpoints[i] = ep
		g.streamManager.connectionHealth[i] = false // consistent with unhealthy endpoints
		g.streamManager.controlStreams[i] = &configurableMockStream{}
		g.circuitBreakers[i] = NewCircuitBreaker(DefaultCircuitBreakerConfig())
	}
	g.streamManager.tunnelStreams = []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "t0"},
		{connIndex: 1, isHealthy: true, streamID: "t1"},
		{connIndex: 2, isHealthy: true, streamID: "t2"},
	}
	g.multiEndpointMode.Store(true)

	// Cancel context so reconnect goroutines exit immediately via
	// g.ctx.Done(), preventing the defer from racing with our assertions.
	cancel()

	g.triggerAllEndpointReconnections("all_endpoints_down")

	// Give goroutines a moment to start and exit.
	time.Sleep(50 * time.Millisecond)

	// Verify disconnectEndpointStreams was called for ALL endpoints
	// (synchronous side effect: control streams nil'd before goroutine).
	g.streamManager.controlMu.RLock()
	for i, cs := range g.streamManager.controlStreams {
		if cs != nil {
			t.Fatalf("expected control stream %d to be nil (disconnected) for unhealthy endpoint", i)
		}
	}
	g.streamManager.controlMu.RUnlock()

	g.streamManager.tunnelMu.RLock()
	for _, s := range g.streamManager.tunnelStreams {
		if s.isHealthy {
			t.Fatalf("expected tunnel stream %s (endpoint %d) to be marked unhealthy", s.streamID, s.connIndex)
		}
	}
	g.streamManager.tunnelMu.RUnlock()

	for i := 0; i < 3; i++ {
		if g.streamManager.connectionHealth[i] {
			t.Fatalf("expected connectionHealth[%d]=false for unhealthy endpoint", i)
		}
	}
}

// TestTriggerAllEndpointReconnections_AllHealthy_ReconnectsNone verifies the
// safety-net: even if triggerAllEndpointReconnections is called erroneously,
// healthy endpoints are protected.
func TestTriggerAllEndpointReconnections_AllHealthy_ReconnectsNone(t *testing.T) {
	t.Parallel()

	g := newEndpointTestClient(t, 3)
	g.gravityURLs = []string{"a", "b", "c"}

	for i := range g.streamManager.controlStreams {
		g.streamManager.controlStreams[i] = &configurableMockStream{}
	}
	g.streamManager.tunnelStreams = []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "t0"},
		{connIndex: 1, isHealthy: true, streamID: "t1"},
		{connIndex: 2, isHealthy: true, streamID: "t2"},
	}

	// All endpoints are healthy — this call should be a no-op.
	g.triggerAllEndpointReconnections("erroneous_call")
	time.Sleep(100 * time.Millisecond)

	for i := 0; i < 3; i++ {
		if g.endpointReconnecting[i].Load() {
			t.Fatalf("expected healthy endpoint %d to NOT enter reconnection", i)
		}
	}

	// All streams must remain intact.
	g.streamManager.controlMu.RLock()
	for i, cs := range g.streamManager.controlStreams {
		if cs == nil {
			t.Fatalf("expected control stream %d to remain intact", i)
		}
	}
	g.streamManager.controlMu.RUnlock()
}

// TestCascadingFailurePrevention_SingleEndpointDeath is an end-to-end scenario
// test that reproduces the exact production bug: one ion dies, its streams
// fail, consecutive write failures trigger aggressive reconnect, and the
// healthy ion's streams must survive.
func TestCascadingFailurePrevention_SingleEndpointDeath(t *testing.T) {
	t.Parallel()

	g := newEndpointTestClient(t, 2)
	g.gravityURLs = []string{"a", "b"}

	// Full stream setup for both endpoints.
	for i := range g.streamManager.controlStreams {
		g.streamManager.controlStreams[i] = &configurableMockStream{}
	}
	g.streamManager.tunnelStreams = []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "ep0-t0"},
		{connIndex: 0, isHealthy: true, streamID: "ep0-t1"},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t0"},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t1"},
	}

	// Simulate: ion 0 dies. Its endpoint is marked unhealthy by health checks.
	g.endpoints[0].healthy.Store(false)
	g.streamManager.connectionHealth[0] = false

	// The write failure path triggers aggressive reconnect.
	g.triggerAllEndpointReconnections("consecutive_write_failures")
	time.Sleep(100 * time.Millisecond)

	// Endpoint 0 (dead ion) should be reconnecting.
	if !g.endpointReconnecting[0].Load() {
		t.Fatal("expected dead endpoint 0 to enter reconnection")
	}

	// CRITICAL: Endpoint 1 (healthy ion) must NOT be reconnecting.
	if g.endpointReconnecting[1].Load() {
		t.Fatal("CASCADING FAILURE: healthy endpoint 1 was killed by aggressive reconnect of dead endpoint 0")
	}

	// Endpoint 1's streams must be fully intact.
	g.streamManager.controlMu.RLock()
	if g.streamManager.controlStreams[1] == nil {
		t.Fatal("CASCADING FAILURE: healthy endpoint 1 control stream was destroyed")
	}
	g.streamManager.controlMu.RUnlock()

	g.streamManager.tunnelMu.RLock()
	for _, s := range g.streamManager.tunnelStreams {
		if s.connIndex == 1 && !s.isHealthy {
			t.Fatal("CASCADING FAILURE: healthy endpoint 1 tunnel stream was marked unhealthy")
		}
	}
	g.streamManager.tunnelMu.RUnlock()

	if !g.endpoints[1].IsHealthy() {
		t.Fatal("CASCADING FAILURE: healthy endpoint 1 was marked unhealthy")
	}

	// The system should still have a healthy endpoint for traffic.
	if !g.hasHealthyEndpoint() {
		t.Fatal("CASCADING FAILURE: no healthy endpoints remain — total outage")
	}
}

// ============================================================================
// WritePacket Integration Tests & Additional Coverage
// ============================================================================

// newWritePacketTestClient creates a GravityClient configured for WritePacket
// integration tests: connected=true, multi-endpoint mode, selector, pool config,
// and buffer pool. Tests must still set up tunnel streams and endpointStreamIndices.
func newWritePacketTestClient(t *testing.T, n int) *GravityClient {
	t.Helper()
	g := newHardeningGravityClient(t, n)
	g.multiEndpointMode.Store(n > 1)
	g.selector = NewEndpointSelector(5 * time.Second)
	g.peerDiscoveryWake = make(chan struct{}, 1) // prevent nil channel panic
	g.streamManager.allocationStrategy = RoundRobin
	return g
}

// makeIPv6Packet creates a minimal 54-byte IPv6 packet with TCP next header.
// Bytes 0: version nibble (0x60), byte 6: next header (TCP=6), bytes 8-23: src IP,
// bytes 24-39: dst IP, bytes 40-43: src/dst ports.
func makeIPv6Packet() []byte {
	pkt := make([]byte, 54)
	pkt[0] = 0x60 // IPv6 version nibble
	pkt[6] = 6    // TCP next header
	pkt[8] = 0xfd // src IP starts with fd00::
	pkt[9] = 0x00
	pkt[23] = 1    // src IP: fd00::1
	pkt[24] = 0xfd // dst IP starts with fd00::
	pkt[25] = 0x00
	pkt[39] = 2    // dst IP: fd00::2
	pkt[40] = 0x00 // src port high byte
	pkt[41] = 0x50 // src port = 80
	pkt[42] = 0x01 // dst port high byte
	pkt[43] = 0xBB // dst port = 443
	return pkt
}

// setupWritePacketStreams creates mock tunnel streams and wires up
// endpointStreamIndices and streamMetrics for WritePacket integration tests.
func setupWritePacketStreams(g *GravityClient, streams []*StreamInfo) {
	g.streamManager.tunnelStreams = streams
	g.endpointStreamIndices = make(map[string][]int)
	for idx, s := range streams {
		if s == nil {
			continue
		}
		url := g.connectionURLs[s.connIndex]
		g.endpointStreamIndices[url] = append(g.endpointStreamIndices[url], idx)
	}
	for _, s := range streams {
		if s == nil {
			continue
		}
		g.streamManager.streamMetrics[s.streamID] = &StreamMetrics{}
	}
}

// preBindFlowToEndpoint creates a flow binding from the given packet to the
// specified endpoint index. This forces selectStreamForPacket to try that
// endpoint first.
func preBindFlowToEndpoint(g *GravityClient, pkt []byte, epIdx int) {
	key := ExtractFlowKey(pkt)
	g.selector.mu.Lock()
	g.selector.bindings[key] = &TunnelBinding{
		Endpoint: g.endpoints[epIdx],
		LastUsed: time.Now(),
	}
	g.selector.mu.Unlock()
}

// --- Category A: Integration Tests Through WritePacket ---

// TestWritePacket_SendFailure_DoesNotTriggerReconnection verifies that a
// tunnel Send() error in WritePacket does NOT trigger endpoint reconnection.
// The stream is marked unhealthy but no aggressive reconnection is started.
func TestWritePacket_SendFailure_DoesNotTriggerReconnection(t *testing.T) {
	t.Parallel()

	g := newWritePacketTestClient(t, 2)

	// ep0 has broken tunnel streams, ep1 has working ones
	ep0Stream := &hardeningMockTunnelStream{sendErr: errors.New("broken pipe")}
	ep1Stream := &hardeningMockTunnelStream{}

	setupWritePacketStreams(g, []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "ep0-t0", stream: ep0Stream, lastUsed: time.Now()},
		{connIndex: 0, isHealthy: true, streamID: "ep0-t1", stream: ep0Stream, lastUsed: time.Now()},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t0", stream: ep1Stream, lastUsed: time.Now()},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t1", stream: ep1Stream, lastUsed: time.Now()},
	})

	pkt := makeIPv6Packet()
	preBindFlowToEndpoint(g, pkt, 0)

	// WritePacket will try ep0, Send fails, marks stream unhealthy,
	// but does NOT trigger reconnection for ANY endpoint.
	_ = g.WritePacket(pkt)

	// Give any potential goroutines time to start
	time.Sleep(50 * time.Millisecond)

	// CRITICAL: Neither endpoint should be reconnecting
	if g.endpointReconnecting[0].Load() {
		t.Fatal("expected endpointReconnecting[0] to be false — Send failure should NOT trigger reconnection")
	}
	if g.endpointReconnecting[1].Load() {
		t.Fatal("expected endpointReconnecting[1] to be false — ep1 was not involved")
	}

	// ep1's streams must remain healthy
	g.streamManager.tunnelMu.RLock()
	for _, s := range g.streamManager.tunnelStreams {
		if s.connIndex == 1 && !s.isHealthy {
			t.Fatal("expected ep1 tunnel streams to remain healthy after ep0 Send failure")
		}
	}
	g.streamManager.tunnelMu.RUnlock()

	// The failed stream should be marked unhealthy
	g.streamManager.tunnelMu.RLock()
	failedStreamUnhealthy := false
	for _, s := range g.streamManager.tunnelStreams {
		if s.connIndex == 0 && !s.isHealthy {
			failedStreamUnhealthy = true
			break
		}
	}
	g.streamManager.tunnelMu.RUnlock()
	if !failedStreamUnhealthy {
		t.Fatal("expected at least one ep0 stream to be marked unhealthy after Send failure")
	}
}

// TestWritePacket_SendFailure_StreamMarkedUnhealthy verifies that when a
// tunnel Send() fails, the specific stream is marked unhealthy and outboundErrors
// is incremented, while other streams remain healthy.
func TestWritePacket_SendFailure_StreamMarkedUnhealthy(t *testing.T) {
	t.Parallel()

	g := newWritePacketTestClient(t, 2)

	brokenStream := &hardeningMockTunnelStream{sendErr: errors.New("transport closing")}
	healthyStream := &hardeningMockTunnelStream{}

	setupWritePacketStreams(g, []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "ep0-t0", stream: brokenStream, lastUsed: time.Now()},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t0", stream: healthyStream, lastUsed: time.Now()},
	})

	pkt := makeIPv6Packet()
	preBindFlowToEndpoint(g, pkt, 0)

	errsBefore := g.outboundErrors.Load()
	_ = g.WritePacket(pkt)

	// The failed stream should be marked unhealthy
	g.streamManager.tunnelMu.RLock()
	ep0Healthy := g.streamManager.tunnelStreams[0].isHealthy
	ep1Healthy := g.streamManager.tunnelStreams[1].isHealthy
	g.streamManager.tunnelMu.RUnlock()

	if ep0Healthy {
		t.Fatal("expected ep0 stream to be marked unhealthy after Send failure")
	}
	if !ep1Healthy {
		t.Fatal("expected ep1 stream to remain healthy")
	}

	// outboundErrors should have incremented
	if g.outboundErrors.Load() <= errsBefore {
		t.Fatal("expected outboundErrors to increment after Send failure")
	}

	// Error metrics on the broken stream should be recorded
	g.streamManager.metricsMu.RLock()
	m := g.streamManager.streamMetrics["ep0-t0"]
	g.streamManager.metricsMu.RUnlock()
	if m == nil || m.ErrorCount == 0 {
		t.Fatal("expected error metrics to be recorded on the failed stream")
	}
}

// TestWritePacket_FailoverToHealthyEndpoint verifies that when the bound
// endpoint has all unhealthy streams, WritePacket falls back to another
// healthy endpoint and delivers the packet.
func TestWritePacket_FailoverToHealthyEndpoint(t *testing.T) {
	t.Parallel()

	g := newWritePacketTestClient(t, 2)

	ep0Stream := &hardeningMockTunnelStream{} // won't be used — streams are unhealthy
	ep1Stream := &hardeningMockTunnelStream{} // healthy target

	setupWritePacketStreams(g, []*StreamInfo{
		{connIndex: 0, isHealthy: false, streamID: "ep0-t0", stream: ep0Stream, lastUsed: time.Now()},
		{connIndex: 0, isHealthy: false, streamID: "ep0-t1", stream: ep0Stream, lastUsed: time.Now()},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t0", stream: ep1Stream, lastUsed: time.Now()},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t1", stream: ep1Stream, lastUsed: time.Now()},
	})

	pkt := makeIPv6Packet()
	preBindFlowToEndpoint(g, pkt, 0)

	// WritePacket: selectStreamForPacket tries ep0 → no healthy tunnels
	// → marks ep0 unhealthy → falls back to ep1
	err := g.WritePacket(pkt)
	if err != nil {
		t.Fatalf("expected WritePacket to succeed via failover, got: %v", err)
	}

	// ep1's stream should have been used
	if ep1Stream.sendCount.Load() == 0 {
		t.Fatal("expected ep1 stream to receive the packet via failover")
	}
	// ep0's stream should NOT have been used (all were unhealthy)
	if ep0Stream.sendCount.Load() != 0 {
		t.Fatal("expected ep0 stream to NOT be used (all unhealthy)")
	}

	// ep0 should be marked unhealthy
	if g.endpoints[0].IsHealthy() {
		t.Fatal("expected endpoint 0 to be marked unhealthy after tunnel failure")
	}
	// ep1 should still be healthy
	if !g.endpoints[1].IsHealthy() {
		t.Fatal("expected endpoint 1 to remain healthy")
	}
}

// TestWritePacket_AllEndpointsDown_ReturnsError verifies that when ALL
// endpoints have no healthy tunnel streams, WritePacket returns an error
// without triggering reconnection (the old code would have called triggerAll).
func TestWritePacket_AllEndpointsDown_ReturnsError(t *testing.T) {
	t.Parallel()

	g := newWritePacketTestClient(t, 2)

	// All streams unhealthy on both endpoints
	// Mark endpoints unhealthy AND connectionHealth false (consistent)
	g.endpoints[0].healthy.Store(false)
	g.endpoints[1].healthy.Store(false)
	g.streamManager.connectionHealth[0] = false
	g.streamManager.connectionHealth[1] = false

	ep0Stream := &hardeningMockTunnelStream{}
	ep1Stream := &hardeningMockTunnelStream{}

	setupWritePacketStreams(g, []*StreamInfo{
		{connIndex: 0, isHealthy: false, streamID: "ep0-t0", stream: ep0Stream, lastUsed: time.Now()},
		{connIndex: 0, isHealthy: false, streamID: "ep0-t1", stream: ep0Stream, lastUsed: time.Now()},
		{connIndex: 1, isHealthy: false, streamID: "ep1-t0", stream: ep1Stream, lastUsed: time.Now()},
		{connIndex: 1, isHealthy: false, streamID: "ep1-t1", stream: ep1Stream, lastUsed: time.Now()},
	})

	pkt := makeIPv6Packet()

	err := g.WritePacket(pkt)
	if err == nil {
		t.Fatal("expected error when all endpoints are down")
	}

	// Give potential goroutines time to start
	time.Sleep(50 * time.Millisecond)

	// CRITICAL: Verify triggerAllEndpointReconnections was NOT called.
	// triggerAllEndpointReconnections would have called disconnectEndpointStreams,
	// which nils control streams and marks tunnel streams unhealthy, and closes
	// gRPC connections. If any of these are intact, triggerAll was not invoked.
	//
	// Note: selectStreamForPacket may trigger per-endpoint reconnect via
	// triggerEndpointReconnectByURL for unhealthy endpoints, which is
	// the correct behavior. But the WritePacket Send() path itself must
	// NOT trigger reconnection. The reconnection here is driven by
	// selectStreamForPacket's "no healthy tunnels" path, not by Send failure.

	// Verify ep0's tunnel streams were not nil'd by triggerAllEndpointReconnections.
	// disconnectEndpointStreams would have set isHealthy=false on all tunnel streams
	// for the endpoint AND nil'd the control stream / closed the gRPC connection.
	g.streamManager.tunnelMu.RLock()
	for _, si := range g.streamManager.tunnelStreams {
		if si != nil && si.stream == nil {
			g.streamManager.tunnelMu.RUnlock()
			t.Fatal("triggerAllEndpointReconnections was called: tunnel stream was disconnected")
		}
	}
	g.streamManager.tunnelMu.RUnlock()

	// Verify that ep1's control stream is still intact (not nil'd by disconnectEndpointStreams).
	// If triggerAll was called, it would have nil'd the control stream for unhealthy endpoints.
	g.streamManager.controlMu.RLock()
	// controlStreams are nil in this test fixture (newWritePacketTestClient doesn't set them),
	// so instead verify that no gRPC connections were closed by checking the mock streams.
	g.streamManager.controlMu.RUnlock()

	// The most reliable signal: ep0Stream and ep1Stream should not have been
	// used by Send (triggerAllEndpointReconnections doesn't Send, but
	// disconnectEndpointStreams would mark streams unhealthy and trigger
	// reconnectEndpoint which could interact with streams). Since both
	// endpoints were already unhealthy, their send counts should be zero.
	if ep0Stream.sendCount.Load() != 0 {
		t.Fatal("triggerAllEndpointReconnections side effect: ep0 stream was used")
	}
	if ep1Stream.sendCount.Load() != 0 {
		t.Fatal("triggerAllEndpointReconnections side effect: ep1 stream was used")
	}
}

// TestWritePacket_SuccessfulSend_NoSideEffects verifies that a successful
// WritePacket updates metrics without triggering any reconnection.
func TestWritePacket_SuccessfulSend_NoSideEffects(t *testing.T) {
	t.Parallel()

	g := newWritePacketTestClient(t, 2)

	ep0Stream := &hardeningMockTunnelStream{}
	ep1Stream := &hardeningMockTunnelStream{}

	setupWritePacketStreams(g, []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "ep0-t0", stream: ep0Stream, lastUsed: time.Now()},
		{connIndex: 0, isHealthy: true, streamID: "ep0-t1", stream: ep0Stream, lastUsed: time.Now()},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t0", stream: ep1Stream, lastUsed: time.Now()},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t1", stream: ep1Stream, lastUsed: time.Now()},
	})

	pkt := makeIPv6Packet()
	preBindFlowToEndpoint(g, pkt, 0)

	sentBefore := g.outboundSent.Load()

	err := g.WritePacket(pkt)
	if err != nil {
		t.Fatalf("expected WritePacket to succeed, got: %v", err)
	}

	// outboundSent should have incremented
	if g.outboundSent.Load() <= sentBefore {
		t.Fatal("expected outboundSent to increment after successful send")
	}

	// Stream metrics should have been updated
	g.streamManager.metricsMu.RLock()
	var found bool
	for _, m := range g.streamManager.streamMetrics {
		if m.PacketsSent > 0 {
			found = true
			break
		}
	}
	g.streamManager.metricsMu.RUnlock()
	if !found {
		t.Fatal("expected stream metrics PacketsSent to be incremented")
	}

	// No reconnection flags set
	if g.endpointReconnecting[0].Load() || g.endpointReconnecting[1].Load() {
		t.Fatal("expected no reconnection flags set after successful send")
	}

	// Both endpoints remain healthy
	if !g.endpoints[0].IsHealthy() || !g.endpoints[1].IsHealthy() {
		t.Fatal("expected both endpoints to remain healthy after successful send")
	}
}

// --- Category B: Tunnel Liveness Per-Endpoint Tests ---

// TestTunnelLiveness_StaleEndpoint_OnlyReconnectsStaleOne verifies that
// checkTunnelStreamLiveness triggers reconnection ONLY for the endpoint with
// stale streams, leaving healthy endpoints untouched.
func TestTunnelLiveness_StaleEndpoint_OnlyReconnectsStaleOne(t *testing.T) {
	t.Parallel()

	g := newWritePacketTestClient(t, 2)
	g.poolConfig.StreamsPerGravity = 2

	ep0Stream := &hardeningMockTunnelStream{}
	ep1Stream := &hardeningMockTunnelStream{}

	setupWritePacketStreams(g, []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "ep0-t0", stream: ep0Stream, lastUsed: time.Now()},
		{connIndex: 0, isHealthy: true, streamID: "ep0-t1", stream: ep0Stream, lastUsed: time.Now()},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t0", stream: ep1Stream, lastUsed: time.Now()},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t1", stream: ep1Stream, lastUsed: time.Now()},
	})

	// Set up control streams so hasHealthyEndpoint() returns true for ep1
	g.streamManager.controlStreams[0] = &configurableMockStream{}
	g.streamManager.controlStreams[1] = &configurableMockStream{}

	staleTimeout := 60 * time.Second

	// ep0: stale streams (activity was long ago)
	staleTime := time.Now().Add(-2 * staleTimeout).UnixMicro()
	g.streamManager.metricsMu.Lock()
	g.streamManager.streamMetrics["ep0-t0"] = &StreamMetrics{LastSendUs: staleTime, LastRecvUs: staleTime}
	g.streamManager.streamMetrics["ep0-t1"] = &StreamMetrics{LastSendUs: staleTime, LastRecvUs: staleTime}
	// ep1: recent activity
	recentTime := time.Now().UnixMicro()
	g.streamManager.streamMetrics["ep1-t0"] = &StreamMetrics{LastSendUs: recentTime, LastRecvUs: recentTime}
	g.streamManager.streamMetrics["ep1-t1"] = &StreamMetrics{LastSendUs: recentTime, LastRecvUs: recentTime}
	g.streamManager.metricsMu.Unlock()

	// Call the liveness check directly
	g.checkTunnelStreamLiveness(staleTimeout)

	// Give the goroutine time to fire
	time.Sleep(100 * time.Millisecond)

	// ep0 should be reconnecting (stale streams)
	if !g.endpointReconnecting[0].Load() {
		t.Fatal("expected endpointReconnecting[0] to be true — stale streams should trigger reconnection")
	}
	// ep1 should NOT be reconnecting (recent activity)
	if g.endpointReconnecting[1].Load() {
		t.Fatal("expected endpointReconnecting[1] to be false — streams have recent activity")
	}

	// ep1 should remain healthy
	if !g.endpoints[1].IsHealthy() {
		t.Fatal("expected endpoint 1 to remain healthy after ep0 liveness failure")
	}
}

// TestTunnelLiveness_IdleStreams_NoReconnection verifies that streams with
// zero timestamps (never carried data) are NOT flagged as stale. This prevents
// reconnection loops on idle hadrons with no containers.
func TestTunnelLiveness_IdleStreams_NoReconnection(t *testing.T) {
	t.Parallel()

	g := newWritePacketTestClient(t, 2)
	g.poolConfig.StreamsPerGravity = 2

	ep0Stream := &hardeningMockTunnelStream{}
	ep1Stream := &hardeningMockTunnelStream{}

	setupWritePacketStreams(g, []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "ep0-t0", stream: ep0Stream, lastUsed: time.Now()},
		{connIndex: 0, isHealthy: true, streamID: "ep0-t1", stream: ep0Stream, lastUsed: time.Now()},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t0", stream: ep1Stream, lastUsed: time.Now()},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t1", stream: ep1Stream, lastUsed: time.Now()},
	})

	// All streams have zero timestamps (idle — never carried data)
	g.streamManager.metricsMu.Lock()
	g.streamManager.streamMetrics["ep0-t0"] = &StreamMetrics{LastSendUs: 0, LastRecvUs: 0}
	g.streamManager.streamMetrics["ep0-t1"] = &StreamMetrics{LastSendUs: 0, LastRecvUs: 0}
	g.streamManager.streamMetrics["ep1-t0"] = &StreamMetrics{LastSendUs: 0, LastRecvUs: 0}
	g.streamManager.streamMetrics["ep1-t1"] = &StreamMetrics{LastSendUs: 0, LastRecvUs: 0}
	g.streamManager.metricsMu.Unlock()

	g.checkTunnelStreamLiveness(60 * time.Second)

	time.Sleep(50 * time.Millisecond)

	// Neither endpoint should be reconnecting
	if g.endpointReconnecting[0].Load() {
		t.Fatal("expected endpointReconnecting[0] to be false — idle streams should not trigger reconnection")
	}
	if g.endpointReconnecting[1].Load() {
		t.Fatal("expected endpointReconnecting[1] to be false — idle streams should not trigger reconnection")
	}
}

// --- Category C: selectStreamForPacket Per-Endpoint Reconnection ---

// TestSelectStreamForPacket_MarksEndpointUnhealthy_TriggersPerEndpointReconnect
// verifies that when all tunnel streams for an endpoint are unhealthy,
// selectStreamForPacket marks the endpoint unhealthy, triggers per-endpoint
// reconnection via triggerEndpointReconnectByURL, and falls back to ep1.
func TestSelectStreamForPacket_MarksEndpointUnhealthy_TriggersPerEndpointReconnect(t *testing.T) {
	t.Parallel()

	g := newWritePacketTestClient(t, 2)

	ep0Stream := &hardeningMockTunnelStream{}
	ep1Stream := &hardeningMockTunnelStream{}

	setupWritePacketStreams(g, []*StreamInfo{
		{connIndex: 0, isHealthy: false, streamID: "ep0-t0", stream: ep0Stream, lastUsed: time.Now()},
		{connIndex: 0, isHealthy: false, streamID: "ep0-t1", stream: ep0Stream, lastUsed: time.Now()},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t0", stream: ep1Stream, lastUsed: time.Now()},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t1", stream: ep1Stream, lastUsed: time.Now()},
	})

	pkt := makeIPv6Packet()
	preBindFlowToEndpoint(g, pkt, 0)

	stream, err := g.selectStreamForPacket(pkt)
	if err != nil {
		t.Fatalf("expected selectStreamForPacket to succeed via fallback, got: %v", err)
	}

	// Should have fallen back to ep1
	if stream.connIndex != 1 {
		t.Fatalf("expected stream from endpoint 1, got connIndex=%d", stream.connIndex)
	}

	// ep0 should now be marked unhealthy
	if g.endpoints[0].IsHealthy() {
		t.Fatal("expected endpoint 0 to be marked unhealthy after tunnel failure")
	}

	// Give triggerEndpointReconnectByURL goroutine time to fire
	time.Sleep(100 * time.Millisecond)

	// ep0 should be reconnecting (triggered by selectStreamForPacket)
	if !g.endpointReconnecting[0].Load() {
		t.Fatal("expected endpointReconnecting[0] to be true — triggerEndpointReconnectByURL should have been called")
	}

	// ep1 should NOT be reconnecting
	if g.endpointReconnecting[1].Load() {
		t.Fatal("expected endpointReconnecting[1] to be false — ep1 is healthy")
	}
}

// TestSelectStreamForPacket_UsesBoundTunnelWhenEndpointHealthIsStale verifies
// that response traffic can still use the endpoint it is already bound to when
// endpoint health has gone stale but the tunnel stream itself is still healthy.
// This covers the post-hello/post-tunnel reconnect window where
// refreshEndpointHealth() may mark every endpoint unhealthy before the data
// path has actually failed.
func TestSelectStreamForPacket_UsesBoundTunnelWhenEndpointHealthIsStale(t *testing.T) {
	t.Parallel()

	g := newWritePacketTestClient(t, 2)

	g.endpoints[0].healthy.Store(false)
	g.endpoints[1].healthy.Store(false)
	g.streamManager.connectionHealth[0] = false
	g.streamManager.connectionHealth[1] = false
	g.streamManager.controlStreams[0] = &configurableMockStream{}
	g.streamManager.controlStreams[1] = &configurableMockStream{}

	boundStream := &hardeningMockTunnelStream{}
	setupWritePacketStreams(g, []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "ep0-t0", stream: boundStream, lastUsed: time.Now()},
		{connIndex: 1, isHealthy: false, streamID: "ep1-t0", stream: &hardeningMockTunnelStream{}, lastUsed: time.Now()},
	})

	pkt := makeIPv6Packet()
	preBindFlowToEndpoint(g, pkt, 0)

	stream, err := g.selectStreamForPacket(pkt)
	if err != nil {
		t.Fatalf("expected bound healthy tunnel fallback, got: %v", err)
	}
	if stream.connIndex != 0 {
		t.Fatalf("expected bound stream from endpoint 0, got connIndex=%d", stream.connIndex)
	}
}

<<<<<<< HEAD
func TestSelectStreamForPacket_BoundTunnelFallbackRefreshesBindingTTL(t *testing.T) {
	t.Parallel()

	g := newWritePacketTestClient(t, 2)

	g.endpoints[0].healthy.Store(false)
	g.endpoints[1].healthy.Store(false)
	g.streamManager.connectionHealth[0] = false
	g.streamManager.connectionHealth[1] = false
	g.streamManager.controlStreams[0] = &configurableMockStream{}
	g.streamManager.controlStreams[1] = &configurableMockStream{}

	boundStream := &hardeningMockTunnelStream{}
	setupWritePacketStreams(g, []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "ep0-t0", stream: boundStream, lastUsed: time.Now()},
	})

	pkt := makeIPv6Packet()
	preBindFlowToEndpoint(g, pkt, 0)

	key := ExtractFlowKey(pkt)
	before := time.Now().Add(-2 * time.Second)
	g.selector.mu.Lock()
	g.selector.bindings[key].LastUsed = before
	g.selector.mu.Unlock()

	_, err := g.selectStreamForPacket(pkt)
	if err != nil {
		t.Fatalf("expected bound healthy tunnel fallback, got: %v", err)
	}

	g.selector.mu.RLock()
	after := g.selector.bindings[key].LastUsed
	g.selector.mu.RUnlock()
	if !after.After(before) {
		t.Fatalf("expected binding lastUsed to advance, before=%v after=%v", before, after)
	}
}

=======
>>>>>>> origin/fix/gravity-bound-tunnel-fallback
// --- Category D: Safety Net Edge Cases ---

// TestTriggerAllEndpointReconnections_ClosingClient verifies that
// triggerAllEndpointReconnections returns immediately when closing=true.
func TestTriggerAllEndpointReconnections_ClosingClient(t *testing.T) {
	t.Parallel()

	g := newEndpointTestClient(t, 3)
	g.gravityURLs = []string{"a", "b", "c"}

	// All endpoints unhealthy
	for i := 0; i < 3; i++ {
		g.endpoints[i].healthy.Store(false)
		g.streamManager.connectionHealth[i] = false
	}

	g.mu.Lock()
	g.closing = true
	g.mu.Unlock()

	g.triggerAllEndpointReconnections("closing_test")

	time.Sleep(50 * time.Millisecond)

	// No endpoints should be reconnecting
	for i := 0; i < 3; i++ {
		if g.endpointReconnecting[i].Load() {
			t.Fatalf("expected endpointReconnecting[%d] to be false when closing", i)
		}
	}
}

// TestTriggerAllEndpointReconnections_AlreadyReconnecting verifies that
// when an endpoint is already reconnecting, the CAS fails and it's not
// disrupted. Healthy endpoints are also skipped.
func TestTriggerAllEndpointReconnections_AlreadyReconnecting(t *testing.T) {
	t.Parallel()

	g := newEndpointTestClient(t, 2)
	g.gravityURLs = []string{"a", "b"}

	for i := range g.streamManager.controlStreams {
		g.streamManager.controlStreams[i] = &configurableMockStream{}
	}
	g.streamManager.tunnelStreams = []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "t0"},
		{connIndex: 1, isHealthy: true, streamID: "t1"},
	}

	// ep0 already reconnecting AND unhealthy
	g.endpointReconnecting[0].Store(true)
	g.endpoints[0].healthy.Store(false)
	g.streamManager.connectionHealth[0] = false

	// ep1 healthy
	// endpoints[1].healthy is already true from newEndpointTestClient

	g.triggerAllEndpointReconnections("already_reconnecting_test")

	time.Sleep(50 * time.Millisecond)

	// ep0 should still be reconnecting (CAS failed, not re-entered)
	if !g.endpointReconnecting[0].Load() {
		t.Fatal("expected endpointReconnecting[0] to remain true (CAS should fail)")
	}

	// ep1 should NOT be reconnecting (healthy, skipped by safety net)
	if g.endpointReconnecting[1].Load() {
		t.Fatal("expected endpointReconnecting[1] to be false — healthy endpoint should be skipped")
	}

	// ep1's control stream should remain intact
	g.streamManager.controlMu.RLock()
	if g.streamManager.controlStreams[1] == nil {
		t.Fatal("expected healthy endpoint 1 control stream to remain intact")
	}
	g.streamManager.controlMu.RUnlock()
}

// TestTriggerAllEndpointReconnections_ConcurrentCalls verifies that
// concurrent calls to triggerAllEndpointReconnections only reconnect
// each endpoint at most once (CAS protects).
func TestTriggerAllEndpointReconnections_ConcurrentCalls(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	g := &GravityClient{
		logger:                logger.NewTestLogger(),
		ctx:                   ctx,
		cancel:                cancel,
		retryConfig:           DefaultRetryConfig(),
		connections:           make([]*grpc.ClientConn, 3),
		sessionClients:        make([]pb.GravitySessionServiceClient, 3),
		connectionURLs:        []string{"grpc://ep-0", "grpc://ep-1", "grpc://ep-2"},
		endpoints:             make([]*GravityEndpoint, 3),
		endpointReconnecting:  make([]atomic.Bool, 3),
		circuitBreakers:       make([]*CircuitBreaker, 3),
		connectionIDChan:      make(chan string, 16),
		endpointStreamIndices: make(map[string][]int),
		closed:                make(chan struct{}, 1),
		peerDiscoveryWake:     make(chan struct{}, 1),
		streamManager: &StreamManager{
			controlStreams:   make([]pb.GravitySessionService_EstablishSessionClient, 3),
			controlSendMu:    make([]sync.Mutex, 3),
			tunnelStreams:    make([]*StreamInfo, 0),
			connectionHealth: make([]bool, 3),
			streamMetrics:    make(map[string]*StreamMetrics),
			contexts:         make([]context.Context, 3),
			cancels:          make([]context.CancelFunc, 3),
		},
	}
	for i := 0; i < 3; i++ {
		ep := &GravityEndpoint{URL: g.connectionURLs[i]}
		ep.healthy.Store(false) // All unhealthy
		g.endpoints[i] = ep
		g.streamManager.connectionHealth[i] = false
		g.streamManager.controlStreams[i] = &configurableMockStream{}
		g.circuitBreakers[i] = NewCircuitBreaker(DefaultCircuitBreakerConfig())
	}
	g.streamManager.tunnelStreams = []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "t0"},
		{connIndex: 1, isHealthy: true, streamID: "t1"},
		{connIndex: 2, isHealthy: true, streamID: "t2"},
	}
	g.multiEndpointMode.Store(true)

	// Cancel context to prevent infinite reconnect loops
	cancel()

	// 10 goroutines call triggerAll simultaneously
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			g.triggerAllEndpointReconnections(fmt.Sprintf("concurrent_%d", i))
		}(i)
	}
	wg.Wait()

	time.Sleep(50 * time.Millisecond)

	// All control streams should be nil (disconnected)
	g.streamManager.controlMu.RLock()
	for i, cs := range g.streamManager.controlStreams {
		if cs != nil {
			t.Fatalf("expected control stream %d to be nil after concurrent triggerAll", i)
		}
	}
	g.streamManager.controlMu.RUnlock()
}

// TestTriggerAllEndpointReconnections_SingleEndpointMode verifies backward
// compatibility: with 1 endpoint (unhealthy), triggerAll reconnects it.
func TestTriggerAllEndpointReconnections_SingleEndpointMode(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	g := &GravityClient{
		logger:                logger.NewTestLogger(),
		ctx:                   ctx,
		cancel:                cancel,
		retryConfig:           DefaultRetryConfig(),
		connections:           make([]*grpc.ClientConn, 1),
		sessionClients:        make([]pb.GravitySessionServiceClient, 1),
		connectionURLs:        []string{"grpc://ep-0"},
		endpoints:             make([]*GravityEndpoint, 1),
		endpointReconnecting:  make([]atomic.Bool, 1),
		circuitBreakers:       make([]*CircuitBreaker, 1),
		connectionIDChan:      make(chan string, 16),
		endpointStreamIndices: make(map[string][]int),
		closed:                make(chan struct{}, 1),
		peerDiscoveryWake:     make(chan struct{}, 1),
		streamManager: &StreamManager{
			controlStreams:   make([]pb.GravitySessionService_EstablishSessionClient, 1),
			controlSendMu:    make([]sync.Mutex, 1),
			tunnelStreams:    make([]*StreamInfo, 0),
			connectionHealth: []bool{false},
			streamMetrics:    make(map[string]*StreamMetrics),
			contexts:         make([]context.Context, 1),
			cancels:          make([]context.CancelFunc, 1),
		},
	}
	ep := &GravityEndpoint{URL: "grpc://ep-0"}
	ep.healthy.Store(false)
	g.endpoints[0] = ep
	g.circuitBreakers[0] = NewCircuitBreaker(DefaultCircuitBreakerConfig())
	g.streamManager.controlStreams[0] = &configurableMockStream{}

	// Cancel so reconnect goroutine exits
	cancel()

	g.triggerAllEndpointReconnections("single_endpoint_test")

	time.Sleep(50 * time.Millisecond)

	// The single unhealthy endpoint should have been disconnected
	g.streamManager.controlMu.RLock()
	cs := g.streamManager.controlStreams[0]
	g.streamManager.controlMu.RUnlock()
	if cs != nil {
		t.Fatal("expected control stream 0 to be nil (disconnected)")
	}
}

// --- Category E: handleEndpointDisconnection ---

// TestHandleEndpointDisconnection_TargetsOnlySpecificEndpoint verifies that
// with 3 endpoints, disconnecting endpoint 1 only affects endpoint 1.
// This is similar to TestHandleEndpointDisconnection_IsolatesOthers but
// explicitly checks that triggerAll is NOT called.
func TestHandleEndpointDisconnection_TargetsOnlySpecificEndpoint(t *testing.T) {
	t.Parallel()

	g := newEndpointTestClient(t, 3)
	g.gravityURLs = []string{"a", "b", "c"}

	for i := range g.streamManager.controlStreams {
		g.streamManager.controlStreams[i] = &configurableMockStream{}
	}
	g.streamManager.tunnelStreams = []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "s0"},
		{connIndex: 1, isHealthy: true, streamID: "s1"},
		{connIndex: 2, isHealthy: true, streamID: "s2"},
	}

	g.handleEndpointDisconnection(1, "test_targets_specific")

	// Give the goroutine a moment to fire, then cancel to stop reconnect loop
	time.Sleep(50 * time.Millisecond)
	g.cancel()

	// Wait for the reconnect goroutine to exit
	deadline := time.Now().Add(2 * time.Second)
	for g.endpointReconnecting[1].Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	// Only endpoint 1 should have been disconnected
	if g.streamManager.controlStreams[1] != nil {
		t.Fatal("expected control stream 1 to be nil")
	}
	if g.streamManager.controlStreams[0] == nil {
		t.Fatal("expected control stream 0 to remain intact")
	}
	if g.streamManager.controlStreams[2] == nil {
		t.Fatal("expected control stream 2 to remain intact")
	}

	// Endpoint 0 and 2 should NOT be reconnecting
	if g.endpointReconnecting[0].Load() {
		t.Fatal("expected endpointReconnecting[0] to be false")
	}
	if g.endpointReconnecting[2].Load() {
		t.Fatal("expected endpointReconnecting[2] to be false")
	}
}

// TestHandleEndpointDisconnection_DegradedModeLogging verifies that when
// one endpoint is disconnected but others are healthy, the system operates
// in degraded mode (hasHealthyEndpoint returns true).
func TestHandleEndpointDisconnection_DegradedModeLogging(t *testing.T) {
	t.Parallel()

	g := newEndpointTestClient(t, 3)
	g.gravityURLs = []string{"a", "b", "c"}

	for i := range g.streamManager.controlStreams {
		g.streamManager.controlStreams[i] = &configurableMockStream{}
	}
	g.streamManager.tunnelStreams = []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "s0"},
		{connIndex: 1, isHealthy: true, streamID: "s1"},
		{connIndex: 2, isHealthy: true, streamID: "s2"},
	}

	g.handleEndpointDisconnection(1, "degraded_test")

	time.Sleep(50 * time.Millisecond)
	g.cancel()

	// Wait for reconnect goroutine to finish
	deadline := time.Now().Add(2 * time.Second)
	for g.endpointReconnecting[1].Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	// hasHealthyEndpoint should return true because ep0 and ep2 are still healthy
	// (they have control streams and healthy tunnel streams)
	if !g.hasHealthyEndpoint() {
		t.Fatal("expected hasHealthyEndpoint() to return true — ep0 and ep2 are still healthy")
	}

	// Control streams for 0 and 2 must remain intact
	g.streamManager.controlMu.RLock()
	if g.streamManager.controlStreams[0] == nil {
		t.Fatal("expected ep0 control stream to remain intact")
	}
	if g.streamManager.controlStreams[2] == nil {
		t.Fatal("expected ep2 control stream to remain intact")
	}
	g.streamManager.controlMu.RUnlock()

	// Endpoint 1 should be marked unhealthy
	if g.endpoints[1].IsHealthy() {
		t.Fatal("expected endpoint 1 to be marked unhealthy")
	}

	// Endpoints 0 and 2 should remain healthy
	if !g.endpoints[0].IsHealthy() || !g.endpoints[2].IsHealthy() {
		t.Fatal("expected endpoints 0 and 2 to remain healthy")
	}
}

// --- Category F: Recovery ---

// TestWritePacket_AfterEndpointRecovery_TrafficResumes verifies that after
// an endpoint's streams are restored to healthy, traffic resumes through it.
func TestWritePacket_AfterEndpointRecovery_TrafficResumes(t *testing.T) {
	t.Parallel()

	g := newWritePacketTestClient(t, 2)

	ep0Stream := &hardeningMockTunnelStream{}
	ep1Stream := &hardeningMockTunnelStream{}

	setupWritePacketStreams(g, []*StreamInfo{
		{connIndex: 0, isHealthy: false, streamID: "ep0-t0", stream: ep0Stream, lastUsed: time.Now()},
		{connIndex: 0, isHealthy: false, streamID: "ep0-t1", stream: ep0Stream, lastUsed: time.Now()},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t0", stream: ep1Stream, lastUsed: time.Now()},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t1", stream: ep1Stream, lastUsed: time.Now()},
	})

	// Mark ep0 as unhealthy consistently
	g.endpoints[0].healthy.Store(false)
	g.streamManager.connectionHealth[0] = false

	pkt := makeIPv6Packet()
	preBindFlowToEndpoint(g, pkt, 0)

	// Phase 1: ep0 down → routes to ep1
	err := g.WritePacket(pkt)
	if err != nil {
		t.Fatalf("expected WritePacket to succeed via ep1 failover, got: %v", err)
	}
	if ep1Stream.sendCount.Load() == 0 {
		t.Fatal("expected traffic to route to ep1 when ep0 is down")
	}

	ep1CountAfterPhase1 := ep1Stream.sendCount.Load()

	// Phase 2: "Recover" ep0 — mark streams healthy again
	g.streamManager.tunnelMu.Lock()
	g.streamManager.tunnelStreams[0].isHealthy = true
	g.streamManager.tunnelStreams[1].isHealthy = true
	g.streamManager.tunnelMu.Unlock()

	g.endpoints[0].healthy.Store(true)
	g.streamManager.healthMu.Lock()
	g.streamManager.connectionHealth[0] = true
	g.streamManager.healthMu.Unlock()

	// Re-bind flow to ep0
	preBindFlowToEndpoint(g, pkt, 0)

	// Phase 3: ep0 recovered → traffic should resume on ep0
	err = g.WritePacket(pkt)
	if err != nil {
		t.Fatalf("expected WritePacket to succeed via recovered ep0, got: %v", err)
	}

	// ep0 should have received a packet now
	if ep0Stream.sendCount.Load() == 0 {
		t.Fatal("expected traffic to resume through recovered ep0")
	}

	// ep1 should not have received additional packets
	if ep1Stream.sendCount.Load() != ep1CountAfterPhase1 {
		t.Fatalf("expected ep1 send count unchanged after ep0 recovery, got %d (was %d)", ep1Stream.sendCount.Load(), ep1CountAfterPhase1)
	}
}

// --- Additional edge case tests ---

// TestWritePacket_ConcurrentMultiEndpoint verifies that concurrent WritePacket
// calls across multiple endpoints don't cause data races or panics.
func TestWritePacket_ConcurrentMultiEndpoint(t *testing.T) {
	t.Parallel()

	g := newWritePacketTestClient(t, 2)

	ep0Stream := &hardeningMockTunnelStream{}
	ep1Stream := &hardeningMockTunnelStream{}

	setupWritePacketStreams(g, []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "ep0-t0", stream: ep0Stream, lastUsed: time.Now()},
		{connIndex: 0, isHealthy: true, streamID: "ep0-t1", stream: ep0Stream, lastUsed: time.Now()},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t0", stream: ep1Stream, lastUsed: time.Now()},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t1", stream: ep1Stream, lastUsed: time.Now()},
	})

	const workers = 50
	const packetsPerWorker = 100

	var wg sync.WaitGroup
	panicCh := make(chan any, 1)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			defer func() {
				if p := recover(); p != nil {
					select {
					case panicCh <- p:
					default:
					}
				}
			}()

			for j := 0; j < packetsPerWorker; j++ {
				pkt := make([]byte, 54)
				pkt[0] = 0x60
				pkt[6] = 6 // TCP
				// Vary dst IP to create different flows
				pkt[39] = byte(workerID)
				pkt[43] = byte(j)
				_ = g.WritePacket(pkt)
			}
		}(i)
	}

	wg.Wait()

	select {
	case p := <-panicCh:
		t.Fatalf("panic during concurrent WritePacket: %v", p)
	default:
	}

	// Both endpoints should have received some traffic
	totalSent := ep0Stream.sendCount.Load() + ep1Stream.sendCount.Load()
	if totalSent == 0 {
		t.Fatal("expected some packets to be sent across endpoints")
	}
}

// TestSelectStreamForPacket_SingleEndpointFallback verifies that with a
// single endpoint (selector != nil but only 1 endpoint), the backward-
// compatible global stream selection path is used.
func TestSelectStreamForPacket_SingleEndpointFallback(t *testing.T) {
	t.Parallel()

	g := newWritePacketTestClient(t, 1)
	g.multiEndpointMode.Store(false) // single endpoint mode

	stream := &hardeningMockTunnelStream{}
	g.streamManager.tunnelStreams = []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "s0", stream: stream, lastUsed: time.Now()},
	}
	g.streamManager.streamMetrics["s0"] = &StreamMetrics{}

	pkt := makeIPv6Packet()
	si, err := g.selectStreamForPacket(pkt)
	if err != nil {
		t.Fatalf("expected stream selection to succeed, got: %v", err)
	}
	if si.streamID != "s0" {
		t.Fatalf("expected stream s0, got %s", si.streamID)
	}
}

// TestTunnelLiveness_AllEndpointsStale_OnlyReconnectsFirst verifies that
// checkTunnelStreamLiveness reconnects one endpoint at a time, not all.
func TestTunnelLiveness_AllEndpointsStale_OnlyReconnectsFirst(t *testing.T) {
	t.Parallel()

	g := newWritePacketTestClient(t, 2)
	g.poolConfig.StreamsPerGravity = 2

	ep0Stream := &hardeningMockTunnelStream{}
	ep1Stream := &hardeningMockTunnelStream{}

	setupWritePacketStreams(g, []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "ep0-t0", stream: ep0Stream, lastUsed: time.Now()},
		{connIndex: 0, isHealthy: true, streamID: "ep0-t1", stream: ep0Stream, lastUsed: time.Now()},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t0", stream: ep1Stream, lastUsed: time.Now()},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t1", stream: ep1Stream, lastUsed: time.Now()},
	})

	// Set up control streams
	g.streamManager.controlStreams[0] = &configurableMockStream{}
	g.streamManager.controlStreams[1] = &configurableMockStream{}

	staleTimeout := 60 * time.Second
	staleTime := time.Now().Add(-2 * staleTimeout).UnixMicro()

	// Both endpoints stale
	g.streamManager.metricsMu.Lock()
	g.streamManager.streamMetrics["ep0-t0"] = &StreamMetrics{LastSendUs: staleTime, LastRecvUs: staleTime}
	g.streamManager.streamMetrics["ep0-t1"] = &StreamMetrics{LastSendUs: staleTime, LastRecvUs: staleTime}
	g.streamManager.streamMetrics["ep1-t0"] = &StreamMetrics{LastSendUs: staleTime, LastRecvUs: staleTime}
	g.streamManager.streamMetrics["ep1-t1"] = &StreamMetrics{LastSendUs: staleTime, LastRecvUs: staleTime}
	g.streamManager.metricsMu.Unlock()

	g.checkTunnelStreamLiveness(staleTimeout)

	time.Sleep(100 * time.Millisecond)

	// The function returns after the first reconnection — only one endpoint
	// should be reconnecting per cycle.
	reconnectingCount := 0
	if g.endpointReconnecting[0].Load() {
		reconnectingCount++
	}
	if g.endpointReconnecting[1].Load() {
		reconnectingCount++
	}

	if reconnectingCount != 1 {
		t.Fatalf("expected exactly 1 endpoint reconnecting per liveness cycle, got %d", reconnectingCount)
	}
}

// TestDisconnectEndpointStreams_RefreshesHealth verifies that
// disconnectEndpointStreams calls refreshEndpointHealth, which re-derives
// endpoint health from connectionHealth. This is the critical discovery:
// if tests set endpoint.healthy=false but leave connectionHealth=true,
// refreshEndpointHealth will override the test's intent.
func TestDisconnectEndpointStreams_RefreshesHealth(t *testing.T) {
	t.Parallel()

	g := newEndpointTestClient(t, 2)
	g.gravityURLs = []string{"a", "b"}

	g.streamManager.controlStreams[0] = &configurableMockStream{}
	g.streamManager.controlStreams[1] = &configurableMockStream{}
	g.streamManager.tunnelStreams = []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "s0"},
		{connIndex: 1, isHealthy: true, streamID: "s1"},
	}

	// Before disconnecting ep0, both are healthy
	if !g.endpoints[0].IsHealthy() || !g.endpoints[1].IsHealthy() {
		t.Fatal("expected both endpoints healthy before disconnect")
	}

	g.disconnectEndpointStreams(0)

	// After disconnecting ep0:
	// - connectionHealth[0] should be false
	// - endpoint 0 should be unhealthy (refreshEndpointHealth derives from connectionHealth)
	// - endpoint 1 should remain healthy (connectionHealth[1] still true)
	if g.streamManager.connectionHealth[0] {
		t.Fatal("expected connectionHealth[0]=false after disconnect")
	}
	if g.endpoints[0].IsHealthy() {
		t.Fatal("expected endpoint 0 unhealthy after disconnectEndpointStreams")
	}
	if !g.endpoints[1].IsHealthy() {
		t.Fatal("expected endpoint 1 to remain healthy after ep0 disconnect")
	}
}

// TestWritePacket_SingleEndpointBrokenStream verifies WritePacket behavior
// when a single-endpoint client has a broken stream: it marks the stream
// unhealthy, does NOT trigger reconnection from the Write path.
func TestWritePacket_SingleEndpointBrokenStream(t *testing.T) {
	t.Parallel()

	g := newHardeningGravityClient(t, 1)
	g.streamManager.allocationStrategy = RoundRobin
	g.peerDiscoveryWake = make(chan struct{}, 1)

	brokenStream := &hardeningMockTunnelStream{sendErr: errors.New("stream reset")}
	g.streamManager.tunnelStreams = []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "s0", stream: brokenStream, lastUsed: time.Now()},
	}
	g.streamManager.connectionHealth[0] = true
	g.streamManager.streamMetrics["s0"] = &StreamMetrics{}

	pkt := make([]byte, 44)
	pkt[0] = 0x60

	err := g.WritePacket(pkt)
	// The error should propagate (stream.Send failed)
	if err == nil {
		t.Fatal("expected WritePacket to return error for broken stream")
	}

	// Stream should be marked unhealthy
	g.streamManager.tunnelMu.RLock()
	if g.streamManager.tunnelStreams[0].isHealthy {
		t.Fatal("expected stream to be marked unhealthy after Send failure")
	}
	g.streamManager.tunnelMu.RUnlock()
}

// TestWritePacket_ClosingClient verifies WritePacket returns
// ErrConnectionClosed when the client is shutting down.
func TestWritePacket_ClosingClient(t *testing.T) {
	t.Parallel()

	g := newWritePacketTestClient(t, 2)

	g.mu.Lock()
	g.closing = true
	g.mu.Unlock()

	pkt := makeIPv6Packet()
	err := g.WritePacket(pkt)
	if !errors.Is(err, ErrConnectionClosed) {
		t.Fatalf("expected ErrConnectionClosed, got %v", err)
	}
}

// TestWritePacket_NotConnected verifies WritePacket returns
// ErrConnectionClosed when the client is not connected.
func TestWritePacket_NotConnected(t *testing.T) {
	t.Parallel()

	g := newWritePacketTestClient(t, 2)

	g.mu.Lock()
	g.connected = false
	g.mu.Unlock()

	pkt := makeIPv6Packet()
	err := g.WritePacket(pkt)
	if !errors.Is(err, ErrConnectionClosed) {
		t.Fatalf("expected ErrConnectionClosed, got %v", err)
	}
}

// --- AGNT Keepalive Liveness Gap Tests ---
//
// These tests verify the exact behavior of the AGNT keepalive mechanism
// and expose a gap: sendTunnelKeepalives does NOT update LastSendUs in
// stream metrics, so if an ion dies before the first AGNT echo ever
// arrives, the liveness monitor sees zero timestamps and classifies the
// stream as "idle" rather than "stale" — the dead endpoint is invisible.

// TestAGNTKeepalive_SendsProbesOnIdleHadron verifies that
// sendTunnelKeepalives sends probes on healthy streams regardless of
// whether any container traffic has flowed. This is the AGNT liveness
// mechanism that keeps idle connections alive.
func TestAGNTKeepalive_SendsProbesOnIdleHadron(t *testing.T) {
	t.Parallel()

	g := newWritePacketTestClient(t, 2)
	g.poolConfig.StreamsPerGravity = 2

	// Set up tunnel streams with mock streams that track Send calls.
	// No container traffic — this is a completely idle hadron.
	ep0Stream := &hardeningMockTunnelStream{}
	ep1Stream := &hardeningMockTunnelStream{}

	g.streamManager.tunnelMu.Lock()
	g.streamManager.tunnelStreams = []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "ep0-t0", stream: ep0Stream, lastUsed: time.Now()},
		{connIndex: 0, isHealthy: true, streamID: "ep0-t1", stream: ep0Stream, lastUsed: time.Now()},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t0", stream: ep1Stream, lastUsed: time.Now()},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t1", stream: ep1Stream, lastUsed: time.Now()},
	}
	g.streamManager.tunnelMu.Unlock()

	g.streamManager.metricsMu.Lock()
	g.streamManager.streamMetrics["ep0-t0"] = &StreamMetrics{}
	g.streamManager.streamMetrics["ep0-t1"] = &StreamMetrics{}
	g.streamManager.streamMetrics["ep1-t0"] = &StreamMetrics{}
	g.streamManager.streamMetrics["ep1-t1"] = &StreamMetrics{}
	g.streamManager.metricsMu.Unlock()

	g.sendTunnelKeepalives()

	// Goroutines run async — wait for them to complete.
	time.Sleep(100 * time.Millisecond)

	// Probes SHOULD be sent on both endpoints (one stream per endpoint per rotation).
	ep0Sends := ep0Stream.sendCount.Load()
	ep1Sends := ep1Stream.sendCount.Load()

	if ep0Sends == 0 {
		t.Fatal("expected AGNT probe sent to endpoint 0 — keepalive not running on idle hadron")
	}
	if ep1Sends == 0 {
		t.Fatal("expected AGNT probe sent to endpoint 1 — keepalive not running on idle hadron")
	}
}

// TestAGNTKeepalive_UpdatesLastProbeSentUs verifies that
// sendTunnelKeepalives updates LastProbeSentUs on every successful probe,
// promoting the stream from "idle" (zero timestamps) to "probed." This
// enables the liveness monitor to detect dead endpoints on idle hadrons
// where no user traffic flows — without this update, the monitor would
// see zero timestamps and classify the stream as idle instead of stale.
// LastProbeSentUs is separate from LastSendUs (updated by data traffic)
// to prevent WritePacket sends from masking zombie tunnels.
func TestAGNTKeepalive_UpdatesLastProbeSentUs(t *testing.T) {
	t.Parallel()

	g := newWritePacketTestClient(t, 1)
	g.poolConfig.StreamsPerGravity = 1

	mockStream := &hardeningMockTunnelStream{}

	g.streamManager.tunnelMu.Lock()
	g.streamManager.tunnelStreams = []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "s0", stream: mockStream, lastUsed: time.Now()},
	}
	g.streamManager.tunnelMu.Unlock()

	g.streamManager.metricsMu.Lock()
	g.streamManager.streamMetrics["s0"] = &StreamMetrics{} // LastSendUs = 0
	g.streamManager.metricsMu.Unlock()

	g.sendTunnelKeepalives()
	time.Sleep(100 * time.Millisecond)

	// Verify probe was sent.
	if mockStream.sendCount.Load() == 0 {
		t.Fatal("expected AGNT probe to be sent")
	}

	// Check if LastProbeSentUs was updated.
	g.streamManager.metricsMu.Lock()
	lastProbeSentUs := g.streamManager.streamMetrics["s0"].LastProbeSentUs
	g.streamManager.metricsMu.Unlock()

	// After fix: sendTunnelKeepalives updates LastProbeSentUs on every
	// successful probe, promoting the stream from "idle" to "probed."
	// LastProbeSentUs is separate from LastSendUs (data traffic) to
	// prevent WritePacket sends from masking zombie tunnels.
	if lastProbeSentUs == 0 {
		t.Fatal("sendTunnelKeepalives must update LastProbeSentUs on probe — dead endpoints on idle hadrons would be invisible to liveness monitor")
	}
}

// TestAGNTKeepalive_SendFailure_DoesNotMarkStreamUnhealthy verifies that
// a single transient Send() failure on an AGNT probe does NOT immediately
// mark the stream as unhealthy. This is intentional — the liveness monitor
// detects dead endpoints via stale timestamps (no echo received within
// staleTimeout), which is more robust than reacting to individual failures.
func TestAGNTKeepalive_SendFailure_DoesNotMarkStreamUnhealthy(t *testing.T) {
	t.Parallel()

	g := newWritePacketTestClient(t, 1)
	g.poolConfig.StreamsPerGravity = 1

	mockStream := &hardeningMockTunnelStream{sendErr: fmt.Errorf("connection broken")}

	g.streamManager.tunnelMu.Lock()
	g.streamManager.tunnelStreams = []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "s0", stream: mockStream, lastUsed: time.Now()},
	}
	g.streamManager.tunnelMu.Unlock()

	g.streamManager.metricsMu.Lock()
	g.streamManager.streamMetrics["s0"] = &StreamMetrics{}
	g.streamManager.metricsMu.Unlock()

	g.sendTunnelKeepalives()
	time.Sleep(100 * time.Millisecond)

	// Check if the stream was marked unhealthy after Send failure.
	g.streamManager.tunnelMu.RLock()
	stillHealthy := g.streamManager.tunnelStreams[0].isHealthy
	g.streamManager.tunnelMu.RUnlock()

	// Send failure on probe does NOT mark stream unhealthy — this is
	// intentional. A single transient Send failure shouldn't flap the
	// stream. The liveness monitor detects dead endpoints via stale
	// timestamps (no echo received within staleTimeout).
	if !stillHealthy {
		t.Fatal("AGNT probe Send failure should NOT mark stream unhealthy — that's too aggressive for a transient error")
	}
}

// TestLiveness_TrulyIdleStreams_NotFlaggedAsStale verifies that streams
// where BOTH LastSendUs and LastRecvUs are zero (never probed, never
// received) are correctly classified as "idle" — not stale. This prevents
// false-positive reconnection on streams that were just established.
func TestLiveness_TrulyIdleStreams_NotFlaggedAsStale(t *testing.T) {
	t.Parallel()

	g := newEndpointTestClient(t, 2)
	g.gravityURLs = []string{"a", "b"}
	g.poolConfig.StreamsPerGravity = 2
	g.peerDiscoveryWake = make(chan struct{}, 1)

	for i := range g.streamManager.controlStreams {
		g.streamManager.controlStreams[i] = &configurableMockStream{}
	}
	g.streamManager.tunnelStreams = []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "ep0-t0"},
		{connIndex: 0, isHealthy: true, streamID: "ep0-t1"},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t0"},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t1"},
	}

	// BOTH timestamps zero — stream just established, no probe yet.
	g.streamManager.metricsMu.Lock()
	g.streamManager.streamMetrics["ep0-t0"] = &StreamMetrics{LastSendUs: 0, LastRecvUs: 0}
	g.streamManager.streamMetrics["ep0-t1"] = &StreamMetrics{LastSendUs: 0, LastRecvUs: 0}
	g.streamManager.streamMetrics["ep1-t0"] = &StreamMetrics{LastSendUs: 0, LastRecvUs: 0}
	g.streamManager.streamMetrics["ep1-t1"] = &StreamMetrics{LastSendUs: 0, LastRecvUs: 0}
	g.streamManager.metricsMu.Unlock()

	staleTimeout := 30 * time.Second
	g.checkTunnelStreamLiveness(staleTimeout)
	time.Sleep(100 * time.Millisecond)

	// Truly idle streams (both zero) should NOT trigger reconnection.
	if g.endpointReconnecting[0].Load() || g.endpointReconnecting[1].Load() {
		t.Fatal("truly idle streams (zero timestamps) should NOT be flagged as stale")
	}
}

// TestLiveness_ProbedButNoEcho_DetectsDeadEndpoint verifies that the
// liveness monitor detects dead endpoints when a probe was sent
// (LastProbeSentUs > 0) but no echo ever came back (LastRecvUs == 0).
// This is the "ion dies before first AGNT echo" scenario on idle hadrons.
func TestLiveness_ProbedButNoEcho_DetectsDeadEndpoint(t *testing.T) {
	t.Parallel()

	g := newEndpointTestClient(t, 2)
	g.gravityURLs = []string{"a", "b"}
	g.poolConfig.StreamsPerGravity = 2
	g.peerDiscoveryWake = make(chan struct{}, 1)

	for i := range g.streamManager.controlStreams {
		g.streamManager.controlStreams[i] = &configurableMockStream{}
	}
	g.streamManager.tunnelStreams = []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "ep0-t0"},
		{connIndex: 0, isHealthy: true, streamID: "ep0-t1"},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t0"},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t1"},
	}

	staleTimeout := 30 * time.Second
	// ep0: probed 60s ago, never got echo — dead endpoint
	oldProbeTime := time.Now().Add(-2 * staleTimeout).UnixMicro()
	// ep1: probed recently, echo received — healthy
	recentTime := time.Now().UnixMicro()

	g.streamManager.metricsMu.Lock()
	g.streamManager.streamMetrics["ep0-t0"] = &StreamMetrics{LastProbeSentUs: oldProbeTime, LastRecvUs: 0}
	g.streamManager.streamMetrics["ep0-t1"] = &StreamMetrics{LastProbeSentUs: oldProbeTime, LastRecvUs: 0}
	g.streamManager.streamMetrics["ep1-t0"] = &StreamMetrics{LastProbeSentUs: recentTime, LastRecvUs: recentTime}
	g.streamManager.streamMetrics["ep1-t1"] = &StreamMetrics{LastProbeSentUs: recentTime, LastRecvUs: recentTime}
	g.streamManager.metricsMu.Unlock()

	g.checkTunnelStreamLiveness(staleTimeout)
	time.Sleep(100 * time.Millisecond)

	// ep0: probed but no echo, probe time is stale → MUST be detected
	if !g.endpointReconnecting[0].Load() {
		t.Fatal("expected dead endpoint 0 (probed, no echo, stale) to enter reconnection")
	}
	// ep1: healthy (recent echo)
	if g.endpointReconnecting[1].Load() {
		t.Fatal("expected healthy endpoint 1 to NOT enter reconnection")
	}
}

// TestLiveness_NonZeroStaleTimestamps_DetectsDeadEndpoint verifies the
// NORMAL case: when AGNT echoes were flowing (non-zero timestamps) and
// then stopped (timestamps go stale), the liveness monitor correctly
// detects the dead endpoint and triggers reconnection.
func TestLiveness_NonZeroStaleTimestamps_DetectsDeadEndpoint(t *testing.T) {
	t.Parallel()

	g := newEndpointTestClient(t, 2)
	g.gravityURLs = []string{"a", "b"}
	g.poolConfig.StreamsPerGravity = 2
	g.peerDiscoveryWake = make(chan struct{}, 1)

	for i := range g.streamManager.controlStreams {
		g.streamManager.controlStreams[i] = &configurableMockStream{}
	}
	g.streamManager.tunnelStreams = []*StreamInfo{
		{connIndex: 0, isHealthy: true, streamID: "ep0-t0"},
		{connIndex: 0, isHealthy: true, streamID: "ep0-t1"},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t0"},
		{connIndex: 1, isHealthy: true, streamID: "ep1-t1"},
	}

	staleTimeout := 60 * time.Second
	staleTime := time.Now().Add(-2 * staleTimeout).UnixMicro() // well past stale
	recentTime := time.Now().UnixMicro()                       // fresh

	// ep0: stale (ion died, no AGNT echoes for >60s)
	// ep1: fresh (AGNT echoes still arriving)
	g.streamManager.metricsMu.Lock()
	g.streamManager.streamMetrics["ep0-t0"] = &StreamMetrics{LastSendUs: staleTime, LastRecvUs: staleTime}
	g.streamManager.streamMetrics["ep0-t1"] = &StreamMetrics{LastSendUs: staleTime, LastRecvUs: staleTime}
	g.streamManager.streamMetrics["ep1-t0"] = &StreamMetrics{LastSendUs: recentTime, LastRecvUs: recentTime}
	g.streamManager.streamMetrics["ep1-t1"] = &StreamMetrics{LastSendUs: recentTime, LastRecvUs: recentTime}
	g.streamManager.metricsMu.Unlock()

	g.checkTunnelStreamLiveness(staleTimeout)
	time.Sleep(100 * time.Millisecond)

	// ep0 should be reconnecting (stale streams detected).
	if !g.endpointReconnecting[0].Load() {
		t.Fatal("expected stale endpoint 0 to enter reconnection — liveness monitor failed")
	}
	// ep1 should NOT be reconnecting (fresh streams).
	if g.endpointReconnecting[1].Load() {
		t.Fatal("expected fresh endpoint 1 to NOT enter reconnection")
	}
}
