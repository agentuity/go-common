package gravity

import (
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/agentuity/go-common/gravity/proto"
	"google.golang.org/protobuf/proto"
)

// PerformanceMetrics is an alias for the protobuf type
type PerformanceMetrics = pb.PerformanceMetrics

// DockerContainerStats is an alias for the protobuf type
type DockerContainerStats = pb.DockerContainerStats

// MetricsCollector collects and aggregates performance metrics
type MetricsCollector struct {
	mu sync.RWMutex

	// Current metrics
	metrics *PerformanceMetrics

	// Latency tracking
	packetLatencies  *LatencyTracker
	controlLatencies *LatencyTracker

	// Counters for rate calculations
	lastPacketCount  int64
	lastByteCount    int64
	lastControlCount int64
	lastUpdateTime   time.Time

	// Circuit breaker tracking
	circuitBreakers   []*CircuitBreaker
	lastCircuitStates []CircuitBreakerState

	// Compression tracking
	totalUncompressed  int64
	totalCompressed    int64
	compressionSamples int64

	// Error tracking
	totalOperations int64
	totalErrors     int64

	// Stream reference for real-time data
	streamManager *StreamManager

	// Collection control
	stopChan         chan struct{}
	collectionTicker *time.Ticker
}

// LatencyTracker tracks latency statistics with percentile calculations
type LatencyTracker struct {
	mu         sync.RWMutex
	samples    []time.Duration
	maxSamples int
	total      time.Duration
	count      int64
	max        time.Duration
}

// NewLatencyTracker creates a new latency tracker
func NewLatencyTracker(maxSamples int) *LatencyTracker {
	return &LatencyTracker{
		samples:    make([]time.Duration, 0, maxSamples),
		maxSamples: maxSamples,
	}
}

// AddSample adds a latency sample
func (lt *LatencyTracker) AddSample(latency time.Duration) {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	lt.total += latency
	lt.count++

	if latency > lt.max {
		lt.max = latency
	}

	// Add to samples for percentile calculation
	lt.samples = append(lt.samples, latency)

	// Keep only recent samples
	if len(lt.samples) > lt.maxSamples {
		lt.samples = lt.samples[1:]
	}
}

// GetStats returns latency statistics
func (lt *LatencyTracker) GetStats() (avg, p95, p99, max time.Duration) {
	lt.mu.RLock()
	defer lt.mu.RUnlock()

	if lt.count == 0 {
		return 0, 0, 0, 0
	}

	avg = lt.total / time.Duration(lt.count)
	max = lt.max

	// Calculate percentiles from samples
	if len(lt.samples) > 0 {
		// Sort samples for percentile calculation
		sorted := make([]time.Duration, len(lt.samples))
		copy(sorted, lt.samples)

		// Simple insertion sort for small arrays
		for i := 1; i < len(sorted); i++ {
			for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
				sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
			}
		}

		p95Index := int(float64(len(sorted)) * 0.95)
		p99Index := int(float64(len(sorted)) * 0.99)

		if p95Index >= len(sorted) {
			p95Index = len(sorted) - 1
		}
		if p99Index >= len(sorted) {
			p99Index = len(sorted) - 1
		}

		p95 = sorted[p95Index]
		p99 = sorted[p99Index]
	}

	return avg, p95, p99, max
}

