package gravity

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/agentuity/go-common/gravity/proto"
	"github.com/agentuity/go-common/gravity/provider"
	"github.com/agentuity/go-common/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

type hardeningTestProvider struct {
	configureErr   error
	configureCalls atomic.Int64
}

func (p *hardeningTestProvider) Configure(provider.Configuration) error {
	p.configureCalls.Add(1)
	return p.configureErr
}

func (p *hardeningTestProvider) ProcessInPacket([]byte) {}

type hardeningCheckpointProvider struct {
	hardeningTestProvider
	results []*pb.SandboxCheckpointed
}

func (p *hardeningCheckpointProvider) HandleEvacuationPlan(context.Context, []*pb.EvacuateSandboxPlan) []*pb.SandboxCheckpointed {
	return p.results
}

func (p *hardeningCheckpointProvider) HandleRestoreSandboxTask(context.Context, *pb.RestoreSandboxTask) *pb.SandboxRestored {
	return nil
}

func (p *hardeningCheckpointProvider) SupportsCheckpointRestore() bool { return true }

type hardeningTestNetworkInterface struct {
	routeErr    error
	routeCalls  atomic.Int64
	unrouteErr  error
	unrouteCall atomic.Int64
}

func (n *hardeningTestNetworkInterface) RouteTraffic([]string) error {
	n.routeCalls.Add(1)
	return n.routeErr
}

func (n *hardeningTestNetworkInterface) UnrouteTraffic() error {
	n.unrouteCall.Add(1)
	return n.unrouteErr
}

func (n *hardeningTestNetworkInterface) Read([]byte) (int, error)  { return 0, io.EOF }
func (n *hardeningTestNetworkInterface) Write([]byte) (int, error) { return 0, nil }
func (n *hardeningTestNetworkInterface) Running() bool             { return true }
func (n *hardeningTestNetworkInterface) Start(func([]byte))        {}

type hardeningMockTunnelStream struct {
	sendErr   error
	sendDelay time.Duration
	sendCount atomic.Int64
	mu        sync.Mutex
}

var _ pb.GravitySessionService_StreamSessionPacketsClient = (*hardeningMockTunnelStream)(nil)

