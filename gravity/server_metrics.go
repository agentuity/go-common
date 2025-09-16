package gravity

import (
	"math"
	"runtime"
	"sync"
	"time"

	pb "github.com/agentuity/go-common/gravity/proto"
)

// ServerMetrics represents enhanced server performance metrics with gRPC-specific data
// This wraps the protobuf ServerMetrics with a mutex for thread safety
type ServerMetrics struct {
	mu sync.RWMutex
	pb *pb.ServerMetrics
}

// Type aliases for protobuf types to maintain API compatibility
type (
	ServerMetricsSnapshot = pb.ServerMetrics
	GRPCConnectionMetrics = pb.GRPCConnectionMetrics
	MessageStatistics     = pb.MessageStatistics
	SystemResourceMetrics = pb.SystemResourceMetrics
	HistoricalMetrics     = pb.HistoricalMetrics
	ThroughputSample      = pb.ThroughputSample
	LatencySample         = pb.LatencySample
	ErrorRateSample       = pb.ErrorRateSample
	HealthSample          = pb.HealthSample
)

// NewServerMetrics creates a new ServerMetrics instance
func NewServerMetrics() *ServerMetrics {
	return &ServerMetrics{
		pb: &pb.ServerMetrics{
			StartTime: time.Now().UnixMilli(),
			MessageStats: &pb.MessageStatistics{
				MessagesByType: make(map[string]int64),
			},
			HistoricalData: &pb.HistoricalMetrics{
				ThroughputHistory: make([]*pb.ThroughputSample, 0, 60),
				LatencyHistory:    make([]*pb.LatencySample, 0, 60),
				ErrorRateHistory:  make([]*pb.ErrorRateSample, 0, 60),
				HealthHistory:     make([]*pb.HealthSample, 0, 60),
				MaxHistoryLength:  60,
			},
			Performance:   &pb.PerformanceMetrics{},
			SystemMetrics: &pb.SystemResourceMetrics{},
		},
	}
}

// UpdateConnection updates connection-related metrics
func (sm *ServerMetrics) UpdateConnection(connected bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	wasConnected := sm.pb.IsConnected
	sm.pb.IsConnected = connected

	if connected && !wasConnected {
		sm.pb.ConnectedTime = time.Now().UnixMilli()
		sm.pb.TotalConnections++
	} else if !connected && wasConnected {
		sm.pb.ReconnectCount++
	}
}

// UpdateGRPCMetrics updates gRPC-specific metrics
func (sm *ServerMetrics) UpdateGRPCMetrics(grpcMetrics *GRPCConnectionMetrics) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.pb.GrpcMetrics = grpcMetrics
}

// UpdatePerformanceMetrics updates performance metrics from the collector
func (sm *ServerMetrics) UpdatePerformanceMetrics(perfMetrics *PerformanceMetrics) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.pb.Performance = perfMetrics

	// Update message statistics from performance metrics
	if sm.pb.MessageStats == nil {
		sm.pb.MessageStats = &pb.MessageStatistics{}
	}
	sm.pb.MessageStats.PacketsPerSecond = perfMetrics.PacketsPerSecond
	sm.pb.MessageStats.BytesPerSecond = perfMetrics.BytesPerSecond
	sm.pb.MessageStats.PacketLossRate = perfMetrics.ErrorRate
}

// RecordMessage records a message transmission
func (sm *ServerMetrics) RecordMessage(messageType string, bytes int64, sent bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.pb.LastMessageTime = time.Now().UnixMilli()

	// Initialize message stats if needed
	if sm.pb.MessageStats == nil {
		sm.pb.MessageStats = &pb.MessageStatistics{
			MessagesByType: make(map[string]int64),
		}
	}

	// Update message type counters
	if sm.pb.MessageStats.MessagesByType == nil {
		sm.pb.MessageStats.MessagesByType = make(map[string]int64)
	}
	sm.pb.MessageStats.MessagesByType[messageType]++

	// Update packet vs control message stats
	if messageType == "packet" {
		if sent {
			sm.pb.MessageStats.PacketsSent++
		} else {
			sm.pb.MessageStats.PacketsReceived++
		}
		sm.pb.MessageStats.PacketBytes += bytes

		// Update average packet size
		totalPackets := sm.pb.MessageStats.PacketsSent + sm.pb.MessageStats.PacketsReceived
		if totalPackets > 0 {
			sm.pb.MessageStats.AvgPacketSize = float64(sm.pb.MessageStats.PacketBytes) / float64(totalPackets)
		}
	} else {
		// Control message
		if sent {
			sm.pb.MessageStats.ControlMessagesSent++
		} else {
			sm.pb.MessageStats.ControlMessagesReceived++
		}
		sm.pb.MessageStats.ControlMessageBytes += bytes
	}
}

