package gravity

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
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
	g.handleSessionHelloResponse("session_hello", &pb.SessionHelloResponse{
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