// Reset clears all latency data
func (lt *LatencyTracker) Reset() {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	lt.samples = lt.samples[:0]
	lt.total = 0
	lt.count = 0
	lt.max = 0
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(streamManager *StreamManager, circuitBreakers []*CircuitBreaker) *MetricsCollector {
	mc := &MetricsCollector{
		metrics: &pb.PerformanceMetrics{
			DockerStats: make(map[string]*pb.DockerContainerStats),
		},
		streamManager:     streamManager,
		circuitBreakers:   circuitBreakers,
		lastCircuitStates: make([]CircuitBreakerState, len(circuitBreakers)),
		packetLatencies:   NewLatencyTracker(1000), // Keep last 1000 samples
		controlLatencies:  NewLatencyTracker(1000),
		stopChan:          make(chan struct{}),
		lastUpdateTime:    time.Now(),
	}

	// Initialize circuit breaker states
	for i, cb := range circuitBreakers {
		mc.lastCircuitStates[i] = cb.State()
	}

	return mc
}

// Start begins metrics collection
func (mc *MetricsCollector) Start(interval time.Duration) {
	mc.mu.Lock()
	mc.collectionTicker = time.NewTicker(interval)
	mc.mu.Unlock()

	go mc.collectMetrics()
}

// Stop stops metrics collection
func (mc *MetricsCollector) Stop() {
	close(mc.stopChan)
	if mc.collectionTicker != nil {
		mc.collectionTicker.Stop()
	}
}

// AddPacketLatency records a packet latency measurement
func (mc *MetricsCollector) AddPacketLatency(latency time.Duration) {
	mc.packetLatencies.AddSample(latency)
}

// AddControlLatency records a control message latency measurement
func (mc *MetricsCollector) AddControlLatency(latency time.Duration) {
	mc.controlLatencies.AddSample(latency)
}

// AddCompressionStats records compression statistics
func (mc *MetricsCollector) AddCompressionStats(uncompressedSize, compressedSize int64, compressionTime time.Duration) {
	atomic.AddInt64(&mc.totalUncompressed, uncompressedSize)
	atomic.AddInt64(&mc.totalCompressed, compressedSize)
	atomic.AddInt64(&mc.compressionSamples, 1)

	// Update compression time (using simple moving average)
	mc.mu.Lock()
	mc.metrics.CompressionTimeNs = (mc.metrics.CompressionTimeNs + int64(compressionTime)) / 2
	mc.mu.Unlock()
}

// RecordPacket records packet transmission
func (mc *MetricsCollector) RecordPacket(bytes int64) {
	atomic.AddInt64(&mc.lastPacketCount, 1)
	atomic.AddInt64(&mc.lastByteCount, bytes)
	atomic.AddInt64(&mc.totalOperations, 1)
}

// RecordControlMessage records control message transmission
func (mc *MetricsCollector) RecordControlMessage() {
	atomic.AddInt64(&mc.lastControlCount, 1)
	atomic.AddInt64(&mc.totalOperations, 1)
}

// RecordError records an error occurrence
func (mc *MetricsCollector) RecordError() {
	atomic.AddInt64(&mc.totalErrors, 1)
}

// RecordRetry records retry attempt information
func (mc *MetricsCollector) RecordRetry(success bool) {
	atomic.AddInt64(&mc.metrics.TotalRetryAttempts, 1)
	if success {
		atomic.AddInt64(&mc.metrics.SuccessfulRetries, 1)
	} else {
		atomic.AddInt64(&mc.metrics.FailedRetries, 1)
	}
}

// GetMetrics returns current performance metrics
func (mc *MetricsCollector) GetMetrics() *PerformanceMetrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	// Return the protobuf metrics directly
	return mc.metrics
}

// collectMetrics runs the metrics collection loop
func (mc *MetricsCollector) collectMetrics() {
	for {
		select {
		case <-mc.stopChan:
			return
		case <-mc.collectionTicker.C:
			mc.updateMetrics()
		}
	}
}

// updateMetrics calculates and updates all metrics
func (mc *MetricsCollector) updateMetrics() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	now := time.Now()
	timeDelta := now.Sub(mc.lastUpdateTime).Seconds()

	// Update stream metrics
	mc.updateStreamMetrics()

	// Update throughput metrics
	mc.updateThroughputMetrics(timeDelta)

	// Update latency metrics
	mc.updateLatencyMetrics()

	// Update compression metrics
	mc.updateCompressionMetrics()

	// Update circuit breaker metrics
	mc.updateCircuitBreakerMetrics()

	// Update error metrics
	mc.updateErrorMetrics()

	// Update retry metrics
	mc.updateRetryMetrics()

	// Update connection pool metrics
	updateConnectionPoolMetrics()

	// Update timestamps
	mc.metrics.LastUpdated = now.UnixMilli()
	mc.lastUpdateTime = now
}

// updateStreamMetrics updates stream-related metrics
func (mc *MetricsCollector) updateStreamMetrics() {
	if mc.streamManager == nil {
		return
	}

	mc.streamManager.tunnelMu.RLock()
	totalStreams := int64(len(mc.streamManager.tunnelStreams))
	activeStreams := int64(0)
	healthyStreams := int64(0)

	for _, streamInfo := range mc.streamManager.tunnelStreams {
		if streamInfo.isHealthy {
			healthyStreams++
			if streamInfo.loadCount > 0 {
				activeStreams++
			}
		}
	}
	mc.streamManager.tunnelMu.RUnlock()

	mc.metrics.TotalStreams = totalStreams
	mc.metrics.ActiveConnections = activeStreams
	mc.metrics.HealthyStreams = healthyStreams
	mc.metrics.UnhealthyStreams = totalStreams - healthyStreams

	// Calculate stream utilization
	if totalStreams > 0 {
		mc.metrics.StreamUtilization = float64(activeStreams) / float64(totalStreams) * 100
	}
}