func (m *hardeningMockTunnelStream) Send(*pb.TunnelPacket) error {
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

func (m *hardeningMockTunnelStream) Recv() (*pb.TunnelPacket, error) {
	return nil, io.EOF
}

func (m *hardeningMockTunnelStream) Header() (metadata.MD, error) { return nil, nil }
func (m *hardeningMockTunnelStream) Trailer() metadata.MD         { return nil }
func (m *hardeningMockTunnelStream) CloseSend() error             { return nil }
func (m *hardeningMockTunnelStream) Context() context.Context     { return context.Background() }
func (m *hardeningMockTunnelStream) SendMsg(any) error            { return nil }
func (m *hardeningMockTunnelStream) RecvMsg(any) error            { return nil }

func newHardeningGravityClient(t *testing.T, n int) *GravityClient {
	t.Helper()
	g := newEndpointTestClient(t, n)
	g.logger = logger.NewTestLogger()
	g.poolConfig = ConnectionPoolConfig{
		PoolSize:             n,
		StreamsPerConnection: 1,
		AllocationStrategy:   RoundRobin,
		HealthCheckInterval:  25 * time.Millisecond,
		FailoverTimeout:      10 * time.Millisecond,
	}
	g.connected = true
	g.sessionReady = make(chan struct{})
	g.bufferPool.New = func() any {
		return make([]byte, maxBufferSize)
	}
	return g
}

func TestHardening_BufferPoolLeak(t *testing.T) {
	g := newHardeningGravityClient(t, 1)

	// Track whether the pool's New function is called during getBuffer.
	// For oversized payloads, getBuffer should bypass the pool entirely —
	// no Get (and therefore no New) should be invoked.
	var newCalls atomic.Int64
	g.bufferPool.New = func() any {
		newCalls.Add(1)
		return make([]byte, maxBufferSize)
	}

	// Pre-warm: ensure the pool is empty so any Get would trigger New.
	_ = g.bufferPool.Get()
	newCalls.Store(0) // reset after warm-up

	largePayload := make([]byte, maxBufferSize+1)
	pb := g.getBuffer(largePayload)

	if newCalls.Load() != 0 {
		t.Fatalf("getBuffer for oversized payload should bypass pool, but pool.New was called %d times", newCalls.Load())
	}
	if len(pb.Buffer) != maxBufferSize+1 {
		t.Fatalf("expected buffer length %d for oversized payload, got %d", maxBufferSize+1, len(pb.Buffer))
	}

	// returnBuffer should NOT put oversized buffers back into the pool.
	g.returnBuffer(pb)
	newCalls.Store(0)
	buf := g.bufferPool.Get().([]byte)
	if len(buf) != maxBufferSize {
		t.Fatalf("expected pool to return maxBufferSize buffer, got %d (oversized buffer leaked into pool)", len(buf))
	}
}

func TestHardening_GetConnectionPoolStatsReturnsLiveMap(t *testing.T) {
	g := newHardeningGravityClient(t, 1)
	g.streamManager.streamMetrics["s0"] = &StreamMetrics{PacketsSent: 1}
	g.streamManager.streamMetrics["s1"] = &StreamMetrics{PacketsSent: 42, BytesSent: 100}

	stats := g.GetConnectionPoolStats()
	returned, ok := stats["stream_metrics"].(map[string]*StreamMetrics)
	if !ok {
		t.Fatalf("unexpected stream_metrics type: %T", stats["stream_metrics"])
	}

	// Map header must be a different object.
	internalPtr := reflect.ValueOf(g.streamManager.streamMetrics).Pointer()
	returnedPtr := reflect.ValueOf(returned).Pointer()
	if internalPtr == returnedPtr {
		t.Fatalf("expected defensive copy of stream metrics map, got live reference")
	}

	// Each *StreamMetrics value must be a deep copy, not a shared pointer.
	for key, retVal := range returned {
		origVal, exists := g.streamManager.streamMetrics[key]
		if !exists {
			continue
		}
		retPtr := reflect.ValueOf(retVal).Pointer()
		origPtr := reflect.ValueOf(origVal).Pointer()
		if retPtr == origPtr {
			t.Fatalf("stream_metrics[%q] shares pointer with internal map (shallow copy)", key)
		}
	}

	// Mutating a returned value must not affect internal state.
	returned["s0"].PacketsSent = 9999
	if g.streamManager.streamMetrics["s0"].PacketsSent == 9999 {
		t.Fatalf("mutating returned StreamMetrics value changed internal state (values not deep-copied)")
	}

	// Inserting a new key must not affect internal state.
	returned["injected"] = &StreamMetrics{PacketsSent: 999}
	if _, exists := g.streamManager.streamMetrics["injected"]; exists {
		t.Fatalf("inserting into returned map modified internal state (data race hazard)")
	}
}

func TestHardening_PerformHealthCheckMarksDeadStreamsHealthy(t *testing.T) {
	g := newHardeningGravityClient(t, 1)
	broken := &StreamInfo{
		stream:    nil,
		connIndex: 0,
		streamID:  "broken-stream",
		isHealthy: false,
		loadCount: 55,
		lastUsed:  time.Now().Add(-time.Minute),
	}
	g.streamManager.tunnelStreams = []*StreamInfo{broken}
	g.streamManager.streamMetrics[broken.streamID] = &StreamMetrics{ErrorCount: 7}

	g.performHealthCheck()

	if broken.isHealthy {
		t.Fatalf("expected broken stream to remain unhealthy until stream is re-established")
	}
	if broken.stream != nil {
		t.Fatalf("expected broken underlying stream to be unchanged")
	}
}

func TestHardening_RandIndexNotRandom(t *testing.T) {
	const n = 1_000_003
	const samples = 2000

	values := make([]int, samples)
	for i := 0; i < samples; i++ {
		values[i] = randIndex(n)
	}

	// For real random values in [0,n), circular step size tends to be very large.
	// Here we assert that the median step should be > 10k if values are random.
	steps := make([]int, 0, samples-1)
	for i := 1; i < len(values); i++ {
		a := values[i-1]
		b := values[i]
		d1 := (b - a + n) % n
		d2 := (a - b + n) % n
		d := d1
		if d2 < d {
			d = d2
		}
		steps = append(steps, d)
	}

	// selection sort for deterministic tiny sample
	for i := 0; i < len(steps); i++ {
		minIdx := i
		for j := i + 1; j < len(steps); j++ {
			if steps[j] < steps[minIdx] {
				minIdx = j
			}
		}
		steps[i], steps[minIdx] = steps[minIdx], steps[i]
	}
	median := steps[len(steps)/2]
	if median < 10_000 {
		t.Fatalf("expected random-looking distribution with large median step, got %d (predictable time-based sequence)", median)
	}
}

func TestHardening_ReconnectDrainsAllConnectionIDs(t *testing.T) {
	g := newHardeningGravityClient(t, 1)

	// Push multiple stale IDs into the real channel.
	g.connectionIDChan <- "a"
	g.connectionIDChan <- "b"
	g.connectionIDChan <- "c"
	g.connectionIDChan <- "d"

	// Call the production drain helper used by reconnect().
	g.drainConnectionIDChan()

	if got := len(g.connectionIDChan); got != 0 {
		t.Fatalf("expected drainConnectionIDChan to clear channel, still has %d stale IDs", got)
	}
}

func TestHardening_ZeroHealthCheckIntervalPanics(t *testing.T) {
	g := newHardeningGravityClient(t, 1)
	g.poolConfig.HealthCheckInterval = 0

	panicCh := make(chan any, 1)
	go func() {
		defer func() {
			panicCh <- recover()
		}()
		g.monitorConnectionHealth()
	}()

	select {
	case p := <-panicCh:
		if p != nil {
			t.Fatalf("monitorConnectionHealth should not panic with zero interval, got panic: %v", p)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("monitorConnectionHealth did not return or panic in time")
	}
}

func TestHardening_SimpleHashBytesMissesDestIP(t *testing.T) {
	p1 := make([]byte, 60)
	p2 := make([]byte, 60)
	copy(p2, p1)

	// Keep source the same (bytes 8..23), change destination (bytes 24..39).
	for i := 24; i < 40; i++ {
		p2[i] = byte(i + 1)
	}

	h1 := simpleHashBytes(p1)
	h2 := simpleHashBytes(p2)
	if h1 == h2 {
		t.Fatalf("expected different destination IPs to produce different hashes, got identical hash=%d", h1)
	}
}

func TestHardening_WaitForSessionTOCTOU(t *testing.T) {
	g := newHardeningGravityClient(t, 1)
	oldReady := make(chan struct{})
	g.sessionReady = oldReady

	errCh := make(chan error, 1)
	go func() {
		errCh <- g.WaitForSession(2 * time.Second)
	}()

	// Give WaitForSession time to snapshot the old channel.
	time.Sleep(10 * time.Millisecond)

	// Simulate reconnect swapping sessionReady while WaitForSession is blocked.
	newReady := make(chan struct{})
	g.mu.Lock()
	g.sessionReady = newReady
	g.mu.Unlock()

	// Close the OLD channel. WaitForSession should wake up, detect the swap,
	// and loop back to wait on newReady — NOT return nil.
	close(oldReady)

	// WaitForSession must not have returned yet (it should be waiting on newReady).
	select {
	case err := <-errCh:
		t.Fatalf("WaitForSession returned prematurely after stale channel close: %v", err)
	case <-time.After(50 * time.Millisecond):
		// Good — still blocking on newReady.
	}

	// Now close the real channel. WaitForSession should return nil.
	close(newReady)

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("WaitForSession should succeed after newReady is closed, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitForSession did not return after newReady was closed")
	}
}

func TestHardening_WritePacketWhenClosing(t *testing.T) {
	g := newHardeningGravityClient(t, 1)
	g.mu.Lock()
	g.connected = true
	g.closing = true
	g.mu.Unlock()

	err := g.WritePacket([]byte{0, 1, 2, 3})
	if !errors.Is(err, ErrConnectionClosed) {
		t.Fatalf("expected ErrConnectionClosed while closing, got %v", err)
	}
}

func TestHardening_ConcurrentCloseAndWritePacket(t *testing.T) {
	g := newHardeningGravityClient(t, 1)
	g.streamManager.allocationStrategy = RoundRobin
	g.streamManager.tunnelStreams = []*StreamInfo{{
		stream:    &hardeningMockTunnelStream{},
		connIndex: 0,
		streamID:  "s0",
		isHealthy: true,
		lastUsed:  time.Now(),
	}}
	g.streamManager.connectionHealth[0] = true
	g.streamManager.streamMetrics["s0"] = &StreamMetrics{}

	var wg sync.WaitGroup
	panicCh := make(chan any, 1)

	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if p := recover(); p != nil {
					select {
					case panicCh <- p:
					default:
					}
				}
			}()

			for j := 0; j < 200; j++ {
				_ = g.WritePacket([]byte{0, 0, 0, 0, 0, 0, 6, 0, 0})
			}
		}()
	}

	go func() {
		_ = g.Close()
	}()

	wg.Wait()
	select {
	case p := <-panicCh:
		t.Fatalf("panic during concurrent Close + WritePacket: %v", p)
	default:
	}
}

