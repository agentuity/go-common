package gravity

import (
	"testing"
	"time"
)

func TestConnectionPoolConfiguration(t *testing.T) {
	// Test default configuration
	config := ConnectionPoolConfig{
		PoolSize:             4,
		StreamsPerConnection: 2,
		AllocationStrategy:   HashBased,
		HealthCheckInterval:  30 * time.Second,
		FailoverTimeout:      5 * time.Second,
	}

	if config.PoolSize != 4 {
		t.Errorf("Expected PoolSize 4, got %d", config.PoolSize)
	}

	if config.StreamsPerConnection != 2 {
		t.Errorf("Expected StreamsPerConnection 2, got %d", config.StreamsPerConnection)
	}

	if config.AllocationStrategy != HashBased {
		t.Errorf("Expected HashBased strategy, got %v", config.AllocationStrategy)
	}
}

func TestStreamAllocationStrategies(t *testing.T) {
	strategies := []StreamAllocationStrategy{
		RoundRobin,
		HashBased,
		LeastConnections,
		WeightedRoundRobin,
	}

	expectedNames := []string{
		"RoundRobin",
		"HashBased",
		"LeastConnections",
		"WeightedRoundRobin",
	}

	for i, strategy := range strategies {
		if strategy.String() != expectedNames[i] {
			t.Errorf("Expected strategy name '%s', got '%s'", expectedNames[i], strategy.String())
		}
	}
}

func TestStreamSelection(t *testing.T) {
	// Mock stream manager for testing
	sm := &StreamManager{
		allocationStrategy: HashBased,
		tunnelStreams: []*StreamInfo{
			{streamID: "stream_0_0", isHealthy: true, loadCount: 0},
			{streamID: "stream_0_1", isHealthy: true, loadCount: 5},
			{streamID: "stream_1_0", isHealthy: false, loadCount: 0},
			{streamID: "stream_1_1", isHealthy: true, loadCount: 2},
		},
	}

	// Mock gRPC gravity server
	g := &GRPCGravityServer{
		streamManager: sm,
	}

	t.Run("RoundRobin", func(t *testing.T) {
		g.streamManager.allocationStrategy = RoundRobin
		g.streamManager.nextTunnelIndex = 0

		// Test multiple selections
		for i := 0; i < 4; i++ {
			index, err := g.selectRoundRobinStream()
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			expected := i % len(sm.tunnelStreams)
			if index != expected {
				t.Errorf("Expected index %d, got %d", expected, index)
			}
		}
	})

	t.Run("HashBased", func(t *testing.T) {
		data1 := []byte("test packet 1")
		data2 := []byte("test packet 2")

		index1, err := g.selectHashBasedStream(data1)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		index2, err := g.selectHashBasedStream(data2)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		// Same data should produce same index
		index1_repeat, err := g.selectHashBasedStream(data1)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		if index1 != index1_repeat {
			t.Errorf("Hash-based selection not consistent: %d != %d", index1, index1_repeat)
		}

		// Indices should be in valid range
		if index1 < 0 || index1 >= len(sm.tunnelStreams) {
			t.Errorf("Index out of range: %d", index1)
		}
		if index2 < 0 || index2 >= len(sm.tunnelStreams) {
			t.Errorf("Index out of range: %d", index2)
		}
	})

	t.Run("LeastConnections", func(t *testing.T) {
		index, err := g.selectLeastConnectionsStream()
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		// Should select stream with lowest load (stream_0_0 with load 0)
		if index != 0 {
			t.Errorf("Expected index 0 (lowest load), got %d", index)
		}
	})

	t.Run("HealthyStreamSelection", func(t *testing.T) {
		index, err := g.selectHealthyStream()
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		// Should find a healthy stream
		if !sm.tunnelStreams[index].isHealthy {
			t.Errorf("Selected unhealthy stream at index %d", index)
		}
	})
}

func TestSimpleHashBytes(t *testing.T) {
	data1 := []byte("test data 1")
	data2 := []byte("test data 2")
	data3 := []byte("test data 1") // Same as data1

	hash1 := simpleHashBytes(data1)
	hash2 := simpleHashBytes(data2)
	hash3 := simpleHashBytes(data3)

	// Same data should produce same hash
	if hash1 != hash3 {
		t.Errorf("Hash function not consistent: %d != %d", hash1, hash3)
	}

	// Different data should (likely) produce different hashes
	if hash1 == hash2 {
		t.Logf("Warning: Different data produced same hash (hash collision)")
	}

	// Hashes should be non-negative
	if hash1 < 0 || hash2 < 0 || hash3 < 0 {
		t.Errorf("Hash function produced negative value")
	}
}

func TestStreamMetrics(t *testing.T) {
	metrics := &StreamMetrics{
		PacketsSent:     100,
		PacketsReceived: 95,
		LastLatency:     10 * time.Millisecond,
		ErrorCount:      2,
		LastError:       time.Now(),
	}

	if metrics.PacketsSent != 100 {
		t.Errorf("Expected PacketsSent 100, got %d", metrics.PacketsSent)
	}

	if metrics.ErrorCount != 2 {
		t.Errorf("Expected ErrorCount 2, got %d", metrics.ErrorCount)
	}

	if metrics.LastLatency != 10*time.Millisecond {
		t.Errorf("Expected LastLatency 10ms, got %v", metrics.LastLatency)
	}
}

func TestStreamInfo(t *testing.T) {
	now := time.Now()
	streamInfo := &StreamInfo{
		connIndex: 1,
		streamID:  "test_stream_1_2",
		isHealthy: true,
		loadCount: 5,
		lastUsed:  now,
	}

	if streamInfo.connIndex != 1 {
		t.Errorf("Expected connIndex 1, got %d", streamInfo.connIndex)
	}

	if streamInfo.streamID != "test_stream_1_2" {
		t.Errorf("Expected streamID 'test_stream_1_2', got '%s'", streamInfo.streamID)
	}

	if !streamInfo.isHealthy {
		t.Error("Expected stream to be healthy")
	}

	if streamInfo.loadCount != 5 {
		t.Errorf("Expected loadCount 5, got %d", streamInfo.loadCount)
	}

	if !streamInfo.lastUsed.Equal(now) {
		t.Errorf("Expected lastUsed to match, got %v", streamInfo.lastUsed)
	}
}