// UpdateSystemMetrics updates system resource metrics
func (sm *ServerMetrics) UpdateSystemMetrics() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Initialize system metrics if needed
	if sm.pb.SystemMetrics == nil {
		sm.pb.SystemMetrics = &pb.SystemResourceMetrics{}
	}

	sm.pb.SystemMetrics.MemoryUsageBytes = int64(m.Alloc)
	sm.pb.SystemMetrics.MemoryUsageMb = float64(m.Alloc) / 1024 / 1024
	sm.pb.SystemMetrics.AllocatedMemory = int64(m.TotalAlloc)
	sm.pb.SystemMetrics.NumGoroutines = int32(runtime.NumGoroutine())

	// Calculate buffer pool hit rate
	totalBufferRequests := sm.pb.SystemMetrics.BufferPoolHits + sm.pb.SystemMetrics.BufferPoolMisses
	if totalBufferRequests > 0 {
		sm.pb.SystemMetrics.BufferPoolHitRate = float64(sm.pb.SystemMetrics.BufferPoolHits) / float64(totalBufferRequests) * 100
	}
}

// AddHistoricalSample adds a sample to historical data
func (sm *ServerMetrics) AddHistoricalSample() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now().UnixMilli()

	// Initialize historical data if needed
	if sm.pb.HistoricalData == nil {
		sm.pb.HistoricalData = &pb.HistoricalMetrics{
			ThroughputHistory: make([]*pb.ThroughputSample, 0, 60),
			LatencyHistory:    make([]*pb.LatencySample, 0, 60),
			ErrorRateHistory:  make([]*pb.ErrorRateSample, 0, 60),
			HealthHistory:     make([]*pb.HealthSample, 0, 60),
			MaxHistoryLength:  60,
		}
	}

	// Add throughput sample
	throughputSample := &pb.ThroughputSample{
		Timestamp:      now,
		PacketsPerSec:  sm.pb.Performance.PacketsPerSecond,
		BytesPerSec:    sm.pb.Performance.BytesPerSecond,
		MessagesPerSec: sm.pb.Performance.ControlMessagesPerSec,
	}
	sm.addToHistory(&sm.pb.HistoricalData.ThroughputHistory, throughputSample)

	// Add latency sample
	latencySample := &pb.LatencySample{
		Timestamp:    now,
		AvgLatencyNs: sm.pb.Performance.AvgPacketLatencyNs,
		P95LatencyNs: sm.pb.Performance.P95PacketLatencyNs,
		P99LatencyNs: sm.pb.Performance.P99PacketLatencyNs,
		MaxLatencyNs: sm.pb.Performance.MaxPacketLatencyNs,
	}
	sm.addToHistory(&sm.pb.HistoricalData.LatencyHistory, latencySample)

	// Add error rate sample
	errorSample := &pb.ErrorRateSample{
		Timestamp:   now,
		ErrorRate:   sm.pb.Performance.ErrorRate,
		TotalErrors: sm.pb.Performance.PacketDrops + sm.pb.Performance.StreamErrors + sm.pb.Performance.ConnectionErrors,
	}
	sm.addToHistory(&sm.pb.HistoricalData.ErrorRateHistory, errorSample)

	// Add health sample
	healthScore := float64(100)
	if sm.pb.Performance.TotalStreams > 0 {
		healthScore = float64(sm.pb.Performance.HealthyStreams) / float64(sm.pb.Performance.TotalStreams) * 100
	}

	healthSample := &pb.HealthSample{
		Timestamp:         now,
		HealthyStreams:    int32(sm.pb.Performance.HealthyStreams),
		TotalStreams:      int32(sm.pb.Performance.TotalStreams),
		ActiveConnections: int32(sm.pb.Performance.ActiveConnections),
		HealthScore:       healthScore,
	}
	sm.addToHistory(&sm.pb.HistoricalData.HealthHistory, healthSample)
}

