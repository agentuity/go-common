package gravity

import (
	"context"
	"io"
	"testing"
	"time"

	pb "github.com/agentuity/go-common/gravity/proto"
	"github.com/agentuity/go-common/logger"
	"google.golang.org/grpc/metadata"
)

type mockSessionStream struct {
	sent chan *pb.SessionMessage
}

func (m *mockSessionStream) Send(msg *pb.SessionMessage) error {
	m.sent <- msg
	return nil
}

func (m *mockSessionStream) Recv() (*pb.SessionMessage, error) {
	return nil, io.EOF
}

func (m *mockSessionStream) Header() (metadata.MD, error) {
	return nil, nil
}

func (m *mockSessionStream) Trailer() metadata.MD {
	return nil
}

func (m *mockSessionStream) CloseSend() error {
	return nil
}

func (m *mockSessionStream) Context() context.Context {
	return context.Background()
}

func (m *mockSessionStream) SendMsg(any) error {
	return nil
}

func (m *mockSessionStream) RecvMsg(any) error {
	return nil
}

func TestHandleGenericResponse_ErrorRoutesToSandboxPending(t *testing.T) {
	msgID := "sandbox-msg"
	resultChan := make(chan routeSandboxResult, 1)

	g := &GravityClient{
		logger:                 logger.NewTestLogger(),
		pending:                make(map[string]chan *pb.ProtocolResponse),
		pendingRouteSandbox:    map[string]chan routeSandboxResult{msgID: resultChan},
		pendingRouteDeployment: make(map[string]chan routeDeploymentResult),
		connectionIDChan:       make(chan string, 1),
	}

	g.handleGenericResponse(msgID, &pb.ProtocolResponse{Id: msgID, Success: false, Error: "boom"})

	select {
	case result := <-resultChan:
		if result.Error != "boom" {
			t.Fatalf("expected error 'boom', got '%s'", result.Error)
		}
	default:
		t.Fatal("expected route sandbox error result")
	}
}

func TestHandleGenericResponse_ErrorRoutesToDeploymentPending(t *testing.T) {
	msgID := "deployment-msg"
	resultChan := make(chan routeDeploymentResult, 1)

	g := &GravityClient{
		logger:                 logger.NewTestLogger(),
		pending:                make(map[string]chan *pb.ProtocolResponse),
		pendingRouteSandbox:    make(map[string]chan routeSandboxResult),
		pendingRouteDeployment: map[string]chan routeDeploymentResult{msgID: resultChan},
		connectionIDChan:       make(chan string, 1),
	}

	g.handleGenericResponse(msgID, &pb.ProtocolResponse{Id: msgID, Success: false, Error: "boom"})

	select {
	case result := <-resultChan:
		if result.Error != "boom" {
			t.Fatalf("expected error 'boom', got '%s'", result.Error)
		}
	default:
		t.Fatal("expected route deployment error result")
	}
}

func TestHandleGenericResponse_SuccessDoesNotRouteToSandbox(t *testing.T) {
	msgID := "success-msg"
	responseChan := make(chan *pb.ProtocolResponse, 1)
	resultChan := make(chan routeSandboxResult, 1)

	g := &GravityClient{
		logger:                 logger.NewTestLogger(),
		pending:                map[string]chan *pb.ProtocolResponse{msgID: responseChan},
		pendingRouteSandbox:    map[string]chan routeSandboxResult{msgID: resultChan},
		pendingRouteDeployment: make(map[string]chan routeDeploymentResult),
		connectionIDChan:       make(chan string, 1),
	}

	g.handleGenericResponse(msgID, &pb.ProtocolResponse{Id: msgID, Success: true})

	select {
	case response := <-responseChan:
		if !response.Success {
			t.Fatalf("expected success response")
		}
	default:
		t.Fatal("expected generic response")
	}

	select {
	case <-resultChan:
		t.Fatal("did not expect route sandbox response for success")
	default:
	}
}

func TestHandleGenericResponse_ErrorNotFoundAnywhere(t *testing.T) {
	g := &GravityClient{
		logger:                 logger.NewTestLogger(),
		pending:                make(map[string]chan *pb.ProtocolResponse),
		pendingRouteSandbox:    make(map[string]chan routeSandboxResult),
		pendingRouteDeployment: make(map[string]chan routeDeploymentResult),
		connectionIDChan:       make(chan string, 1),
	}

	g.handleGenericResponse("missing", &pb.ProtocolResponse{Id: "missing", Success: false, Error: "boom"})
}

