package gravity

import (
	"crypto/rand"
	"fmt"
	"sync"
	"testing"
	"time"

	pb "github.com/agentuity/go-common/gravity/proto"
	"google.golang.org/protobuf/proto"
)

// BenchmarkStreamAllocationStrategies tests different allocation strategies
func BenchmarkStreamAllocationStrategies(b *testing.B) {
	strategies := []StreamAllocationStrategy{
		RoundRobin,
		HashBased,
		LeastConnections,
		WeightedRoundRobin,
	}

	for _, strategy := range strategies {
		b.Run(strategy.String(), func(b *testing.B) {
			benchmarkSimpleStreamAllocation(b, strategy)
		})
	}
}

func benchmarkSimpleStreamAllocation(b *testing.B, strategy StreamAllocationStrategy) {
	b.Helper()

	// Create test data
	data := make([]byte, 1024)
	rand.Read(data)

	streamCount := 8

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		var selectedIndex int

		switch strategy {
		case RoundRobin:
			selectedIndex = i % streamCount
		case HashBased:
			hash := simpleHashBytes(data)
			selectedIndex = hash % streamCount
		case LeastConnections:
			// Simulate selecting stream with lowest load
			selectedIndex = 0 // Simplified
		case WeightedRoundRobin:
			// Simulate weighted selection
			selectedIndex = (i * 2) % streamCount
		}

		_ = selectedIndex // Use the result
	}
}

// BenchmarkProtobufSerialization tests protobuf serialization performance
func BenchmarkProtobufSerialization(b *testing.B) {
	sessionMessage := &pb.SessionMessage{
		Id:       "test_message",
		StreamId: "test_stream",
		MessageType: &pb.SessionMessage_Response{
			Response: &pb.ProtocolResponse{
				Id:      "test_response",
				Event:   "test_event",
				Success: true,
				Payload: make([]byte, 1024),
			},
		},
	}

	tunnelPacket := &pb.TunnelPacket{
		Data:     make([]byte, 1500), // Typical packet size
		StreamId: "tunnel_stream",
	}

	b.Run("SessionMessage_Protobuf", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			data, _ := proto.Marshal(sessionMessage)
			var unmarshaled pb.SessionMessage
			_ = proto.Unmarshal(data, &unmarshaled)
		}
	})

	b.Run("TunnelPacket_Protobuf", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			data, _ := proto.Marshal(tunnelPacket)
			var unmarshaled pb.TunnelPacket
			_ = proto.Unmarshal(data, &unmarshaled)
		}
	})
}

// BenchmarkConnectionPoolEfficiency tests pool management overhead
func BenchmarkConnectionPoolEfficiency(b *testing.B) {
	poolSizes := []int{2, 4, 8}

	for _, poolSize := range poolSizes {
		b.Run(fmt.Sprintf("Pool_%dConns", poolSize), func(b *testing.B) {
			benchmarkPoolManagement(b, poolSize)
		})
	}
}

func benchmarkPoolManagement(b *testing.B, poolSize int) {
	b.Helper()

	// Simulate connection pool with health tracking
	connectionHealth := make([]bool, poolSize)
	streamMetrics := make(map[string]*StreamMetrics)

	// Initialize all connections as healthy
	for i := 0; i < poolSize; i++ {
		connectionHealth[i] = true
		streamMetrics[fmt.Sprintf("stream_%d", i)] = &StreamMetrics{
			PacketsSent:     int64(i * 100),
			PacketsReceived: int64(i * 95),
			ErrorCount:      int64(i % 3),
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Simulate health check operations
		healthyCount := 0
		for _, healthy := range connectionHealth {
			if healthy {
				healthyCount++
			}
		}

		// Simulate metric updates
		streamID := fmt.Sprintf("stream_%d", i%poolSize)
		if metrics := streamMetrics[streamID]; metrics != nil {
			metrics.PacketsSent++
			metrics.LastLatency = time.Microsecond * time.Duration(i%1000)
		}

		_ = healthyCount // Use the result
	}
}

// BenchmarkConcurrentStreamUsage tests concurrent access patterns
func BenchmarkConcurrentStreamUsage(b *testing.B) {
	streamCount := 8
	streamsPerGoroutine := streamCount / 4 // 4 goroutines

	streamMetrics := make([]*StreamMetrics, streamCount)
	for i := 0; i < streamCount; i++ {
		streamMetrics[i] = &StreamMetrics{}
	}

	b.ResetTimer()
	b.ReportAllocs()

	var wg sync.WaitGroup
	operationsPerGoroutine := b.N / 4

	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			startStream := goroutineID * streamsPerGoroutine
			endStream := startStream + streamsPerGoroutine

			for i := 0; i < operationsPerGoroutine; i++ {
				streamIdx := startStream + (i % streamsPerGoroutine)
				if streamIdx < endStream && streamIdx < len(streamMetrics) {
					// Simulate stream usage
					streamMetrics[streamIdx].PacketsSent++
					streamMetrics[streamIdx].LastLatency = time.Microsecond * time.Duration(i%100)
				}
			}
		}(g)
	}

	wg.Wait()
}

// BenchmarkHashingPerformance tests packet hashing for stream selection
func BenchmarkHashingPerformance(b *testing.B) {
	packetSizes := []int{64, 512, 1024, 1500}

	for _, size := range packetSizes {
		b.Run(fmt.Sprintf("Hash_%dB", size), func(b *testing.B) {
			packet := make([]byte, size)
			rand.Read(packet)

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				hash := simpleHashBytes(packet)
				streamIndex := hash % 8 // 8 streams
				_ = streamIndex
			}
		})
	}
}