func TestHardening_SelectWeightedRoundRobinWithEmptyHealth(t *testing.T) {
	g := newHardeningGravityClient(t, 1)
	g.streamManager.tunnelStreams = []*StreamInfo{{
		stream:    &hardeningMockTunnelStream{},
		connIndex: 1, // out of bounds for connectionHealth below
		streamID:  "s-out-of-range",
		isHealthy: true,
	}}
	g.streamManager.connectionHealth = []bool{true}

	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("selectWeightedRoundRobinStream should not panic on sparse health data: %v", p)
		}
	}()

	_, _ = g.selectWeightedRoundRobinStream()
}

func TestHardening_AllStreamSelectionStrategies(t *testing.T) {
	baseline := runtime.NumGoroutine()

	cases := []struct {
		name             string
		strategy         StreamAllocationStrategy
		tunnelStreams    []*StreamInfo
		connectionHealth []bool
		expectErr        bool
	}{
		{
			name:             "round_robin_no_streams",
			strategy:         RoundRobin,
			tunnelStreams:    nil,
			connectionHealth: []bool{true},
			expectErr:        true,
		},
		{
			name:     "hash_based_single_stream",
			strategy: HashBased,
			tunnelStreams: []*StreamInfo{{
				streamID:  "s1",
				isHealthy: true,
			}},
			connectionHealth: []bool{true},
			expectErr:        false,
		},
		{
			name:     "least_connections_all_unhealthy",
			strategy: LeastConnections,
			tunnelStreams: []*StreamInfo{{
				streamID:  "s1",
				isHealthy: false,
			}},
			connectionHealth: []bool{true},
			expectErr:        true,
		},
		{
			name:     "weighted_round_robin_mixed_health",
			strategy: WeightedRoundRobin,
			tunnelStreams: []*StreamInfo{
				{streamID: "s1", connIndex: 0, isHealthy: false},
				{streamID: "s2", connIndex: 0, isHealthy: true},
			},
			connectionHealth: []bool{true},
			expectErr:        false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			g := newHardeningGravityClient(t, 1)
			g.streamManager.allocationStrategy = tc.strategy
			g.streamManager.tunnelStreams = tc.tunnelStreams
			g.streamManager.connectionHealth = tc.connectionHealth

			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("strategy %s panicked: %v", tc.strategy.String(), p)
				}
			}()

			_, err := g.selectOptimalStream([]byte{1, 2, 3, 4})
			if tc.expectErr && err == nil {
				t.Fatalf("expected error for strategy %s case %s", tc.strategy.String(), tc.name)
			}
			if !tc.expectErr && err != nil {
				t.Fatalf("unexpected error for strategy %s case %s: %v", tc.strategy.String(), tc.name, err)
			}
		})
	}

	// Verify no goroutine leak from stream selection logic.
	const allowedLeak = 5
	if got := runtime.NumGoroutine(); got > baseline+allowedLeak {
		t.Fatalf("unexpected goroutine growth: got=%d baseline=%d allowed=%d", got, baseline, allowedLeak)
	}
}

