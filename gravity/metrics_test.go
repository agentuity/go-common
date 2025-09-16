package gravity

import (
	"testing"
	"time"

	pb "github.com/agentuity/go-common/gravity/proto"
)

func TestMetricsCollector_Creation(t *testing.T) {
	// Create dummy stream manager and circuit breakers
	streamManager := &StreamManager{
		streamMetrics:      make(map[string]*StreamMetrics),
		allocationStrategy: RoundRobin,
	}

	circuitBreakers := []*CircuitBreaker{
		NewCircuitBreaker(DefaultCircuitBreakerConfig()),
		NewCircuitBreaker(DefaultCircuitBreakerConfig()),
	}

	mc := NewMetricsCollector(streamManager, circuitBreakers)

	if mc == nil {
		t.Error("Expected metrics collector to be created")
	}

	if mc.streamManager != streamManager {
		t.Error("Stream manager not set correctly")
	}

	if len(mc.circuitBreakers) != 2 {
		t.Errorf("Expected 2 circuit breakers, got %d", len(mc.circuitBreakers))
	}

	if mc.packetLatencies == nil {
		t.Error("Packet latency tracker not created")
	}

	if mc.controlLatencies == nil {
		t.Error("Control latency tracker not created")
	}
}

func TestMetricsCollector_LatencyTracking(t *testing.T) {
	mc := NewMetricsCollector(nil, nil)

	// Add some latency samples
	latencies := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		30 * time.Millisecond,
		40 * time.Millisecond,
		50 * time.Millisecond,
	}

	for _, latency := range latencies {
		mc.AddPacketLatency(latency)
	}

	// Update metrics to calculate latency stats
	mc.updateLatencyMetrics()

	// Get metrics
	metrics := mc.GetMetrics()

	if metrics.AvgPacketLatencyNs == 0 {
		t.Error("Expected non-zero average packet latency")
	}

	if metrics.MaxPacketLatencyNs == 0 {
		t.Error("Expected non-zero max packet latency")
	}

	// Max should be 50ms in nanoseconds
	expectedMaxNs := int64(50 * time.Millisecond)
	if metrics.MaxPacketLatencyNs != expectedMaxNs {
		t.Errorf("Expected max latency to be %d ns, got %d ns", expectedMaxNs, metrics.MaxPacketLatencyNs)
	}

	// Average should be 30ms in nanoseconds
	expectedAvgNs := int64(30 * time.Millisecond)
	if metrics.AvgPacketLatencyNs != expectedAvgNs {
		t.Errorf("Expected average latency to be %d ns, got %d ns", expectedAvgNs, metrics.AvgPacketLatencyNs)
	}
}

func TestMetricsCollector_CompressionStats(t *testing.T) {
	mc := NewMetricsCollector(nil, nil)

	// Add compression stats
	mc.AddCompressionStats(1000, 300, 5*time.Millisecond) // 70% compression
	mc.AddCompressionStats(2000, 800, 8*time.Millisecond) // 60% compression

	// Update metrics
	mc.updateMetrics()

	metrics := mc.GetMetrics()

	// Total uncompressed: 3000, total compressed: 1100, saved: 1900
	// Expected ratio ~63.33%
	if metrics.CompressionRatio < 60 || metrics.CompressionRatio > 70 {
		t.Errorf("Expected compression ratio around 63%%, got %.2f%%", metrics.CompressionRatio)
	}

	if metrics.BytesSaved != 1900 {
		t.Errorf("Expected 1900 bytes saved, got %d", metrics.BytesSaved)
	}

	if metrics.CompressionTimeNs == 0 {
		t.Error("Expected non-zero compression time")
	}
}

func TestMetricsCollector_PacketAndControlRecording(t *testing.T) {
	mc := NewMetricsCollector(nil, nil)

	// Record some packets and control messages
	mc.RecordPacket(1024)
	mc.RecordPacket(2048)
	mc.RecordControlMessage()
	mc.RecordControlMessage()

	// Simulate time passing and update metrics
	time.Sleep(10 * time.Millisecond)
	mc.updateThroughputMetrics(0.01) // 10ms = 0.01 seconds

	metrics := mc.GetMetrics()

	// Should have positive throughput values
	if metrics.PacketsPerSecond <= 0 {
		t.Error("Expected positive packets per second")
	}

	if metrics.BytesPerSecond <= 0 {
		t.Error("Expected positive bytes per second")
	}

	if metrics.ControlMessagesPerSec <= 0 {
		t.Error("Expected positive control messages per second")
	}
}

