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
	"github.com/agentuity/go-common/gravity/provider"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type dataplaneMockProvider struct {
	mu      sync.Mutex
	packets [][]byte
	notify  chan []byte
}

func newDataplaneMockProvider() *dataplaneMockProvider {
	return &dataplaneMockProvider{notify: make(chan []byte, 256)}
}

func (m *dataplaneMockProvider) Configure(provider.Configuration) error { return nil }

func (m *dataplaneMockProvider) ProcessInPacket(payload []byte) {
	cp := append([]byte(nil), payload...)
	m.mu.Lock()
	m.packets = append(m.packets, cp)
	m.mu.Unlock()
	select {
	case m.notify <- cp:
	default:
	}
}

func (m *dataplaneMockProvider) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.packets)
}

type dataplaneMockTunnelStream struct {
	ctx context.Context

	sendErr atomic.Value

	sendMu sync.Mutex
	sent   []*pb.TunnelPacket

	recvCh chan *pb.TunnelPacket
	errCh  chan error

	closeSendCount atomic.Int64
}

var _ pb.GravitySessionService_StreamSessionPacketsClient = (*dataplaneMockTunnelStream)(nil)

func newDataplaneMockTunnelStream(ctx context.Context) *dataplaneMockTunnelStream {
	if ctx == nil {
		ctx = context.Background()
	}
	return &dataplaneMockTunnelStream{
		ctx:    ctx,
		recvCh: make(chan *pb.TunnelPacket, 256),
		errCh:  make(chan error, 32),
	}
}

func (m *dataplaneMockTunnelStream) Send(p *pb.TunnelPacket) error {
	if v := m.sendErr.Load(); v != nil {
		if err, ok := v.(error); ok && err != nil {
			return err
		}
	}
	cp := &pb.TunnelPacket{Data: append([]byte(nil), p.Data...), StreamId: p.StreamId, EnqueuedAtUs: p.EnqueuedAtUs}
	m.sendMu.Lock()
	m.sent = append(m.sent, cp)
	m.sendMu.Unlock()
	return nil
}

func (m *dataplaneMockTunnelStream) Recv() (*pb.TunnelPacket, error) {
	select {
	case p := <-m.recvCh:
		return p, nil
	case err := <-m.errCh:
		return nil, err
	case <-m.ctx.Done():
		return nil, status.Error(codes.Canceled, "context canceled")
	}
}

func (m *dataplaneMockTunnelStream) Header() (metadata.MD, error) { return nil, nil }
func (m *dataplaneMockTunnelStream) Trailer() metadata.MD         { return nil }
func (m *dataplaneMockTunnelStream) CloseSend() error {
	m.closeSendCount.Add(1)
	return nil
}
func (m *dataplaneMockTunnelStream) Context() context.Context { return m.ctx }
func (m *dataplaneMockTunnelStream) SendMsg(any) error        { return nil }
func (m *dataplaneMockTunnelStream) RecvMsg(any) error        { return nil }

func (m *dataplaneMockTunnelStream) sentCount() int {
	m.sendMu.Lock()
	defer m.sendMu.Unlock()
	return len(m.sent)
}

func (m *dataplaneMockTunnelStream) lastSent() *pb.TunnelPacket {
	m.sendMu.Lock()
	defer m.sendMu.Unlock()
	if len(m.sent) == 0 {
		return nil
	}
	return m.sent[len(m.sent)-1]
}

type dataplaneMockSessionClient struct {
	streams []pb.GravitySessionService_StreamSessionPacketsClient
	idx     int
	mu      sync.Mutex
}

func (m *dataplaneMockSessionClient) EstablishSession(context.Context, ...grpc.CallOption) (grpc.BidiStreamingClient[pb.SessionMessage, pb.SessionMessage], error) {
	return nil, errors.New("not implemented")
}

func (m *dataplaneMockSessionClient) StreamSessionPackets(context.Context, ...grpc.CallOption) (grpc.BidiStreamingClient[pb.TunnelPacket, pb.TunnelPacket], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.idx >= len(m.streams) {
		return nil, fmt.Errorf("no stream configured at index %d", m.idx)
	}
	s := m.streams[m.idx]
	m.idx++
	return s, nil
}