func TestSendRouteSandboxRequest_ErrorFromServer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := &mockSessionStream{sent: make(chan *pb.SessionMessage, 1)}
	client := &GravityClient{
		logger:                 logger.NewTestLogger(),
		ctx:                    ctx,
		pending:                make(map[string]chan *pb.ProtocolResponse),
		pendingRouteSandbox:    make(map[string]chan routeSandboxResult),
		pendingRouteDeployment: make(map[string]chan routeDeploymentResult),
		streamManager:          &StreamManager{controlStreams: []pb.GravitySessionService_EstablishSessionClient{stream}},
		retryConfig:            DefaultRetryConfig(),
		circuitBreakers:        []*CircuitBreaker{NewCircuitBreaker(DefaultCircuitBreakerConfig())},
		sessionReady:           make(chan struct{}),
	}
	close(client.sessionReady)

	errChan := make(chan error, 1)
	go func() {
		_, err := client.SendRouteSandboxRequest("sandbox-1", "fd00::1", time.Second)
		errChan <- err
	}()

	msg := <-stream.sent
	client.pendingRouteSandboxMu.RLock()
	responseChan := client.pendingRouteSandbox[msg.Id]
	client.pendingRouteSandboxMu.RUnlock()
	if responseChan == nil {
		t.Fatal("expected pending route sandbox channel")
	}
	responseChan <- routeSandboxResult{Error: "not authorized"}

	if err := <-errChan; err == nil || err.Error() != "route sandbox request failed: not authorized" {
		t.Fatalf("expected error from server, got %v", err)
	}
}

func TestSendRouteSandboxRequest_SuccessFromServer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := &mockSessionStream{sent: make(chan *pb.SessionMessage, 1)}
	client := &GravityClient{
		logger:                 logger.NewTestLogger(),
		ctx:                    ctx,
		pending:                make(map[string]chan *pb.ProtocolResponse),
		pendingRouteSandbox:    make(map[string]chan routeSandboxResult),
		pendingRouteDeployment: make(map[string]chan routeDeploymentResult),
		streamManager:          &StreamManager{controlStreams: []pb.GravitySessionService_EstablishSessionClient{stream}},
		retryConfig:            DefaultRetryConfig(),
		circuitBreakers:        []*CircuitBreaker{NewCircuitBreaker(DefaultCircuitBreakerConfig())},
		sessionReady:           make(chan struct{}),
	}
	close(client.sessionReady)

	respChan := make(chan *pb.RouteSandboxResponse, 1)
	go func() {
		resp, err := client.SendRouteSandboxRequest("sandbox-1", "fd00::1", time.Second)
		if err != nil {
			respChan <- nil
			return
		}
		respChan <- resp
	}()

	msg := <-stream.sent
	client.pendingRouteSandboxMu.RLock()
	responseChan := client.pendingRouteSandbox[msg.Id]
	client.pendingRouteSandboxMu.RUnlock()
	if responseChan == nil {
		t.Fatal("expected pending route sandbox channel")
	}
	responseChan <- routeSandboxResult{Response: &pb.RouteSandboxResponse{Ip: "fd00::2"}}

	resp := <-respChan
	if resp == nil {
		t.Fatal("expected response from server")
	}
	if resp.Ip != "fd00::2" {
		t.Fatalf("expected response IP fd00::2, got %s", resp.Ip)
	}
}

func TestSessionReadiness_WaitBeforeReady(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := &GravityClient{
		ctx:          ctx,
		sessionReady: make(chan struct{}),
	}

	resultChan := make(chan error, 1)
	go func() {
		resultChan <- client.WaitForSession(time.Second)
	}()

	time.Sleep(10 * time.Millisecond)
	close(client.sessionReady)

	if err := <-resultChan; err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestSessionReadiness_AlreadyReady(t *testing.T) {
	client := &GravityClient{
		ctx:          context.Background(),
		sessionReady: make(chan struct{}),
	}
	close(client.sessionReady)

	if err := client.WaitForSession(10 * time.Millisecond); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestSessionReadiness_Timeout(t *testing.T) {
	client := &GravityClient{
		ctx:          context.Background(),
		sessionReady: make(chan struct{}),
	}

	if err := client.WaitForSession(10 * time.Millisecond); err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestSessionReadiness_ResetOnReconnect(t *testing.T) {
	client := &GravityClient{
		logger:            logger.NewTestLogger(),
		serverMetrics:     NewServerMetrics(),
		sessionReady:      make(chan struct{}),
		streamManager:     &StreamManager{},
		skipAutoReconnect: true,
		closed:            make(chan struct{}, 1),
		connected:         true,
	}
	close(client.sessionReady)
	previous := client.sessionReady

	client.handleServerDisconnection("test")

	if client.sessionReady == previous {
		t.Fatal("expected sessionReady to be reset")
	}

	select {
	case <-client.sessionReady:
		t.Fatal("expected sessionReady to be open after reset")
	default:
	}
}
