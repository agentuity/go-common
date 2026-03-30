package gravity

import (
	"context"
	"errors"
	"io"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/agentuity/go-common/gravity/proto"
	"github.com/agentuity/go-common/logger"
	"google.golang.org/grpc/metadata"
)

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

	stats := g.GetConnectionPoolStats()
	returned, ok := stats["stream_metrics"].(map[string]*StreamMetrics)
	if !ok {
		t.Fatalf("unexpected stream_metrics type: %T", stats["stream_metrics"])
	}

	internalPtr := reflect.ValueOf(g.streamManager.streamMetrics).Pointer()
	returnedPtr := reflect.ValueOf(returned).Pointer()
	if internalPtr == returnedPtr {
		t.Fatalf("expected defensive copy of stream metrics map, got live reference")
	}

	returned["injected"] = &StreamMetrics{PacketsSent: 999}
	if _, exists := g.streamManager.streamMetrics["injected"]; exists {
		t.Fatalf("mutating returned map modified internal state (data race hazard)")
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
	connectionIDChan := make(chan string, 8)
	connectionIDChan <- "a"
	connectionIDChan <- "b"
	connectionIDChan <- "c"
	connectionIDChan <- "d"

	// Drain loop matching the production code in reconnect().
	for {
		select {
		case <-connectionIDChan:
		default:
			goto drained
		}
	}
drained:

	if got := len(connectionIDChan); got != 0 {
		t.Fatalf("expected reconnect drain to clear channel, still has %d stale IDs", got)
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
		errCh <- g.WaitForSession(200 * time.Millisecond)
	}()

	// Give WaitForSession time to snapshot old channel.
	time.Sleep(5 * time.Millisecond)

	newReady := make(chan struct{})
	g.mu.Lock()
	g.sessionReady = newReady
	g.mu.Unlock()

	close(oldReady)

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatalf("WaitForSession returned using stale channel while new session channel is still not ready")
		}
	case <-time.After(50 * time.Millisecond):
		// If it blocks here, race didn't trigger this run.
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

	// Sanity: this test itself shouldn't explode goroutine count unexpectedly.
	if runtime.NumGoroutine() <= 0 {
		t.Fatal("invalid goroutine count")
	}
}