func (m *dataplaneMockSessionClient) GetDeploymentMetadata(context.Context, *pb.DeploymentMetadataRequest, ...grpc.CallOption) (*pb.DeploymentMetadataResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *dataplaneMockSessionClient) GetSandboxMetadata(context.Context, *pb.SandboxMetadataRequest, ...grpc.CallOption) (*pb.SandboxMetadataResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *dataplaneMockSessionClient) Identify(context.Context, *pb.IdentifyRequest, ...grpc.CallOption) (*pb.IdentifyResponse, error) {
	return nil, errors.New("not implemented")
}

func newTunnelDataplaneTestClient(t *testing.T, n int) (*GravityClient, *dataplaneMockProvider) {
	t.Helper()
	g := newHardeningGravityClient(t, n)
	p := newDataplaneMockProvider()
	g.provider = p
	g.inboundPackets = make(chan *PooledBuffer, 1000)
	g.outboundPackets = make(chan []byte, 1000)
	if g.streamManager.streamMetrics == nil {
		g.streamManager.streamMetrics = make(map[string]*StreamMetrics)
	}
	if g.endpointStreamIndices == nil {
		g.endpointStreamIndices = make(map[string][]int)
	}
	return g, p
}

func installStreams(g *GravityClient, streams ...*StreamInfo) {
	g.streamManager.tunnelStreams = streams
	g.streamManager.streamMetrics = make(map[string]*StreamMetrics)
	g.endpointStreamIndices = make(map[string][]int)
	for i, s := range streams {
		if s == nil {
			continue
		}
		g.streamManager.streamMetrics[s.streamID] = &StreamMetrics{}
		if s.connIndex >= 0 && s.connIndex < len(g.connectionURLs) {
			g.endpointStreamIndices[g.connectionURLs[s.connIndex]] = append(g.endpointStreamIndices[g.connectionURLs[s.connIndex]], i)
		}
	}
}

func waitUntil(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func reverseFlow(pkt []byte) []byte {
	r := append([]byte(nil), pkt...)
	copy(r[8:24], pkt[24:40])
	copy(r[24:40], pkt[8:24])
	r[40], r[41], r[42], r[43] = pkt[42], pkt[43], pkt[40], pkt[41]
	return r
}

func TestWritePacket_SendsToStream(t *testing.T) {
	t.Parallel()
	g, _ := newTunnelDataplaneTestClient(t, 1)
	stream := newDataplaneMockTunnelStream(g.ctx)
	installStreams(g, &StreamInfo{stream: stream, connIndex: 0, streamID: "s0", isHealthy: true, lastUsed: time.Now()})

	payload := []byte{0x60, 0, 0, 0, 0, 0, 6, 0}
	if err := g.WritePacket(payload); err != nil {
		t.Fatalf("WritePacket error: %v", err)
	}

	got := stream.lastSent()
	if got == nil || string(got.Data) != string(payload) {
		t.Fatalf("expected payload to be sent to tunnel stream")
	}
}

func TestWritePacket_RoundRobinAcrossStreams(t *testing.T) {
	t.Parallel()
	g, _ := newTunnelDataplaneTestClient(t, 1)
	g.streamManager.allocationStrategy = RoundRobin
	s0 := newDataplaneMockTunnelStream(g.ctx)
	s1 := newDataplaneMockTunnelStream(g.ctx)
	s2 := newDataplaneMockTunnelStream(g.ctx)
	installStreams(g,
		&StreamInfo{stream: s0, connIndex: 0, streamID: "s0", isHealthy: true, lastUsed: time.Now()},
		&StreamInfo{stream: s1, connIndex: 0, streamID: "s1", isHealthy: true, lastUsed: time.Now()},
		&StreamInfo{stream: s2, connIndex: 0, streamID: "s2", isHealthy: true, lastUsed: time.Now()},
	)

	for i := 0; i < 6; i++ {
		if err := g.WritePacket([]byte{0x60, 0, 0, 0, 0, 0, 6, byte(i)}); err != nil {
			t.Fatalf("WritePacket(%d) error: %v", i, err)
		}
	}

	if s0.sentCount() != 2 || s1.sentCount() != 2 || s2.sentCount() != 2 {
		t.Fatalf("expected 2 sends per stream, got s0=%d s1=%d s2=%d", s0.sentCount(), s1.sentCount(), s2.sentCount())
	}
}

func TestWritePacket_SkipsDeadStreams(t *testing.T) {
	t.Parallel()
	g, _ := newTunnelDataplaneTestClient(t, 1)
	g.streamManager.allocationStrategy = RoundRobin
	live := newDataplaneMockTunnelStream(g.ctx)
	installStreams(g,
		nil,
		&StreamInfo{stream: live, connIndex: 0, streamID: "live", isHealthy: true, lastUsed: time.Now()},
	)

	if err := g.WritePacket([]byte{0x60, 0, 0, 0, 0, 0, 6, 1}); err != nil {
		t.Fatalf("expected dead stream to be skipped, got error: %v", err)
	}
	if live.sentCount() != 1 {
		t.Fatalf("expected live stream to receive packet, got %d sends", live.sentCount())
	}
}

func TestWritePacket_ErrorOnNoStreams(t *testing.T) {
	t.Parallel()
	g, _ := newTunnelDataplaneTestClient(t, 1)
	g.streamManager.tunnelStreams = nil
	if err := g.WritePacket([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected error when no tunnel streams are available")
	}
}

func TestWritePacket_ConcurrentSafe(t *testing.T) {
	t.Parallel()
	g, _ := newTunnelDataplaneTestClient(t, 1)
	s0 := newDataplaneMockTunnelStream(g.ctx)
	s1 := newDataplaneMockTunnelStream(g.ctx)
	s2 := newDataplaneMockTunnelStream(g.ctx)
	s3 := newDataplaneMockTunnelStream(g.ctx)
	installStreams(g,
		&StreamInfo{stream: s0, connIndex: 0, streamID: "s0", isHealthy: true, lastUsed: time.Now()},
		&StreamInfo{stream: s1, connIndex: 0, streamID: "s1", isHealthy: true, lastUsed: time.Now()},
		&StreamInfo{stream: s2, connIndex: 0, streamID: "s2", isHealthy: true, lastUsed: time.Now()},
		&StreamInfo{stream: s3, connIndex: 0, streamID: "s3", isHealthy: true, lastUsed: time.Now()},
	)

	const workers = 100
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errCh <- g.WritePacket([]byte{0x60, 0, 0, 0, 0, 0, 6, byte(i)})
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent WritePacket failed: %v", err)
		}
	}
	total := s0.sentCount() + s1.sentCount() + s2.sentCount() + s3.sentCount()
	if total != workers {
		t.Fatalf("expected %d packets sent, got %d", workers, total)
	}
}