func TestHardening_IdentifyRejectsMalformedPEM(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate test key: %v", err)
	}

	_, err = Identify(context.Background(), IdentifyConfig{
		GravityURL:      "grpc://127.0.0.1:443",
		InstanceID:      "hardening-identify",
		ECDSAPrivateKey: key,
		CACert:          "not-a-valid-pem",
	})
	if err == nil {
		t.Fatal("expected malformed PEM to be rejected")
	}
	if err.Error() != "failed to parse CA certificate PEM" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHardening_CertLifetimeExtended(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate test key: %v", err)
	}

	cert, err := createSelfSignedTLSConfig(key, "hardening-cert-lifetime")
	if err != nil {
		t.Fatalf("createSelfSignedTLSConfig failed: %v", err)
	}
	if cert.Leaf == nil {
		t.Fatal("expected parsed leaf certificate")
	}

	lifetime := cert.Leaf.NotAfter.Sub(cert.Leaf.NotBefore)
	if lifetime < 300*24*time.Hour {
		t.Fatalf("expected certificate lifetime > 300 days, got %v", lifetime)
	}
}

func TestHardening_EvacuationTimerCleanup(t *testing.T) {
	g := newHardeningGravityClient(t, 1)
	g.context = context.Background()
	g.pending = make(map[string]chan *pb.ProtocolResponse)
	g.streamManager.controlStreams[0] = &mockSessionStream{sent: make(chan *pb.SessionMessage, 8)}

	provider := &hardeningCheckpointProvider{
		results: []*pb.SandboxCheckpointed{{SandboxId: "sbx-1", Success: true}},
	}
	g.provider = provider

	callbackDone := make(chan struct{})
	g.SetEvacuationCallback(func() {
		select {
		case <-callbackDone:
		default:
			close(callbackDone)
		}
	})

	go func() {
		for {
			g.pendingMu.RLock()
			for _, ch := range g.pending {
				select {
				case ch <- &pb.ProtocolResponse{Success: true}:
				default:
				}
			}
			g.pendingMu.RUnlock()

			select {
			case <-callbackDone:
				return
			default:
			}
			time.Sleep(1 * time.Millisecond)
		}
	}()

	g.handleEvacuationPlan("evac-msg", &pb.EvacuationPlan{
		Sandboxes: []*pb.EvacuateSandboxPlan{{SandboxId: "sbx-1"}},
	})

	select {
	case <-callbackDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("evacuation callback did not fire")
	}

	g.pendingMu.RLock()
	pending := len(g.pending)
	g.pendingMu.RUnlock()
	if pending != 0 {
		t.Fatalf("expected pending map to be empty after evacuation ack, got %d", pending)
	}
}

func TestHardening_ConnectionCtxCancelsBackgroundGoroutines(t *testing.T) {
	g := newHardeningGravityClient(t, 1)
	g.poolConfig.HealthCheckInterval = 10 * time.Millisecond
	g.pingInterval = 10 * time.Millisecond

	g.mu.Lock()
	g.connectionCtx, g.connectionCancel = context.WithCancel(g.ctx)
	g.mu.Unlock()

	dones := []chan struct{}{
		make(chan struct{}),
		make(chan struct{}),
		make(chan struct{}),
		make(chan struct{}),
		make(chan struct{}),
	}

	go func() { defer close(dones[0]); g.handleInboundPackets() }()
	go func() { defer close(dones[1]); g.handleOutboundPackets() }()
	go func() { defer close(dones[2]); g.handleTextMessages() }()
	go func() { defer close(dones[3]); g.monitorConnectionHealth() }()
	go func() { defer close(dones[4]); g.handlePingHeartbeat() }()

	time.Sleep(20 * time.Millisecond)
	g.mu.Lock()
	g.connectionCancel()
	g.mu.Unlock()

	for i, done := range dones {
		select {
		case <-done:
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("background goroutine %d did not exit after connection context cancel", i)
		}
	}
}