func TestMetricsCollector_ErrorTracking(t *testing.T) {
	mc := NewMetricsCollector(nil, nil)

	// Record operations and errors
	mc.RecordPacket(1024)
	mc.RecordPacket(1024)
	mc.RecordError() // 1 error out of 2 operations = 50% error rate

	mc.updateMetrics()

	metrics := mc.GetMetrics()

	expectedErrorRate := 50.0
	if metrics.ErrorRate != expectedErrorRate {
		t.Errorf("Expected error rate %.1f%%, got %.1f%%", expectedErrorRate, metrics.ErrorRate)
	}
}

func TestMetricsCollector_RetryTracking(t *testing.T) {
	mc := NewMetricsCollector(nil, nil)

	// Record retry attempts
	mc.RecordRetry(true)  // success
	mc.RecordRetry(false) // failure
	mc.RecordRetry(true)  // success

	mc.updateMetrics()

	metrics := mc.GetMetrics()

	if metrics.TotalRetryAttempts != 3 {
		t.Errorf("Expected 3 retry attempts, got %d", metrics.TotalRetryAttempts)
	}

	if metrics.SuccessfulRetries != 2 {
		t.Errorf("Expected 2 successful retries, got %d", metrics.SuccessfulRetries)
	}

	if metrics.FailedRetries != 1 {
		t.Errorf("Expected 1 failed retry, got %d", metrics.FailedRetries)
	}

	// Expected success rate ~66.67%
	if metrics.RetrySuccessRate < 66 || metrics.RetrySuccessRate > 67 {
		t.Errorf("Expected retry success rate around 66.67%%, got %.2f%%", metrics.RetrySuccessRate)
	}
}

func TestLatencyTracker_Percentiles(t *testing.T) {
	lt := NewLatencyTracker(100)

	// Add 100 samples with known distribution
	for i := 1; i <= 100; i++ {
		lt.AddSample(time.Duration(i) * time.Millisecond)
	}

	avg, p95, p99, max := lt.GetStats()

	// Average should be around 50.5ms
	expectedAvg := 50500 * time.Microsecond // 50.5ms
	if avg < expectedAvg-1*time.Millisecond || avg > expectedAvg+1*time.Millisecond {
		t.Errorf("Expected average around %v, got %v", expectedAvg, avg)
	}

	// P95 should be around 95ms
	expectedP95 := 95 * time.Millisecond
	if p95 < expectedP95-2*time.Millisecond || p95 > expectedP95+2*time.Millisecond {
		t.Errorf("Expected P95 around %v, got %v", expectedP95, p95)
	}

	// P99 should be around 99ms
	expectedP99 := 99 * time.Millisecond
	if p99 < expectedP99-2*time.Millisecond || p99 > expectedP99+2*time.Millisecond {
		t.Errorf("Expected P99 around %v, got %v", expectedP99, p99)
	}

	// Max should be 100ms
	expectedMax := 100 * time.Millisecond
	if max != expectedMax {
		t.Errorf("Expected max %v, got %v", expectedMax, max)
	}
}

func TestLatencyTracker_SampleLimit(t *testing.T) {
	maxSamples := 10
	lt := NewLatencyTracker(maxSamples)

	// Add more samples than the limit
	for i := 1; i <= 20; i++ {
		lt.AddSample(time.Duration(i) * time.Millisecond)
	}

	// Should only keep the most recent samples
	lt.mu.RLock()
	sampleCount := len(lt.samples)
	lt.mu.RUnlock()

	if sampleCount > maxSamples {
		t.Errorf("Expected at most %d samples, got %d", maxSamples, sampleCount)
	}

	// The samples should be the most recent ones (11-20)
	_, _, _, max := lt.GetStats()
	expectedMax := 20 * time.Millisecond
	if max != expectedMax {
		t.Errorf("Expected max to be %v (most recent), got %v", expectedMax, max)
	}
}