func TestHandleTunnelStream_ReceivesPacket(t *testing.T) {
	t.Parallel()
	g, _ := newTunnelDataplaneTestClient(t, 1)
	g.inboundPackets = make(chan *PooledBuffer, 2)
	stream := newDataplaneMockTunnelStream(g.ctx)
	go g.handleTunnelStream(0, stream, "s0")

	pkt := makeIPv6Packet()
	stream.recvCh <- &pb.TunnelPacket{Data: pkt}

	// Consume the packet before sending the stop error. Sending both
	// to recvCh and errCh simultaneously causes a race in the mock
	// Recv()'s select — Go may pick the error first and exit the loop.
	select {
	case got := <-g.inboundPackets:
		if string(got.Buffer[:got.Length]) != string(pkt) {
			t.Fatalf("received payload mismatch")
		}
		g.returnBuffer(got)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for inbound packet")
	}
	stream.errCh <- errors.New("stop")
}

func TestHandleTunnelStream_DeliveryToProvider(t *testing.T) {
	t.Parallel()
	g, p := newTunnelDataplaneTestClient(t, 1)
	connCtx, cancel := context.WithCancel(g.ctx)
	g.connectionCtx = connCtx
	g.connectionCancel = cancel
	defer cancel()

	go g.handleInboundPackets()
	stream := newDataplaneMockTunnelStream(g.ctx)
	go g.handleTunnelStream(0, stream, "s0")

	pkt := makeIPv6Packet()
	stream.recvCh <- &pb.TunnelPacket{Data: pkt}

	select {
	case got := <-p.notify:
		if string(got) != string(pkt) {
			t.Fatalf("provider received wrong packet")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for provider delivery")
	}
	stream.errCh <- errors.New("stop")
}

func TestHandleTunnelStream_KeepaliveNotDelivered(t *testing.T) {
	t.Parallel()
	g, p := newTunnelDataplaneTestClient(t, 1)
	g.inboundPackets = make(chan *PooledBuffer, 1)
	stream := newDataplaneMockTunnelStream(g.ctx)
	go g.handleTunnelStream(0, stream, "s0")

	stream.recvCh <- &pb.TunnelPacket{Data: append([]byte(nil), TunnelKeepaliveMarker...)}
	stream.errCh <- errors.New("stop")

	// Bounded select: fails immediately if a keepalive packet is
	// incorrectly delivered; succeeds after timeout with no delivery.
	select {
	case <-g.inboundPackets:
		t.Fatal("keepalive packet should not be enqueued")
	case <-time.After(100 * time.Millisecond):
		// Good — no packet delivered within the window.
	}
	if p.Count() != 0 {
		t.Fatal("keepalive packet should not be delivered to provider")
	}
}

func TestHandleTunnelStream_ChannelFullDropsPacket(t *testing.T) {
	t.Parallel()
	g, _ := newTunnelDataplaneTestClient(t, 1)
	g.inboundPackets = make(chan *PooledBuffer, 1)
	g.inboundPackets <- &PooledBuffer{Buffer: []byte{1, 2, 3}, Length: 3}

	stream := newDataplaneMockTunnelStream(g.ctx)
	go g.handleTunnelStream(0, stream, "s0")
	stream.recvCh <- &pb.TunnelPacket{Data: makeIPv6Packet()}

	// Wait for the packet to be processed (dropped) before sending the
	// stop error. Sending both to recvCh and errCh simultaneously causes
	// a race in the mock Recv()'s select — Go may pick the error first,
	// causing handleTunnelStream to exit before processing the packet.
	waitUntil(t, time.Second, func() bool { return g.inboundDropped.Load() == 1 })
	stream.errCh <- errors.New("stop")

	if len(g.inboundPackets) != 1 {
		t.Fatal("full channel should keep original packet and drop incoming packet")
	}
}

func TestHandleTunnelStream_StreamErrorTriggersReconnect(t *testing.T) {
	t.Parallel()
	g, _ := newTunnelDataplaneTestClient(t, 2)
	g.multiEndpointMode.Store(true)
	g.streamManager.controlStreams[0] = &configurableMockStream{}
	s := newDataplaneMockTunnelStream(g.ctx)
	installStreams(g, &StreamInfo{stream: s, connIndex: 0, streamID: "s0", isHealthy: true, lastUsed: time.Now()})

	done := make(chan struct{})
	go func() {
		defer close(done)
		g.handleTunnelStream(0, s, "s0")
	}()
	s.errCh <- io.EOF

	waitUntil(t, time.Second, func() bool {
		g.streamManager.controlMu.RLock()
		defer g.streamManager.controlMu.RUnlock()
		return g.streamManager.controlStreams[0] == nil
	})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handleTunnelStream did not exit")
	}
	if !g.endpointReconnecting[0].Load() {
		t.Fatal("expected endpoint reconnection to be triggered")
	}
	g.cancel()
}