// addToHistory adds a sample to a history slice, maintaining max length
func (sm *ServerMetrics) addToHistory(history interface{}, sample interface{}) {
	maxLen := int(sm.pb.HistoricalData.MaxHistoryLength)

	switch h := history.(type) {
	case *[]*pb.ThroughputSample:
		*h = append(*h, sample.(*pb.ThroughputSample))
		if len(*h) > maxLen {
			*h = (*h)[1:]
		}
	case *[]*pb.LatencySample:
		*h = append(*h, sample.(*pb.LatencySample))
		if len(*h) > maxLen {
			*h = (*h)[1:]
		}
	case *[]*pb.ErrorRateSample:
		*h = append(*h, sample.(*pb.ErrorRateSample))
		if len(*h) > maxLen {
			*h = (*h)[1:]
		}
	case *[]*pb.HealthSample:
		*h = append(*h, sample.(*pb.HealthSample))
		if len(*h) > maxLen {
			*h = (*h)[1:]
		}
	}
}

// GetSnapshot returns a read-only snapshot of current metrics
func (sm *ServerMetrics) GetSnapshot() *pb.ServerMetrics {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Update uptime and return the protobuf metrics directly
	now := time.Now().UnixMilli()
	sm.pb.Uptime = uint64(now - sm.pb.StartTime)

	return sm.pb
}

// Reset resets all metrics to initial state
func (sm *ServerMetrics) Reset() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Reset protobuf fields, keeping original start time
	startTime := sm.pb.StartTime

	sm.pb = &pb.ServerMetrics{
		StartTime: startTime,
		MessageStats: &pb.MessageStatistics{
			MessagesByType: make(map[string]int64),
		},
		HistoricalData: &pb.HistoricalMetrics{
			ThroughputHistory: make([]*pb.ThroughputSample, 0, 60),
			LatencyHistory:    make([]*pb.LatencySample, 0, 60),
			ErrorRateHistory:  make([]*pb.ErrorRateSample, 0, 60),
			HealthHistory:     make([]*pb.HealthSample, 0, 60),
			MaxHistoryLength:  60,
		},
		Performance:   &pb.PerformanceMetrics{},
		SystemMetrics: &pb.SystemResourceMetrics{},
	}
}

// GetHealthScore calculates an overall health score (0-100)
func (sm *ServerMetrics) GetHealthScore() float64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	score := float64(0)
	factors := 0

	// Connection health (25% weight)
	if sm.pb.IsConnected {
		score += 25
	}
	factors++

	// Stream health (25% weight)
	if sm.pb.Performance != nil && sm.pb.Performance.TotalStreams > 0 {
		streamHealth := float64(sm.pb.Performance.HealthyStreams) / float64(sm.pb.Performance.TotalStreams) * 25
		score += streamHealth
	} else {
		score += 25 // No streams is okay
	}
	factors++

	// Error rate health (25% weight)
	errorHealth := float64(25)
	if sm.pb.Performance != nil {
		errorHealth = math.Max(0, 25-sm.pb.Performance.ErrorRate*0.5) // 50% error rate = 0 score
	}
	score += errorHealth
	factors++

	// Circuit breaker health (25% weight)
	circuitHealth := float64(25)
	if sm.pb.Performance != nil && len(sm.pb.Performance.CircuitBreakerStates) > 0 {
		openCircuits := 0
		for _, state := range sm.pb.Performance.CircuitBreakerStates {
			if state == "OPEN" {
				openCircuits++
			}
		}
		circuitHealth = float64(len(sm.pb.Performance.CircuitBreakerStates)-openCircuits) / float64(len(sm.pb.Performance.CircuitBreakerStates)) * 25
	}
	score += circuitHealth
	factors++

	return score
}