// updateThroughputMetrics updates throughput-related metrics
func (mc *MetricsCollector) updateThroughputMetrics(timeDelta float64) {
	if timeDelta <= 0 {
		return
	}

	currentPackets := atomic.LoadInt64(&mc.lastPacketCount)
	currentBytes := atomic.LoadInt64(&mc.lastByteCount)
	currentControl := atomic.LoadInt64(&mc.lastControlCount)

	mc.metrics.PacketsPerSecond = float64(currentPackets) / timeDelta
	mc.metrics.BytesPerSecond = float64(currentBytes) / timeDelta
	mc.metrics.ControlMessagesPerSec = float64(currentControl) / timeDelta

	// Reset counters
	atomic.StoreInt64(&mc.lastPacketCount, 0)
	atomic.StoreInt64(&mc.lastByteCount, 0)
	atomic.StoreInt64(&mc.lastControlCount, 0)
}

// updateLatencyMetrics updates latency-related metrics
func (mc *MetricsCollector) updateLatencyMetrics() {
	avg, p95, p99, max := mc.packetLatencies.GetStats()
	mc.metrics.AvgPacketLatencyNs = int64(avg)
	mc.metrics.P95PacketLatencyNs = int64(p95)
	mc.metrics.P99PacketLatencyNs = int64(p99)
	mc.metrics.MaxPacketLatencyNs = int64(max)

	// Control message latencies
	avgControl, p95Control, _, _ := mc.controlLatencies.GetStats()
	mc.metrics.AvgControlLatencyNs = int64(avgControl)
	mc.metrics.P95ControlLatencyNs = int64(p95Control)
}

// updateCompressionMetrics updates compression-related metrics
func (mc *MetricsCollector) updateCompressionMetrics() {
	totalUncompressed := atomic.LoadInt64(&mc.totalUncompressed)
	totalCompressed := atomic.LoadInt64(&mc.totalCompressed)

	if totalUncompressed > 0 {
		savedBytes := totalUncompressed - totalCompressed
		mc.metrics.BytesSaved = savedBytes
		mc.metrics.CompressionRatio = float64(savedBytes) / float64(totalUncompressed) * 100
	}
}

// updateCircuitBreakerMetrics updates circuit breaker metrics
func (mc *MetricsCollector) updateCircuitBreakerMetrics() {
	// Initialize slice if needed
	if len(mc.metrics.CircuitBreakerStates) != len(mc.circuitBreakers) {
		mc.metrics.CircuitBreakerStates = make([]string, len(mc.circuitBreakers))
	}

	for i, cb := range mc.circuitBreakers {
		currentState := cb.State()
		mc.metrics.CircuitBreakerStates[i] = currentState.String()

		// Track state transitions
		if i < len(mc.lastCircuitStates) && mc.lastCircuitStates[i] == StateClosed && currentState == StateOpen {
			// Note: TotalCircuitBreaks field doesn't exist in protobuf, removing this line
			// atomic.AddInt64(&mc.metrics.TotalCircuitBreaks, 1)
		}

		if i < len(mc.lastCircuitStates) {
			mc.lastCircuitStates[i] = currentState
		}
	}
}

// updateErrorMetrics updates error-related metrics
func (mc *MetricsCollector) updateErrorMetrics() {
	totalOps := atomic.LoadInt64(&mc.totalOperations)
	totalErrs := atomic.LoadInt64(&mc.totalErrors)

	if totalOps > 0 {
		mc.metrics.ErrorRate = float64(totalErrs) / float64(totalOps) * 100
	}
}

// updateRetryMetrics updates retry-related metrics
func (mc *MetricsCollector) updateRetryMetrics() {
	successful := atomic.LoadInt64(&mc.metrics.SuccessfulRetries)
	total := atomic.LoadInt64(&mc.metrics.TotalRetryAttempts)

	if total > 0 {
		mc.metrics.RetrySuccessRate = float64(successful) / float64(total) * 100
	}
}