func TestHandleTunnelStream_CancelledContextExits(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	g, _ := newTunnelDataplaneTestClient(t, 1)
	g.mu.Lock()
	g.closing = true
	g.mu.Unlock()
	stream := newDataplaneMockTunnelStream(ctx)

	done := make(chan struct{})
	go func() {
		defer close(done)
		g.handleTunnelStream(0, stream, "s0")
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected handleTunnelStream to exit on context cancellation")
	}
}

func TestStreamManager_RegisterStream(t *testing.T) {
	t.Parallel()
	g, _ := newTunnelDataplaneTestClient(t, 1)
	g.poolConfig.StreamsPerConnection = 2
	g.sessionClients = []pb.GravitySessionServiceClient{
		&dataplaneMockSessionClient{streams: []pb.GravitySessionService_StreamSessionPacketsClient{
			newDataplaneMockTunnelStream(g.ctx),
			newDataplaneMockTunnelStream(g.ctx),
		}},
	}
	g.streamManager.controlStreams = []pb.GravitySessionService_EstablishSessionClient{&mockControlStream{ctx: g.ctx}}
	g.helloAckedStreams.Store(0, true)
	go func() {
		time.Sleep(20 * time.Millisecond)
		g.connectionIDChan <- "machine-1"
	}()

	if err := g.establishTunnelStreams(); err != nil {
		t.Fatalf("establishTunnelStreams error: %v", err)
	}
	if len(g.streamManager.tunnelStreams) != 2 {
		t.Fatalf("expected 2 tunnel stream slots, got %d", len(g.streamManager.tunnelStreams))
	}
	if g.streamManager.tunnelStreams[0] == nil || g.streamManager.tunnelStreams[1] == nil {
		t.Fatal("expected tunnel stream slots to be registered")
	}
}

