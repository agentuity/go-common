package gravity

import (
	"testing"
	"time"

	pb "github.com/agentuity/go-common/gravity/proto"
	"github.com/agentuity/go-common/gravity/provider"
)

// TestGRPCGravityServerImplementsProviderServer verifies interface compliance
func TestGRPCGravityServerImplementsProviderServer(t *testing.T) {
	// This will fail at compile time if GRPCGravityServer doesn't implement provider.Server
	var _ provider.Server = (*GRPCGravityServer)(nil)
}

// TestActivityMethod tests the Activity method functionality
func TestActivityMethod(t *testing.T) {
	// Create a minimal GRPCGravityServer for testing
	server := &GRPCGravityServer{
		activities: make([]*pb.HTTPEvent, 0),
	}

	// Test activity using protobuf type
	event := &pb.HTTPEvent{
		Method:       "GET",
		Path:         "/test",
		Status:       200,
		Started:      time.Now().UnixMilli(),
		Duration:     100,
		DeploymentId: "test-deployment",
		SessionId:    "test-session",
		AgentId:      "test-agent",
	}

	// Call Activity method
	server.Activity(event)

	// Verify activity was stored
	server.activityMu.Lock()
	defer server.activityMu.Unlock()

	if len(server.activities) != 1 {
		t.Errorf("Expected 1 activity, got %d", len(server.activities))
	}

	if server.activities[0].Method != "GET" {
		t.Errorf("Expected method GET, got %s", server.activities[0].Method)
	}

	if server.activities[0].Path != "/test" {
		t.Errorf("Expected path /test, got %s", server.activities[0].Path)
	}
}

// TestWritePacketMethod tests the WritePacket method with no connection
func TestWritePacketMethodWithoutConnection(t *testing.T) {
	// Create a minimal GRPCGravityServer for testing (not connected)
	server := &GRPCGravityServer{
		connected: false,
		closing:   false,
	}

	// Test WritePacket with no connection should return error
	err := server.WritePacket([]byte("test packet"))

	if err != ErrConnectionClosed {
		t.Errorf("Expected ErrConnectionClosed, got %v", err)
	}
}

// TestUnprovisionMethod tests the Unprovision method with no streams
func TestUnprovisionMethodWithoutStreams(t *testing.T) {
	// Create a minimal GRPCGravityServer for testing (no streams)
	server := &GRPCGravityServer{
		streamManager: &StreamManager{
			controlStreams: make([]pb.GravityTunnel_EstablishTunnelClient, 0),
		},
	}

	// Test Unprovision with no streams should return error
	err := server.Unprovision("test-deployment")

	if err == nil {
		t.Error("Expected error for unprovision with no control streams")
	}

	expectedMsg := "no control streams available"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}
}

// TestPauseMethod tests the Pause method with no streams
func TestPauseMethodWithoutStreams(t *testing.T) {
	// Create a minimal GRPCGravityServer for testing (no streams)
	server := &GRPCGravityServer{
		streamManager: &StreamManager{
			controlStreams: make([]pb.GravityTunnel_EstablishTunnelClient, 0),
		},
	}

	// Test Pause with no streams should return error
	err := server.Pause("")

	if err == nil {
		t.Error("Expected error for pause with no control streams")
	}

	expectedMsg := "no control streams available"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}
}

// TestResumeMethod tests the Resume method with no streams
func TestResumeMethodWithoutStreams(t *testing.T) {
	// Create a minimal GRPCGravityServer for testing (no streams)
	server := &GRPCGravityServer{
		streamManager: &StreamManager{
			controlStreams: make([]pb.GravityTunnel_EstablishTunnelClient, 0),
		},
	}

	// Test Resume with no streams should return error
	err := server.Resume("")

	if err == nil {
		t.Error("Expected error for resume with no control streams")
	}

	expectedMsg := "no control streams available"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}
}

// TestSelectOptimalTunnelStreamNoStreams tests stream selection with no streams
func TestSelectOptimalTunnelStreamNoStreams(t *testing.T) {
	manager := &StreamManager{
		tunnelStreams: make([]*StreamInfo, 0),
	}

	stream := manager.selectOptimalTunnelStream()

	if stream != nil {
		t.Error("Expected nil stream when no tunnel streams available")
	}
}

// TestSelectOptimalTunnelStreamWithUnhealthyStreams tests stream selection with unhealthy streams
func TestSelectOptimalTunnelStreamWithUnhealthyStreams(t *testing.T) {
	manager := &StreamManager{
		tunnelStreams: []*StreamInfo{
			{
				streamID:  "stream1",
				isHealthy: false,
				loadCount: 0,
			},
			{
				streamID:  "stream2",
				isHealthy: false,
				loadCount: 1,
			},
		},
	}

	stream := manager.selectOptimalTunnelStream()

	if stream != nil {
		t.Error("Expected nil stream when all tunnel streams are unhealthy")
	}
}

// TestSelectOptimalTunnelStreamWithHealthyStreams tests stream selection with healthy streams
func TestSelectOptimalTunnelStreamWithHealthyStreams(t *testing.T) {
	manager := &StreamManager{
		tunnelStreams: []*StreamInfo{
			{
				streamID:  "stream1",
				isHealthy: true,
				loadCount: 5,
				lastUsed:  time.Now(),
			},
			{
				streamID:  "stream2",
				isHealthy: true,
				loadCount: 2, // Lower load, should be selected
				lastUsed:  time.Now(),
			},
			{
				streamID:  "stream3",
				isHealthy: false,
				loadCount: 0, // Unhealthy, should be ignored
			},
		},
	}

	stream := manager.selectOptimalTunnelStream()

	if stream == nil {
		t.Fatal("Expected to get a stream when healthy streams are available")
	}

	if stream.streamID != "stream2" {
		t.Errorf("Expected stream2 (lowest load), got %s", stream.streamID)
	}

	if stream.loadCount != 3 { // Should be incremented from 2 to 3
		t.Errorf("Expected load count to be incremented to 3, got %d", stream.loadCount)
	}
}