// updateConnectionPoolMetrics updates connection pool metrics
func updateConnectionPoolMetrics() {
	// Connection pool metrics would be provided by the actual gRPC client
	// For now, this is a placeholder for future implementation
}

// Reset resets all metrics
func (mc *MetricsCollector) Reset() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.metrics = &pb.PerformanceMetrics{
		CircuitBreakerStates: make([]string, 0),
		DockerStats:          make(map[string]*pb.DockerContainerStats),
	}

	mc.packetLatencies.Reset()
	mc.controlLatencies.Reset()

	atomic.StoreInt64(&mc.totalUncompressed, 0)
	atomic.StoreInt64(&mc.totalCompressed, 0)
	atomic.StoreInt64(&mc.compressionSamples, 0)
	atomic.StoreInt64(&mc.totalOperations, 0)
	atomic.StoreInt64(&mc.totalErrors, 0)
	atomic.StoreInt64(&mc.lastPacketCount, 0)
	atomic.StoreInt64(&mc.lastByteCount, 0)
	atomic.StoreInt64(&mc.lastControlCount, 0)

	mc.lastUpdateTime = time.Now()
}

// UpdateDockerStats updates Docker container statistics for a specific deployment
func (mc *MetricsCollector) UpdateDockerStats(deploymentID string, stats interface{}) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	// Convert from various input formats to our DockerContainerStats
	var dockerStats *DockerContainerStats

	switch v := stats.(type) {
	case *DockerContainerStats:
		dockerStats = v
	case map[string]interface{}:
		// Convert from map format (used by DockerProvider)
		dockerStats = &DockerContainerStats{}

		// Container identity
		if val, ok := v["container_id"].(string); ok {
			dockerStats.ContainerId = val
		}
		if val, ok := v["container_name"].(string); ok {
			dockerStats.ContainerName = val
		}
		if val, ok := v["image"].(string); ok {
			dockerStats.Image = val
		}
		if val, ok := v["deployment_id"].(string); ok {
			dockerStats.DeploymentId = val
		}

		// Container state
		if val, ok := v["status"].(string); ok {
			dockerStats.Status = val
		}
		if val, ok := v["paused"].(bool); ok {
			dockerStats.Paused = val
		}
		if val, ok := v["paused_time"].(int64); ok {
			dockerStats.PausedTime = val
		}
		if val, ok := v["started_time"].(int64); ok {
			dockerStats.StartedTime = val
		}
		if val, ok := v["restart_count"].(int64); ok {
			dockerStats.RestartCount = val
		}

		// CPU metrics
		if val, ok := v["cpu_usage_percent"].(float64); ok {
			dockerStats.CpuUsagePercent = val
		}
		if val, ok := v["cpu_usage_total"].(uint64); ok {
			dockerStats.CpuUsageTotalNs = val
		}
		if val, ok := v["cpu_usage_kernel"].(uint64); ok {
			dockerStats.CpuUsageKernelNs = val
		}
		if val, ok := v["cpu_usage_user"].(uint64); ok {
			dockerStats.CpuUsageUserNs = val
		}
		if val, ok := v["cpu_throttled_periods"].(uint64); ok {
			dockerStats.CpuThrottledPeriods = val
		}
		if val, ok := v["cpu_throttled_time"].(uint64); ok {
			dockerStats.CpuThrottledTimeNs = val
		}
		if val, ok := v["cpu_limit"].(float64); ok {
			dockerStats.CpuLimit = val
		}

		// Memory metrics
		if val, ok := v["memory_usage"].(uint64); ok {
			dockerStats.MemoryUsageBytes = val
		}
		if val, ok := v["memory_limit"].(uint64); ok {
			dockerStats.MemoryLimitBytes = val
		}
		if val, ok := v["memory_percent"].(float64); ok {
			dockerStats.MemoryUsagePercent = val
		}
		if val, ok := v["memory_max_usage"].(uint64); ok {
			dockerStats.MemoryMaxUsageBytes = val
		}
		if val, ok := v["memory_cache"].(uint64); ok {
			dockerStats.MemoryCacheBytes = val
		}
		if val, ok := v["memory_rss"].(uint64); ok {
			dockerStats.MemoryRssBytes = val
		}
		if val, ok := v["memory_swap"].(uint64); ok {
			dockerStats.MemorySwapBytes = val
		}
		if val, ok := v["memory_swap_limit"].(uint64); ok {
			dockerStats.MemorySwapLimitBytes = val
		}
		if val, ok := v["oom_kills"].(uint64); ok {
			dockerStats.OomKills = val
		}

		// Network I/O metrics
		if val, ok := v["network_rx_bytes"].(uint64); ok {
			dockerStats.NetworkRxBytes = val
		}
		if val, ok := v["network_tx_bytes"].(uint64); ok {
			dockerStats.NetworkTxBytes = val
		}
		if val, ok := v["network_rx_packets"].(uint64); ok {
			dockerStats.NetworkRxPackets = val
		}
		if val, ok := v["network_tx_packets"].(uint64); ok {
			dockerStats.NetworkTxPackets = val
		}
		if val, ok := v["network_rx_errors"].(uint64); ok {
			dockerStats.NetworkRxErrors = val
		}
		if val, ok := v["network_tx_errors"].(uint64); ok {
			dockerStats.NetworkTxErrors = val
		}
		if val, ok := v["network_rx_dropped"].(uint64); ok {
			dockerStats.NetworkRxDropped = val
		}
		if val, ok := v["network_tx_dropped"].(uint64); ok {
			dockerStats.NetworkTxDropped = val
		}

		// Block I/O metrics
		if val, ok := v["block_io_read_bytes"].(uint64); ok {
			dockerStats.BlockIoReadBytes = val
		}
		if val, ok := v["block_io_write_bytes"].(uint64); ok {
			dockerStats.BlockIoWriteBytes = val
		}
		if val, ok := v["block_io_read_ops"].(uint64); ok {
			dockerStats.BlockIoReadOps = val
		}
		if val, ok := v["block_io_write_ops"].(uint64); ok {
			dockerStats.BlockIoWriteOps = val
		}

		// Process metrics
		if val, ok := v["pids"].(uint64); ok {
			dockerStats.PidsCurrent = val
		}
		if val, ok := v["pids_limit"].(uint64); ok {
			dockerStats.PidsLimit = val
		}

		// Network interface information
		if val, ok := v["ipv4"].(string); ok {
			dockerStats.Ipv4Address = val
		}
		if val, ok := v["ipv6"].(string); ok {
			dockerStats.Ipv6Address = val
		}
		if val, ok := v["hostname"].(string); ok {
			dockerStats.Hostname = val
		}

		// Runtime metrics
		if val, ok := v["inflight_requests"].(int64); ok {
			dockerStats.InflightRequests = val
		}

		// Health status
		if val, ok := v["healthy"].(bool); ok {
			dockerStats.Healthy = val
		}

		// Metadata
		if val, ok := v["last_updated"].(int64); ok {
			dockerStats.LastUpdated = val
		}
	default:
		// Unsupported type, skip
		return
	}

	if dockerStats != nil {
		dockerStats.LastUpdated = time.Now().UnixMilli()
		mc.metrics.DockerStats[deploymentID] = dockerStats
	}
}

// RemoveDockerStats removes Docker statistics for a specific deployment
func (mc *MetricsCollector) RemoveDockerStats(deploymentID string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	delete(mc.metrics.DockerStats, deploymentID)
}

// PauseDockerStats marks a container as paused and stops collecting stats
func (mc *MetricsCollector) PauseDockerStats(deploymentID string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if stats, exists := mc.metrics.DockerStats[deploymentID]; exists {
		stats.Paused = true
		stats.PausedTime = time.Now().UnixMilli()
	}
}

// UnpauseDockerStats marks a container as unpaused and resumes collecting stats
func (mc *MetricsCollector) UnpauseDockerStats(deploymentID string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if stats, exists := mc.metrics.DockerStats[deploymentID]; exists {
		stats.Paused = false
		stats.PausedTime = 0
	}
}

// GetDockerStats returns a copy of Docker stats for a specific deployment
func (mc *MetricsCollector) GetDockerStats(deploymentID string) (*DockerContainerStats, bool) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if stats, exists := mc.metrics.DockerStats[deploymentID]; exists {
		// Return a copy to avoid data races
		statsCopy := proto.Clone(stats).(*DockerContainerStats)
		return statsCopy, true
	}
	return nil, false
}