func TestStreamManager_StreamMetricsTracked(t *testing.T) {
	t.Parallel()
	g, _ := newTunnelDataplaneTestClient(t, 1)
	stream := newDataplaneMockTunnelStream(g.ctx)
	installStreams(g, &StreamInfo{stream: stream, connIndex: 0, streamID: "s0", isHealthy: true, lastUsed: time.Now()})

	go g.handleTunnelStream(0, stream, "s0")
	pkt := makeIPv6Packet()
	stream.recvCh <- &pb.TunnelPacket{Data: pkt}
	waitUntil(t, time.Second, func() bool { return g.inboundReceived.Load() >= 1 })
	stream.errCh <- errors.New("stop")

	waitUntil(t, time.Second, func() bool {
		g.streamManager.metricsMu.RLock()
		m := g.streamManager.streamMetrics["s0"]
		g.streamManager.metricsMu.RUnlock()
		return m != nil && m.PacketsReceived == 1
	})

	g.streamManager.metricsMu.RLock()
	m := g.streamManager.streamMetrics["s0"]
	g.streamManager.metricsMu.RUnlock()
	if m.BytesReceived != int64(len(pkt)) || m.LastRecvUs == 0 {
		t.Fatalf("expected receive metrics updated, got bytes=%d lastRecvUs=%d", m.BytesReceived, m.LastRecvUs)
	}
}

func TestStreamManager_DeadStreamDetection(t *testing.T) {
	t.Parallel()
	g, _ := newTunnelDataplaneTestClient(t, 1)
	stream := newDataplaneMockTunnelStream(g.ctx)
	installStreams(g, &StreamInfo{stream: stream, connIndex: 0, streamID: "dead", isHealthy: true, lastUsed: time.Now()})

	stream.errCh <- io.EOF
	g.handleTunnelStream(0, stream, "dead")

	g.streamManager.tunnelMu.RLock()
	defer g.streamManager.tunnelMu.RUnlock()
	if g.streamManager.tunnelStreams[0].isHealthy {
		t.Fatal("expected stream to be marked unhealthy after receive error")
	}
}

func TestStreamManager_StreamReplacedOnReconnect(t *testing.T) {
	t.Parallel()
	g, _ := newTunnelDataplaneTestClient(t, 2)
	old := newDataplaneMockTunnelStream(g.ctx)
	installStreams(g,
		&StreamInfo{stream: old, connIndex: 0, streamID: "old", isHealthy: true, lastUsed: time.Now()},
		&StreamInfo{stream: newDataplaneMockTunnelStream(g.ctx), connIndex: 1, streamID: "other", isHealthy: true, lastUsed: time.Now()},
	)

	g.disconnectEndpointStreams(0)
	newS := newDataplaneMockTunnelStream(g.ctx)
	g.streamManager.tunnelMu.Lock()
	g.streamManager.tunnelStreams[0] = &StreamInfo{stream: newS, connIndex: 0, streamID: "new", isHealthy: true, lastUsed: time.Now()}
	g.streamManager.tunnelMu.Unlock()

	if g.streamManager.tunnelStreams[0].streamID != "new" {
		t.Fatal("expected old stream to be replaced with new stream")
	}
}

func TestReconnect_OldStreamsClose(t *testing.T) {
	t.Parallel()
	g, _ := newTunnelDataplaneTestClient(t, 2)
	old0 := newDataplaneMockTunnelStream(g.ctx)
	old1 := newDataplaneMockTunnelStream(g.ctx)
	other := newDataplaneMockTunnelStream(g.ctx)
	installStreams(g,
		&StreamInfo{stream: old0, connIndex: 0, streamID: "old0", isHealthy: true, lastUsed: time.Now()},
		&StreamInfo{stream: old1, connIndex: 0, streamID: "old1", isHealthy: true, lastUsed: time.Now()},
		&StreamInfo{stream: other, connIndex: 1, streamID: "other", isHealthy: true, lastUsed: time.Now()},
	)

	g.disconnectEndpointStreams(0)

	if old0.closeSendCount.Load() == 0 || old1.closeSendCount.Load() == 0 {
		t.Fatal("expected old endpoint streams to be closed on reconnect")
	}
	if other.closeSendCount.Load() != 0 {
		t.Fatal("expected non-target endpoint stream to remain open")
	}
}

