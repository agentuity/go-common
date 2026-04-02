package gravity

import (
	"context"
	"errors"
	"testing"

	pb "github.com/agentuity/go-common/gravity/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type mockSessionClient struct {
	establishErr     error
	streamPacketsErr error

	establishCalls     int
	streamPacketsCalls int
}

func (m *mockSessionClient) EstablishSession(ctx context.Context, opts ...grpc.CallOption) (grpc.BidiStreamingClient[pb.SessionMessage, pb.SessionMessage], error) {
	m.establishCalls++
	if m.establishErr != nil {
		return nil, m.establishErr
	}
	return &mockControlStream{ctx: ctx}, nil
}

func (m *mockSessionClient) StreamSessionPackets(ctx context.Context, opts ...grpc.CallOption) (grpc.BidiStreamingClient[pb.TunnelPacket, pb.TunnelPacket], error) {
	m.streamPacketsCalls++
	if m.streamPacketsErr != nil {
		return nil, m.streamPacketsErr
	}
	return &mockTunnelStream{ctx: ctx}, nil
}

func (m *mockSessionClient) GetDeploymentMetadata(context.Context, *pb.DeploymentMetadataRequest, ...grpc.CallOption) (*pb.DeploymentMetadataResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockSessionClient) GetSandboxMetadata(context.Context, *pb.SandboxMetadataRequest, ...grpc.CallOption) (*pb.SandboxMetadataResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockSessionClient) Identify(context.Context, *pb.IdentifyRequest, ...grpc.CallOption) (*pb.IdentifyResponse, error) {
	return nil, errors.New("not implemented")
}

type mockControlStream struct {
	ctx context.Context
}

func (m *mockControlStream) Send(*pb.SessionMessage) error { return nil }

func (m *mockControlStream) Recv() (*pb.SessionMessage, error) {
	<-m.ctx.Done()
	return nil, status.Error(codes.Canceled, "test context canceled")
}

func (m *mockControlStream) Header() (metadata.MD, error) { return nil, nil }
func (m *mockControlStream) Trailer() metadata.MD         { return nil }
func (m *mockControlStream) CloseSend() error             { return nil }
func (m *mockControlStream) Context() context.Context     { return m.ctx }
func (m *mockControlStream) SendMsg(any) error            { return nil }
func (m *mockControlStream) RecvMsg(any) error            { return nil }

type mockTunnelStream struct {
	ctx context.Context
}

func (m *mockTunnelStream) Send(*pb.TunnelPacket) error { return nil }

func (m *mockTunnelStream) Recv() (*pb.TunnelPacket, error) {
	<-m.ctx.Done()
	return nil, status.Error(codes.Canceled, "test context canceled")
}

func (m *mockTunnelStream) Header() (metadata.MD, error) { return nil, nil }
func (m *mockTunnelStream) Trailer() metadata.MD         { return nil }
func (m *mockTunnelStream) CloseSend() error             { return nil }
func (m *mockTunnelStream) Context() context.Context     { return m.ctx }
func (m *mockTunnelStream) SendMsg(any) error            { return nil }
func (m *mockTunnelStream) RecvMsg(any) error            { return nil }

func newResilienceTestClient(t *testing.T, n int, multi bool) *GravityClient {
	t.Helper()
	g := newEndpointTestClient(t, n)
	g.multiEndpointMode.Store(multi)
	t.Cleanup(func() {
		g.mu.Lock()
		g.closing = true
		g.mu.Unlock()
		if g.cancel != nil {
			g.cancel()
		}
	})
	return g
}

func countNonNilControlStreams(g *GravityClient) int {
	g.streamManager.controlMu.RLock()
	defer g.streamManager.controlMu.RUnlock()
	count := 0
	for _, stream := range g.streamManager.controlStreams {
		if stream != nil {
			count++
		}
	}
	return count
}

func TestEstablishControlStreams_AllSucceed(t *testing.T) {
	g := newResilienceTestClient(t, 3, true)
	g.sessionClients = []pb.GravitySessionServiceClient{
		&mockSessionClient{},
		&mockSessionClient{},
		&mockSessionClient{},
	}

	err := g.establishControlStreams()
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if got := countNonNilControlStreams(g); got != 3 {
		t.Fatalf("expected 3 control streams, got %d", got)
	}

	for i := range g.endpointReconnecting {
		if g.endpointReconnecting[i].Load() {
			t.Fatalf("expected endpoint %d reconnecting=false", i)
		}
	}
}