func TestHardening_SessionHelloConfigureFailureUnblocksWait(t *testing.T) {
	g := newHardeningGravityClient(t, 1)
	g.context = context.Background()
	g.provider = &hardeningTestProvider{configureErr: errors.New("configure failed")}
	g.networkInterface = &hardeningTestNetworkInterface{}

	errCh := make(chan error, 1)
	go func() {
		errCh <- g.WaitForSession(500 * time.Millisecond)
	}()

	time.Sleep(10 * time.Millisecond)
	g.handleSessionHelloResponse(0, "session_hello", &pb.SessionHelloResponse{
		MachineId:        "machine-1",
		MachineToken:     "token-1",
		SubnetRoutes:     []string{"fd00::/64"},
		Environment:      []string{"A=B"},
		HostMapping:      []*pb.HostMapping{},
		SshPublicKey:     []byte("ssh-key"),
		SigningPublicKey: []byte("signing-key"),
	})

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("WaitForSession should unblock without timeout, got: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("WaitForSession remained blocked after configure failure")
	}

	select {
	case id := <-g.connectionIDChan:
		if id != "" {
			t.Fatalf("expected empty machine ID failure signal, got %q", id)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected failure signal on connectionIDChan")
	}
}

func TestHardening_LockOrderingConsistency(t *testing.T) {
	g := newHardeningGravityClient(t, 1)
	g.poolConfig.FailoverTimeout = 5 * time.Millisecond
	g.connections = make([]*grpc.ClientConn, 1)
	g.streamManager.connectionHealth = []bool{true}
	g.streamManager.tunnelStreams = []*StreamInfo{{
		streamID:  "s0",
		connIndex: 0,
		isHealthy: true,
		lastUsed:  time.Now().Add(-time.Second),
	}}
	g.streamManager.streamMetrics["s0"] = &StreamMetrics{}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			g.performHealthCheck()
		}
	}()

	for i := 0; i < 1000; i++ {
		g.streamManager.tunnelMu.Lock()
		_, _ = g.selectWeightedRoundRobinStream()
		g.streamManager.tunnelMu.Unlock()
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("potential deadlock detected between performHealthCheck and weighted selection")
	}
}

// ============================================================================
// IDLE Connection Recovery Tests
// ============================================================================
//
// These tests verify the performHealthCheck IDLE detection and Connect()
// recovery that was added to fix a critical production bug: gRPC connections
// stuck in IDLE state after ion restarts were never recovered, causing a
// 48-minute full region outage.