func TestReconnect_NewStreamsEstablished(t *testing.T) {
	t.Parallel()
	g, _ := newTunnelDataplaneTestClient(t, 2)
	installStreams(g,
		&StreamInfo{stream: newDataplaneMockTunnelStream(g.ctx), connIndex: 0, streamID: "old", isHealthy: true, lastUsed: time.Now()},
		&StreamInfo{stream: newDataplaneMockTunnelStream(g.ctx), connIndex: 1, streamID: "keep", isHealthy: true, lastUsed: time.Now()},
	)

	g.disconnectEndpointStreams(0)
	g.streamManager.tunnelMu.Lock()
	g.streamManager.tunnelStreams[0] = &StreamInfo{stream: newDataplaneMockTunnelStream(g.ctx), connIndex: 0, streamID: "new", isHealthy: true, lastUsed: time.Now()}
	g.streamManager.tunnelMu.Unlock()
	g.rebuildEndpointStreamIndices()

	if got := g.streamManager.tunnelStreams[0].streamID; got != "new" {
		t.Fatalf("expected new stream ID after reconnect, got %s", got)
	}
}

func TestReconnect_PacketsFlowThroughNewStreams(t *testing.T) {
	t.Parallel()
	g, _ := newTunnelDataplaneTestClient(t, 1)
	old := newDataplaneMockTunnelStream(g.ctx)
	installStreams(g, &StreamInfo{stream: old, connIndex: 0, streamID: "old", isHealthy: true, lastUsed: time.Now()})

	g.disconnectEndpointStreams(0)
	newS := newDataplaneMockTunnelStream(g.ctx)
	g.streamManager.tunnelMu.Lock()
	g.streamManager.tunnelStreams[0] = &StreamInfo{stream: newS, connIndex: 0, streamID: "new", isHealthy: true, lastUsed: time.Now()}
	g.streamManager.tunnelMu.Unlock()

	if err := g.WritePacket(makeIPv6Packet()); err != nil {
		t.Fatalf("WritePacket failed after reconnect: %v", err)
	}
	if newS.sentCount() != 1 || old.sentCount() != 0 {
		t.Fatalf("expected packet to flow through new stream only, old=%d new=%d", old.sentCount(), newS.sentCount())
	}
}

func TestReconnect_InboundContinuesAfterReconnect(t *testing.T) {
	t.Parallel()
	g, p := newTunnelDataplaneTestClient(t, 1)
	connCtx, cancel := context.WithCancel(g.ctx)
	g.connectionCtx = connCtx
	g.connectionCancel = cancel
	defer cancel()
	go g.handleInboundPackets()

	old := newDataplaneMockTunnelStream(g.ctx)
	go g.handleTunnelStream(0, old, "old")
	old.errCh <- errors.New("old stream closed")

	newS := newDataplaneMockTunnelStream(g.ctx)
	go g.handleTunnelStream(0, newS, "new")
	pkt := makeIPv6Packet()
	newS.recvCh <- &pb.TunnelPacket{Data: pkt}

	select {
	case got := <-p.notify:
		if string(got) != string(pkt) {
			t.Fatal("provider received wrong packet after reconnect")
		}
	case <-time.After(time.Second):
		t.Fatal("expected inbound packets to continue on new stream")
	}
	newS.errCh <- errors.New("stop")
}