func TestEstablishControlStreams_PartialFailure(t *testing.T) {
	g := newResilienceTestClient(t, 6, true)
	g.sessionClients = []pb.GravitySessionServiceClient{
		&mockSessionClient{},
		&mockSessionClient{establishErr: errors.New("endpoint-1 down")},
		&mockSessionClient{},
		&mockSessionClient{establishErr: errors.New("endpoint-3 down")},
		&mockSessionClient{},
		&mockSessionClient{establishErr: errors.New("endpoint-5 down")},
	}

	err := g.establishControlStreams()
	if err != nil {
		t.Fatalf("expected partial success, got error: %v", err)
	}

	if got := countNonNilControlStreams(g); got != 3 {
		t.Fatalf("expected 3 established control streams, got %d", got)
	}

	failed := map[int]bool{1: true, 3: true, 5: true}
	for i := range g.sessionClients {
		_, isFailed := failed[i]
		if isFailed {
			if g.streamManager.controlStreams[i] != nil {
				t.Fatalf("expected failed endpoint %d control stream=nil", i)
			}
			if g.sessionClients[i] != nil {
				t.Fatalf("expected failed endpoint %d session client=nil after disconnect", i)
			}
			if !g.endpointReconnecting[i].Load() {
				t.Fatalf("expected failed endpoint %d reconnecting=true", i)
			}
		} else {
			if g.streamManager.controlStreams[i] == nil {
				t.Fatalf("expected healthy endpoint %d control stream to be set", i)
			}
		}
	}
}

func TestEstablishControlStreams_AllFail(t *testing.T) {
	g := newResilienceTestClient(t, 3, true)
	g.sessionClients = []pb.GravitySessionServiceClient{
		&mockSessionClient{establishErr: errors.New("down-0")},
		&mockSessionClient{establishErr: errors.New("down-1")},
		&mockSessionClient{establishErr: errors.New("down-2")},
	}

	err := g.establishControlStreams()
	if err == nil {
		t.Fatalf("expected error when all control streams fail")
	}

	if got := countNonNilControlStreams(g); got != 0 {
		t.Fatalf("expected 0 control streams, got %d", got)
	}
}

func TestEstablishControlStreams_SingleEndpointPreservesOriginalBehavior(t *testing.T) {
	g := newResilienceTestClient(t, 1, false)
	g.sessionClients = []pb.GravitySessionServiceClient{
		&mockSessionClient{establishErr: errors.New("single endpoint down")},
	}

	err := g.establishControlStreams()
	if err == nil {
		t.Fatalf("expected error in single-endpoint mode")
	}
	if g.endpointReconnecting[0].Load() {
		t.Fatalf("expected single-endpoint reconnecting flag to remain false")
	}
}

func TestEstablishTunnelStreams_SkipsNilClients(t *testing.T) {
	g := newResilienceTestClient(t, 4, true)
	g.poolConfig.StreamsPerGravity = 2
	g.connectionIDChan = make(chan string)

	g.streamManager.controlStreams = []pb.GravitySessionService_EstablishSessionClient{
		&mockControlStream{ctx: g.ctx},
		nil,
		&mockControlStream{ctx: g.ctx},
		nil,
	}

	c0 := &mockSessionClient{}
	c2 := &mockSessionClient{}
	g.sessionClients = []pb.GravitySessionServiceClient{
		c0,
		nil,
		c2,
		nil,
	}

	go func() {
		g.connectionIDChan <- "machine-1"
		g.connectionIDChan <- "machine-1"
	}()

	err := g.establishTunnelStreams()
	if err != nil {
		t.Fatalf("expected success establishing tunnel streams with nil clients skipped, got: %v", err)
	}

	if c0.streamPacketsCalls != 2 {
		t.Fatalf("expected client 0 StreamSessionPackets calls=2, got %d", c0.streamPacketsCalls)
	}
	if c2.streamPacketsCalls != 2 {
		t.Fatalf("expected client 2 StreamSessionPackets calls=2, got %d", c2.streamPacketsCalls)
	}

	if got := len(g.streamManager.tunnelStreams); got != 4 {
		t.Fatalf("expected 4 tunnel streams, got %d", got)
	}
	for i, stream := range g.streamManager.tunnelStreams {
		if stream == nil {
			t.Fatalf("expected tunnel stream %d to be non-nil", i)
		}
	}
}