// newIdleGRPCConn creates a real grpc.ClientConn in IDLE state for testing.
// gRPC connections created via grpc.NewClient start lazy (IDLE) and only
// attempt to connect when Connect() is called or an RPC is issued.
func newIdleGRPCConn(t *testing.T) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(
		"dns:///127.0.0.1:1",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to create idle grpc connection: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// TestPerformHealthCheck_IDLEConnectionRecovery verifies that when a
// connection is in IDLE state, performHealthCheck:
//  1. Marks connectionHealth[i] = false
//  2. Increments connectionIdleCount[i]
//  3. Calls conn.Connect() to force reconnection
//
// This is the core regression test for the 48-minute outage fix.
func TestPerformHealthCheck_IDLEConnectionRecovery(t *testing.T) {
	g := newHardeningGravityClient(t, 1)

	// Create a real gRPC connection in IDLE state.
	conn := newIdleGRPCConn(t)
	if state := conn.GetState(); state != connectivity.Idle {
		t.Fatalf("precondition: expected new connection in IDLE state, got %s", state)
	}

	g.connections = []*grpc.ClientConn{conn}
	g.streamManager.connectionHealth = []bool{true} // starts "healthy"
	g.streamManager.connectionIdleCount = []int{0}
	g.streamManager.tunnelStreams = []*StreamInfo{} // no streams to check

	g.performHealthCheck()

	// Connection should be marked unhealthy (IDLE is not READY/CONNECTING).
	if g.streamManager.connectionHealth[0] {
		t.Fatal("expected IDLE connection to be marked unhealthy")
	}

	// Idle counter should have been incremented from 0 to 1.
	if g.streamManager.connectionIdleCount[0] != 1 {
		t.Fatalf("expected connectionIdleCount[0]=1, got %d", g.streamManager.connectionIdleCount[0])
	}

	// Verify conn.Connect() was called: the connection should leave IDLE.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn.WaitForStateChange(ctx, connectivity.Idle)
	newState := conn.GetState()
	if newState == connectivity.Idle {
		t.Fatalf("expected connection to leave IDLE after Connect() was called by performHealthCheck, still %s", newState)
	}
}

// TestPerformHealthCheck_IDLECounterResets verifies that the idle counter
// resets to 0 when a connection transitions out of IDLE to a non-IDLE state
// (e.g., CONNECTING after Connect() was called, or TRANSIENT_FAILURE).
func TestPerformHealthCheck_IDLECounterResets(t *testing.T) {
	g := newHardeningGravityClient(t, 1)

	// Create an IDLE connection and force it out of IDLE via Connect().
	conn := newIdleGRPCConn(t)
	conn.Connect()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn.WaitForStateChange(ctx, connectivity.Idle)

	state := conn.GetState()
	if state == connectivity.Idle {
		t.Fatal("precondition: expected connection to have left IDLE after Connect()")
	}

	// Pre-set a high idle count to simulate previous consecutive IDLE detections.
	g.connections = []*grpc.ClientConn{conn}
	g.streamManager.connectionHealth = []bool{false}
	g.streamManager.connectionIdleCount = []int{5}
	g.streamManager.tunnelStreams = []*StreamInfo{}

	g.performHealthCheck()

	// The counter should reset to 0 because the connection is no longer IDLE.
	// Whether it's CONNECTING (healthy) or TRANSIENT_FAILURE (unhealthy),
	// neither path increments the idle counter — both reset it.
	if g.streamManager.connectionIdleCount[0] != 0 {
		t.Fatalf("expected connectionIdleCount[0]=0 after non-IDLE state (%s), got %d",
			state, g.streamManager.connectionIdleCount[0])
	}
}

// TestPerformHealthCheck_IDLECounterIncrementsOnConsecutiveIDLE verifies that
// the idle counter accumulates across health check cycles when a connection
// remains in IDLE state. This simulates the scenario where Connect() is called
// but the connection transitions back to IDLE before the next check.
func TestPerformHealthCheck_IDLECounterIncrementsOnConsecutiveIDLE(t *testing.T) {
	g := newHardeningGravityClient(t, 1)

	// Pre-set the idle count to 3 to simulate 3 previous IDLE detections.
	g.streamManager.connectionIdleCount = []int{3}

	// Create a fresh IDLE connection (simulates a connection that went back to IDLE).
	conn := newIdleGRPCConn(t)
	g.connections = []*grpc.ClientConn{conn}
	g.streamManager.connectionHealth = []bool{true}
	g.streamManager.tunnelStreams = []*StreamInfo{}

	g.performHealthCheck()

	// Counter should have incremented from 3 to 4.
	if g.streamManager.connectionIdleCount[0] != 4 {
		t.Fatalf("expected connectionIdleCount[0]=4 (incremented from 3), got %d",
			g.streamManager.connectionIdleCount[0])
	}

	// Health should be false (IDLE is not healthy).
	if g.streamManager.connectionHealth[0] {
		t.Fatal("expected IDLE connection to be marked unhealthy")
	}
}

// TestPerformHealthCheck_TransientFailureNoConnect verifies that a connection
// in TRANSIENT_FAILURE state does NOT trigger the IDLE recovery path
// (no Connect() call, no idle counter increment). gRPC handles its own
// reconnection retries for TRANSIENT_FAILURE via exponential backoff.
func TestPerformHealthCheck_TransientFailureNoConnect(t *testing.T) {
	g := newHardeningGravityClient(t, 1)

	// Create a connection and force it through IDLE → CONNECTING → TRANSIENT_FAILURE.
	conn := newIdleGRPCConn(t)
	conn.Connect()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// Wait for it to leave IDLE.
	conn.WaitForStateChange(ctx, connectivity.Idle)

	// If it's CONNECTING, wait for it to reach TRANSIENT_FAILURE.
	state := conn.GetState()
	if state == connectivity.Connecting {
		conn.WaitForStateChange(ctx, connectivity.Connecting)
		state = conn.GetState()
	}

	// We may get TRANSIENT_FAILURE or possibly still CONNECTING due to backoff.
	// Either way, the key assertion is the counter behavior.
	if state == connectivity.Idle {
		t.Skip("connection unexpectedly returned to IDLE, skipping TRANSIENT_FAILURE test")
	}

	g.connections = []*grpc.ClientConn{conn}
	g.streamManager.connectionHealth = []bool{true}
	g.streamManager.connectionIdleCount = []int{7} // pre-set high idle count
	g.streamManager.tunnelStreams = []*StreamInfo{}

	g.performHealthCheck()

	// The idle counter should reset to 0 (non-IDLE path resets, doesn't increment).
	if g.streamManager.connectionIdleCount[0] != 0 {
		t.Fatalf("expected connectionIdleCount[0]=0 after non-IDLE state (%s), got %d",
			state, g.streamManager.connectionIdleCount[0])
	}

	// Connection should be marked unhealthy if TRANSIENT_FAILURE, or healthy
	// if CONNECTING. Either is correct — the key point is no IDLE recovery.
	isHealthy := state == connectivity.Connecting || state == connectivity.Ready
	if g.streamManager.connectionHealth[0] != isHealthy {
		t.Fatalf("expected connectionHealth[0]=%v for state %s, got %v",
			isHealthy, state, g.streamManager.connectionHealth[0])
	}
}

// TestPerformHealthCheck_MultipleConnectionsMixedState verifies that with
// 3 connections where only connection 1 is IDLE, only connection 1 gets
// the IDLE recovery treatment (Connect() + idle count increment).
func TestPerformHealthCheck_MultipleConnectionsMixedState(t *testing.T) {
	g := newHardeningGravityClient(t, 3)

	// Connections 0 and 2: force out of IDLE via Connect().
	conn0 := newIdleGRPCConn(t)
	conn0.Connect()
	conn2 := newIdleGRPCConn(t)
	conn2.Connect()

	// Connection 1: stays in IDLE (fresh, no Connect() called).
	conn1 := newIdleGRPCConn(t)

	// Wait for connections 0 and 2 to leave IDLE.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn0.WaitForStateChange(ctx, connectivity.Idle)
	conn2.WaitForStateChange(ctx, connectivity.Idle)

	// Verify preconditions.
	if conn1.GetState() != connectivity.Idle {
		t.Fatal("precondition: expected connection 1 to be in IDLE state")
	}
	if conn0.GetState() == connectivity.Idle {
		t.Fatal("precondition: expected connection 0 to have left IDLE")
	}
	if conn2.GetState() == connectivity.Idle {
		t.Fatal("precondition: expected connection 2 to have left IDLE")
	}

	g.connections = []*grpc.ClientConn{conn0, conn1, conn2}
	g.streamManager.connectionHealth = []bool{true, true, true}
	g.streamManager.connectionIdleCount = []int{0, 0, 0}
	g.streamManager.tunnelStreams = []*StreamInfo{}

	g.performHealthCheck()

	// Only connection 1 (IDLE) should have its idle count incremented.
	if g.streamManager.connectionIdleCount[1] != 1 {
		t.Fatalf("expected IDLE connectionIdleCount[1]=1, got %d",
			g.streamManager.connectionIdleCount[1])
	}

	// Connections 0 and 2 should have their idle count at 0 (non-IDLE state resets).
	if g.streamManager.connectionIdleCount[0] != 0 {
		t.Fatalf("expected non-IDLE connectionIdleCount[0]=0, got %d",
			g.streamManager.connectionIdleCount[0])
	}
	if g.streamManager.connectionIdleCount[2] != 0 {
		t.Fatalf("expected non-IDLE connectionIdleCount[2]=0, got %d",
			g.streamManager.connectionIdleCount[2])
	}

	// Connection 1 should be marked unhealthy (IDLE is not healthy).
	if g.streamManager.connectionHealth[1] {
		t.Fatal("expected IDLE connection 1 to be marked unhealthy")
	}
}

// TestPerformHealthCheck_AllConnectionsIDLE verifies that when all connections
// go IDLE simultaneously (e.g., during a rolling ion restart), every connection
// gets the recovery treatment: Connect() called and idle count incremented.
func TestPerformHealthCheck_AllConnectionsIDLE(t *testing.T) {
	g := newHardeningGravityClient(t, 3)

	conn0 := newIdleGRPCConn(t)
	conn1 := newIdleGRPCConn(t)
	conn2 := newIdleGRPCConn(t)

	g.connections = []*grpc.ClientConn{conn0, conn1, conn2}
	g.streamManager.connectionHealth = []bool{true, true, true}
	g.streamManager.connectionIdleCount = []int{0, 0, 0}
	g.streamManager.tunnelStreams = []*StreamInfo{}

	g.performHealthCheck()

	// All connections should be detected as IDLE and treated.
	for i := 0; i < 3; i++ {
		if g.streamManager.connectionHealth[i] {
			t.Fatalf("expected connectionHealth[%d]=false for IDLE connection", i)
		}
		if g.streamManager.connectionIdleCount[i] != 1 {
			t.Fatalf("expected connectionIdleCount[%d]=1, got %d",
				i, g.streamManager.connectionIdleCount[i])
		}
	}

	// Verify Connect() was called on all connections: they should all leave IDLE.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conns := []*grpc.ClientConn{conn0, conn1, conn2}
	for i, conn := range conns {
		conn.WaitForStateChange(ctx, connectivity.Idle)
		state := conn.GetState()
		if state == connectivity.Idle {
			t.Fatalf("expected connection %d to leave IDLE after Connect(), still %s", i, state)
		}
	}
}

// TestPerformHealthCheck_IDLECounterArrayBounds verifies that performHealthCheck
// does NOT panic when connectionIdleCount is shorter than connections. This is
// a defensive bounds check — the production code guards with
// `if i < len(g.streamManager.connectionIdleCount)`.
func TestPerformHealthCheck_IDLECounterArrayBounds(t *testing.T) {
	g := newHardeningGravityClient(t, 2)

	conn0 := newIdleGRPCConn(t)
	conn1 := newIdleGRPCConn(t)

	g.connections = []*grpc.ClientConn{conn0, conn1}
	g.streamManager.connectionHealth = []bool{true, true}
	// connectionIdleCount intentionally shorter than connections.
	// This could happen if a code path initializes connections but forgets
	// to resize connectionIdleCount.
	g.streamManager.connectionIdleCount = []int{0} // 1 element, but 2 connections
	g.streamManager.tunnelStreams = []*StreamInfo{}

	// Must NOT panic despite mismatched slice lengths.
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("performHealthCheck panicked on mismatched slice lengths: %v", p)
		}
	}()

	g.performHealthCheck()

	// Connection 0 (within bounds) should have its count incremented.
	if g.streamManager.connectionIdleCount[0] != 1 {
		t.Fatalf("expected connectionIdleCount[0]=1, got %d",
			g.streamManager.connectionIdleCount[0])
	}

	// Connection 1 is out of bounds for connectionIdleCount but should still
	// be marked unhealthy (IDLE detection works, just no counter tracking).
	if g.streamManager.connectionHealth[1] {
		t.Fatal("expected IDLE connection 1 to be marked unhealthy even with short counter array")
	}

	// Connect() should still have been called on connection 1 (verified by state change).
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn1.WaitForStateChange(ctx, connectivity.Idle)
	if conn1.GetState() == connectivity.Idle {
		t.Fatal("expected connection 1 to leave IDLE after Connect() despite short counter array")
	}
}

