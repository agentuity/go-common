package gravity

import (
	"testing"
	"time"

	pb "github.com/agentuity/go-common/gravity/proto"
	"github.com/agentuity/go-common/gravity/provider"
	"github.com/agentuity/go-common/logger"
)

// TestGRPCGravityServerImplementsProviderServer verifies interface compliance
func TestGRPCGravityServerImplementsProviderServer(t *testing.T) {
	// This will fail at compile time if GRPCGravityServer doesn't implement provider.Server
	var _ provider.Server = (*GravityClient)(nil)
}

// TestWritePacketMethod tests the WritePacket method with no connection
func TestWritePacketMethodWithoutConnection(t *testing.T) {
	// Create a minimal GRPCGravityServer for testing (not connected)
	server := &GravityClient{
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
	server := &GravityClient{
		streamManager: &StreamManager{
			controlStreams: make([]pb.GravitySessionService_EstablishSessionClient, 0),
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
	server := &GravityClient{
		streamManager: &StreamManager{
			controlStreams: make([]pb.GravitySessionService_EstablishSessionClient, 0),
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
	server := &GravityClient{
		streamManager: &StreamManager{
			controlStreams: make([]pb.GravitySessionService_EstablishSessionClient, 0),
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

func TestExtractHostnameFromURL(t *testing.T) {
	var c GravityClient
	c.logger = logger.NewTestLogger()

	// Default fallback when no custom server name is set
	val, err := extractHostnameFromGravityURL("grpc://127.0.0.1", "")
	if err != nil {
		t.Fatal(err)
	}
	if val != "gravity.agentuity.com" {
		t.Errorf("Expected hostname to be gravity.agentuity.com, got %s", val)
	}

	// Custom fallback server name for IP addresses
	val, err = extractHostnameFromGravityURL("grpc://10.0.0.1", "custom-gravity.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if val != "custom-gravity.example.com" {
		t.Errorf("Expected hostname to be custom-gravity.example.com, got %s", val)
	}

	// Hostname URLs should ignore the fallback entirely
	val, err = c.extractHostnameFromURL("grpc://gravity.agentuity.io")
	if err != nil {
		t.Fatal(err)
	}
	if val != "gravity.agentuity.io" {
		t.Errorf("Expected hostname to be gravity.agentuity.io, got %s", val)
	}
}
