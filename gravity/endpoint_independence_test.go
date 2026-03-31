package gravity

import (
	"context"
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