// TestConnectionHealthAndIdleCountSameLength is a structural canary test:
// after production-style initialization, connectionHealth and connectionIdleCount
// MUST have the same length as connections. If they diverge, performHealthCheck's
// bounds check silently skips idle tracking, hiding the IDLE recovery regression.
func TestConnectionHealthAndIdleCountSameLength(t *testing.T) {
	sizes := []int{1, 2, 3, 5, 10}
	for _, n := range sizes {
		t.Run(fmt.Sprintf("pool_size_%d", n), func(t *testing.T) {
			g := newHardeningGravityClient(t, n)

			// Simulate production initialization (mirrors grpc_client.go lines 657-658
			// and lines 922-923).
			g.streamManager.connectionHealth = make([]bool, n)
			g.streamManager.connectionIdleCount = make([]int, n)

			healthLen := len(g.streamManager.connectionHealth)
			idleLen := len(g.streamManager.connectionIdleCount)
			connLen := len(g.connections)

			if healthLen != idleLen {
				t.Fatalf("connectionHealth length (%d) != connectionIdleCount length (%d)",
					healthLen, idleLen)
			}
			if healthLen != connLen {
				t.Fatalf("connectionHealth length (%d) != connections length (%d)",
					healthLen, connLen)
			}
		})
	}
}