func TestLatencyTracker_Reset(t *testing.T) {
	lt := NewLatencyTracker(100)

	// Add some samples
	lt.AddSample(10 * time.Millisecond)
	lt.AddSample(20 * time.Millisecond)

	// Verify data exists
	avg, _, _, _ := lt.GetStats()
	if avg == 0 {
		t.Error("Expected non-zero average before reset")
	}

	// Reset
	lt.Reset()

	// Verify data is cleared
	avg, p95, p99, max := lt.GetStats()
	if avg != 0 || p95 != 0 || p99 != 0 || max != 0 {
		t.Error("Expected all stats to be zero after reset")
	}

	lt.mu.RLock()
	sampleCount := len(lt.samples)
	totalCount := lt.count
	lt.mu.RUnlock()

	if sampleCount != 0 {
		t.Errorf("Expected 0 samples after reset, got %d", sampleCount)
	}

	if totalCount != 0 {
		t.Errorf("Expected 0 total count after reset, got %d", totalCount)
	}
}

func TestServerMetrics_BasicFunctionality(t *testing.T) {
	sm := NewServerMetrics()

	if sm == nil {
		t.Error("Expected server metrics to be created")
	}

	// Test connection update
	sm.UpdateConnection(true)

	snapshot := sm.GetSnapshot()
	if !snapshot.IsConnected {
		t.Error("Expected connection to be marked as connected")
	}

	if snapshot.TotalConnections != 1 {
		t.Errorf("Expected 1 total connection, got %d", snapshot.TotalConnections)
	}
}

func TestServerMetrics_MessageRecording(t *testing.T) {
	sm := NewServerMetrics()

	// Record some messages
	sm.RecordMessage("packet", 1024, true)  // sent packet
	sm.RecordMessage("packet", 2048, false) // received packet
	sm.RecordMessage("control", 512, true)  // sent control

	snapshot := sm.GetSnapshot()

	if snapshot.MessageStats.PacketsSent != 1 {
		t.Errorf("Expected 1 packet sent, got %d", snapshot.MessageStats.PacketsSent)
	}

	if snapshot.MessageStats.PacketsReceived != 1 {
		t.Errorf("Expected 1 packet received, got %d", snapshot.MessageStats.PacketsReceived)
	}

	if snapshot.MessageStats.ControlMessagesSent != 1 {
		t.Errorf("Expected 1 control message sent, got %d", snapshot.MessageStats.ControlMessagesSent)
	}

	expectedPacketBytes := int64(1024 + 2048)
	if snapshot.MessageStats.PacketBytes != expectedPacketBytes {
		t.Errorf("Expected %d packet bytes, got %d", expectedPacketBytes, snapshot.MessageStats.PacketBytes)
	}

	expectedAvgSize := float64(expectedPacketBytes) / 2.0 // 2 packets
	if snapshot.MessageStats.AvgPacketSize != expectedAvgSize {
		t.Errorf("Expected average packet size %.1f, got %.1f", expectedAvgSize, snapshot.MessageStats.AvgPacketSize)
	}
}

func TestServerMetrics_HealthScore(t *testing.T) {
	sm := NewServerMetrics()

	// Set up performance metrics for health calculation
	if sm.pb.Performance == nil {
		sm.pb.Performance = &pb.PerformanceMetrics{}
	}
	sm.pb.Performance.TotalStreams = 10
	sm.pb.Performance.HealthyStreams = 8
	sm.pb.Performance.ErrorRate = 5.0 // 5% error rate
	sm.pb.Performance.CircuitBreakerStates = []string{"CLOSED", "CLOSED", "OPEN"}

	sm.UpdateConnection(true)

	healthScore := sm.GetHealthScore()

	// Expected calculation:
	// - Connection: 25 (connected)
	// - Stream health: 20 (8/10 * 25)
	// - Error health: 22.5 (25 - 5*0.5)
	// - Circuit breaker: 16.67 (2/3 * 25)
	// Total: ~84.17

	if healthScore < 80 || healthScore > 90 {
		t.Errorf("Expected health score around 84, got %.2f", healthScore)
	}
}