func TestReconnect_NoPacketLossDuringSwitch(t *testing.T) {
	t.Parallel()
	g, _ := newTunnelDataplaneTestClient(t, 1)
	newS := newDataplaneMockTunnelStream(g.ctx)
	installStreams(g, &StreamInfo{stream: newS, connIndex: 0, streamID: "new", isHealthy: true, lastUsed: time.Now()})

	const packets = 25
	for i := 0; i < packets; i++ {
		if err := g.SendPacket([]byte{0x60, 0, 0, 0, 0, 0, 6, byte(i)}); err != nil {
			t.Fatalf("SendPacket enqueue failed: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(g.ctx)
	g.connectionCtx = ctx
	g.connectionCancel = cancel
	go g.handleOutboundPackets()
	waitUntil(t, time.Second, func() bool { return newS.sentCount() == packets })
	cancel()
}

func TestMultiEndpoint_StreamsPerEndpoint(t *testing.T) {
	t.Parallel()
	g, _ := newTunnelDataplaneTestClient(t, 2)
	installStreams(g,
		&StreamInfo{stream: newDataplaneMockTunnelStream(g.ctx), connIndex: 0, streamID: "ep0-a", isHealthy: true, lastUsed: time.Now()},
		&StreamInfo{stream: newDataplaneMockTunnelStream(g.ctx), connIndex: 0, streamID: "ep0-b", isHealthy: true, lastUsed: time.Now()},
		&StreamInfo{stream: newDataplaneMockTunnelStream(g.ctx), connIndex: 1, streamID: "ep1-a", isHealthy: true, lastUsed: time.Now()},
	)
	g.rebuildEndpointStreamIndices()

	if len(g.endpointStreamIndices[g.connectionURLs[0]]) != 2 {
		t.Fatal("expected two streams mapped to endpoint 0")
	}
	if len(g.endpointStreamIndices[g.connectionURLs[1]]) != 1 {
		t.Fatal("expected one stream mapped to endpoint 1")
	}
}

func TestMultiEndpoint_InboundFlowBinding(t *testing.T) {
	t.Parallel()
	g, _ := newTunnelDataplaneTestClient(t, 2)
	g.multiEndpointMode.Store(true)
	g.selector = NewEndpointSelector(30 * time.Second)

	s := newDataplaneMockTunnelStream(g.ctx)
	installStreams(g,
		&StreamInfo{stream: newDataplaneMockTunnelStream(g.ctx), connIndex: 0, streamID: "ep0", isHealthy: true, lastUsed: time.Now()},
		&StreamInfo{stream: s, connIndex: 1, streamID: "ep1", isHealthy: true, lastUsed: time.Now()},
	)

	go g.handleTunnelStream(1, s, "ep1")
	in := makeIPv6Packet()
	s.recvCh <- &pb.TunnelPacket{Data: in}

	// Wait for the packet to be processed before sending the stop error.
	// See TestHandleTunnelStream_ChannelFullDropsPacket for rationale.
	waitUntil(t, time.Second, func() bool { return g.inboundReceived.Load() >= 1 })
	s.errCh <- errors.New("stop")

	resp := reverseFlow(in)
	ep := g.selector.Select(resp, g.endpoints)
	if ep == nil || ep.URL != g.endpoints[1].URL {
		t.Fatalf("expected reverse flow bound to endpoint 1")
	}
}

func TestMultiEndpoint_EndpointHealthAffectsStreams(t *testing.T) {
	t.Parallel()
	g, _ := newTunnelDataplaneTestClient(t, 2)
	g.multiEndpointMode.Store(true)
	g.selector = NewEndpointSelector(30 * time.Second)
	g.endpoints[0].healthy.Store(false)
	g.endpoints[1].healthy.Store(true)

	dead := newDataplaneMockTunnelStream(g.ctx)
	live := newDataplaneMockTunnelStream(g.ctx)
	installStreams(g,
		&StreamInfo{stream: dead, connIndex: 0, streamID: "dead", isHealthy: true, lastUsed: time.Now()},
		&StreamInfo{stream: live, connIndex: 1, streamID: "live", isHealthy: true, lastUsed: time.Now()},
	)

	if err := g.WritePacket(makeIPv6Packet()); err != nil {
		t.Fatalf("WritePacket failed: %v", err)
	}
	if live.sentCount() == 0 {
		t.Fatal("expected healthy endpoint stream to be used")
	}
	if dead.sentCount() != 0 {
		t.Fatal("expected unhealthy endpoint stream to be skipped")
	}
}

func TestBufferPool_GetAndReturn(t *testing.T) {
	t.Parallel()
	g, _ := newTunnelDataplaneTestClient(t, 1)

	p := g.getBuffer([]byte("hello"))
	if p == nil || p.Length != 5 {
		t.Fatal("expected pooled buffer for payload")
	}
	first := &p.Buffer[0]
	g.returnBuffer(p)

	p2 := g.getBuffer([]byte("world"))
	if p2 == nil || p2.Length != 5 {
		t.Fatal("expected pooled buffer on second get")
	}
	second := &p2.Buffer[0]
	g.returnBuffer(p2)

	if first != second {
		t.Fatal("expected buffer to be recycled through pool")
	}
}

func TestBufferPool_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	g, _ := newTunnelDataplaneTestClient(t, 1)
	const workers = 100
	const loops = 100
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(seed byte) {
			defer wg.Done()
			for j := 0; j < loops; j++ {
				buf := g.getBuffer([]byte{seed, byte(j)})
				g.returnBuffer(buf)
			}
		}(byte(i))
	}
	wg.Wait()
}