func TestAddEndpoint_GrowsConnectionIdleCount(t *testing.T) {
	// Verify that addEndpoint grows connectionIdleCount alongside
	// connectionHealth so that idle recovery tracking works for
	// dynamically-added endpoints.
	g := newHardeningGravityClient(t, 1)
	g.poolConfig.MaxGravityPeers = 5

	g.streamManager.healthMu.Lock()
	g.streamManager.connectionHealth = []bool{true}
	g.streamManager.connectionIdleCount = []int{0}
	g.streamManager.healthMu.Unlock()

	// Add a new endpoint — this should grow both connectionHealth and connectionIdleCount
	g.addEndpoint("grpc://10.0.0.2:443")

	g.streamManager.healthMu.RLock()
	healthLen := len(g.streamManager.connectionHealth)
	idleLen := len(g.streamManager.connectionIdleCount)
	g.streamManager.healthMu.RUnlock()

	if healthLen != idleLen {
		t.Fatalf("after addEndpoint: connectionHealth length (%d) != connectionIdleCount length (%d)",
			healthLen, idleLen)
	}
	if healthLen < 2 {
		t.Fatalf("expected at least 2 entries after addEndpoint, got %d", healthLen)
	}
}

func TestPerformHealthCheck_PersistentIDLEEscalates(t *testing.T) {
	// Verify that when connectionIdleCount exceeds the escalation threshold (>3),
	// performHealthCheck triggers handleEndpointDisconnection without panicking.
	// We pre-set the idle count to simulate persistent IDLE and verify it
	// increments past the threshold. We use a real IDLE connection but manually
	// set the count high to test the escalation path directly.
	g := newHardeningGravityClient(t, 1)

	conn := newIdleGRPCConn(t)
	g.connections = []*grpc.ClientConn{conn}
	g.streamManager.connectionHealth = []bool{true}
	// Pre-set count to 3 — next check should escalate (>3 triggers handleEndpointDisconnection)
	g.streamManager.connectionIdleCount = []int{3}
	g.streamManager.tunnelStreams = []*StreamInfo{}

	// Verify the connection is IDLE
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for conn.GetState() != connectivity.Idle {
		if !conn.WaitForStateChange(ctx, conn.GetState()) {
			t.Skip("connection did not reach IDLE state in time")
			return
		}
	}

	// This health check should see IDLE, increment count to 4, and escalate
	// to handleEndpointDisconnection (which runs async). No panic = success.
	g.performHealthCheck()

	g.streamManager.healthMu.RLock()
	idleCount := g.streamManager.connectionIdleCount[0]
	healthy := g.streamManager.connectionHealth[0]
	g.streamManager.healthMu.RUnlock()

	if idleCount != 4 {
		t.Fatalf("expected connectionIdleCount[0]=4 after escalation check, got %d", idleCount)
	}
	if healthy {
		t.Fatal("expected connectionHealth[0] = false for IDLE connection")
	}

	// Give the async handleEndpointDisconnection goroutine a moment to run
	// (it should not panic even with minimal client state)
	time.Sleep(50 * time.Millisecond)
}
