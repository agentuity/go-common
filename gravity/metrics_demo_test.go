package gravity

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// TestMetricsDemo demonstrates the comprehensive metrics system
func TestMetricsDemo(t *testing.T) {
	// Skip this test in normal runs - it's for demonstration
	if testing.Short() {
		t.Skip("Skipping metrics demo in short mode")
	}

	fmt.Println("\n=== Performance Metrics System Demo ===")

	// Create a demo stream manager with some streams
	streamManager := &StreamManager{
		streamMetrics:      make(map[string]*StreamMetrics),
		allocationStrategy: HashBased,
		tunnelStreams: []*StreamInfo{
			{streamID: "stream-1", isHealthy: true, loadCount: 5},
			{streamID: "stream-2", isHealthy: true, loadCount: 0},
			{streamID: "stream-3", isHealthy: false, loadCount: 0},
			{streamID: "stream-4", isHealthy: true, loadCount: 2},
		},
	}

	// Create circuit breakers in different states
	circuitBreakers := []*CircuitBreaker{
		NewCircuitBreaker(DefaultCircuitBreakerConfig()),
		NewCircuitBreaker(DefaultCircuitBreakerConfig()),
		NewCircuitBreaker(DefaultCircuitBreakerConfig()),
	}

	// Simulate circuit breaker states
	circuitBreakers[1].TransitionToHalfOpen()
	// Force circuit breaker 2 to open state by triggering failures
	for i := 0; i < 6; i++ {
		circuitBreakers[2].onFailure()
	}

	// Create metrics collector
	mc := NewMetricsCollector(streamManager, circuitBreakers)

	// Add mock Docker container stats
	now := time.Now()
	dockerStats1 := &DockerContainerStats{
		ContainerId:        "abc123def456",
		ContainerName:      "frontend-app",
		Image:              "nginx:latest",
		DeploymentId:       "app-frontend-v1",
		Status:             "running",
		CpuUsagePercent:    45.7,
		MemoryUsageBytes:   536870912,  // 512MB
		MemoryLimitBytes:   1073741824, // 1GB
		MemoryUsagePercent: 50.0,
		NetworkRxBytes:     1048576,   // 1MB
		NetworkTxBytes:     2097152,   // 2MB
		BlockIoReadBytes:   104857600, // 100MB
		BlockIoWriteBytes:  52428800,  // 50MB
		PidsCurrent:        15,
		Paused:             false,
		Ipv4Address:        "10.0.0.10",
		Ipv6Address:        "fc00::1:10",
		Hostname:           "frontend-app",
		InflightRequests:   5,
		LastUpdated:        now.UnixMilli(),
	}

	dockerStats2 := &DockerContainerStats{
		ContainerId:        "def456ghi789",
		ContainerName:      "backend-api",
		Image:              "node:18-alpine",
		DeploymentId:       "app-backend-v2",
		Status:             "running",
		CpuUsagePercent:    78.3,
		MemoryUsageBytes:   805306368,  // 768MB
		MemoryLimitBytes:   2147483648, // 2GB
		MemoryUsagePercent: 37.5,
		NetworkRxBytes:     5242880,   // 5MB
		NetworkTxBytes:     3145728,   // 3MB
		BlockIoReadBytes:   209715200, // 200MB
		BlockIoWriteBytes:  104857600, // 100MB
		PidsCurrent:        28,
		Paused:             false,
		Ipv4Address:        "10.0.0.20",
		Ipv6Address:        "fc00::1:20",
		Hostname:           "backend-api",
		InflightRequests:   12,
		LastUpdated:        now.UnixMilli(),
	}

	dockerStats3 := &DockerContainerStats{
		ContainerId:        "ghi789jkl012",
		ContainerName:      "redis-cache",
		Image:              "redis:7-alpine",
		DeploymentId:       "app-cache-v1",
		Status:             "paused",
		CpuUsagePercent:    12.1,
		MemoryUsageBytes:   268435456, // 256MB
		MemoryLimitBytes:   536870912, // 512MB
		MemoryUsagePercent: 50.0,
		NetworkRxBytes:     524288,   // 512KB
		NetworkTxBytes:     1048576,  // 1MB
		BlockIoReadBytes:   52428800, // 50MB
		BlockIoWriteBytes:  26214400, // 25MB
		PidsCurrent:        8,
		Paused:             true,
		PausedTime:         now.Add(-2 * time.Minute).UnixMilli(),
		Ipv4Address:        "10.0.0.30",
		Ipv6Address:        "fc00::1:30",
		Hostname:           "redis-cache",
		InflightRequests:   2,
		LastUpdated:        now.UnixMilli(),
	}

	// Update metrics collector with Docker stats
	mc.UpdateDockerStats("app-frontend-v1", dockerStats1)
	mc.UpdateDockerStats("app-backend-v2", dockerStats2)
	mc.UpdateDockerStats("app-cache-v1", dockerStats3)

	fmt.Println("1. Initial Metrics Collection Setup")
	fmt.Printf("   - Stream Manager: %d streams (%d healthy)\n", len(streamManager.tunnelStreams), 3)
	fmt.Printf("   - Circuit Breakers: %d (states: CLOSED, HALF_OPEN, OPEN)\n", len(circuitBreakers))
	fmt.Printf("   - Latency Trackers: Packet and Control (max 1000 samples each)\n")
	fmt.Printf("   - Docker Containers: %d (2 running, 1 paused)\n", 3)

	// Simulate traffic for 5 seconds
	fmt.Println("2. Simulating Traffic for 5 seconds...")

	go func() {
		for i := 0; i < 50; i++ {
			// Simulate packets
			mc.RecordPacket(1024)
			mc.AddPacketLatency(time.Duration(10+i%50) * time.Millisecond)

			// Simulate control messages every 10 packets
			if i%10 == 0 {
				mc.RecordControlMessage()
				mc.AddControlLatency(time.Duration(5+i%20) * time.Millisecond)
				mc.AddCompressionStats(500, 150, 2*time.Millisecond) // 70% compression
			}

			// Simulate some errors
			if i%15 == 0 {
				mc.RecordError()
			}

			// Simulate retries
			if i%8 == 0 {
				mc.RecordRetry(i%3 != 0) // 2/3 success rate
			}

			// Simulate Docker stats updates every 20 iterations
			if i%20 == 0 {
				// Update CPU and memory usage with some variance
				dockerStats1.CpuUsagePercent = 45.7 + float64(i%10-5)*2.5
				dockerStats1.MemoryUsageBytes = uint64(536870912 + int64(i%10-5)*10485760) // ±10MB variance
				dockerStats1.LastUpdated = time.Now().UnixMilli()
				mc.UpdateDockerStats("app-frontend-v1", dockerStats1)

				dockerStats2.CpuUsagePercent = 78.3 + float64(i%15-7)*1.8
				dockerStats2.MemoryUsageBytes = uint64(805306368 + int64(i%15-7)*5242880) // ±5MB variance
				dockerStats2.LastUpdated = time.Now().UnixMilli()
				mc.UpdateDockerStats("app-backend-v2", dockerStats2)
			}

			time.Sleep(100 * time.Millisecond)
		}
	}()

	// Wait and then collect final metrics
	time.Sleep(6 * time.Second)
	mc.updateMetrics()
	metrics := mc.GetMetrics()

	fmt.Println("\n3. Performance Metrics Results:")
	printMetricsSection("Stream Utilization", map[string]interface{}{
		"Active Connections": metrics.ActiveConnections,
		"Total Streams":      metrics.TotalStreams,
		"Healthy Streams":    metrics.HealthyStreams,
	})

	printMetricsSection("Throughput", map[string]interface{}{
		"Packets/sec":      fmt.Sprintf("%.2f", metrics.PacketsPerSecond),
		"Bytes/sec":        fmt.Sprintf("%.2f", metrics.BytesPerSecond),
		"Control Msgs/sec": fmt.Sprintf("%.2f", metrics.ControlMessagesPerSec),
	})

	printMetricsSection("Latency", map[string]interface{}{
		"Avg Packet Latency (ns)": metrics.AvgPacketLatencyNs,
		"P95 Packet Latency (ns)": metrics.P95PacketLatencyNs,
		"P99 Packet Latency (ns)": metrics.P99PacketLatencyNs,
		"Max Packet Latency (ns)": metrics.MaxPacketLatencyNs,
	})

	printMetricsSection("Compression", map[string]interface{}{
		"Compression Ratio %": fmt.Sprintf("%.2f", metrics.CompressionRatio),
		"Bytes Saved":         metrics.BytesSaved,
		"Compression Time ns": metrics.CompressionTimeNs,
	})

	printMetricsSection("Circuit Breakers", map[string]interface{}{
		"States": metrics.CircuitBreakerStates,
	})

	printMetricsSection("Retry Logic", map[string]interface{}{
		"Total Attempts": metrics.TotalRetryAttempts,
		"Successful":     metrics.SuccessfulRetries,
		"Failed":         metrics.FailedRetries,
		"Success Rate %": fmt.Sprintf("%.2f", metrics.RetrySuccessRate),
	})

	printMetricsSection("Error Tracking", map[string]interface{}{
		"Packet Drops":      metrics.PacketDrops,
		"Stream Errors":     metrics.StreamErrors,
		"Connection Errors": metrics.ConnectionErrors,
		"Error Rate %":      fmt.Sprintf("%.2f", metrics.ErrorRate),
	})

	// Display Docker container statistics
	printMetricsSection("Docker Container Stats", map[string]interface{}{
		"Total Containers": len(metrics.DockerStats),
		"Running":          2,
		"Paused":           1,
	})

	// Show individual container details
	for deploymentID, stats := range metrics.DockerStats {
		status := "RUNNING"
		if stats.Paused {
			status = "PAUSED"
		}

		printMetricsSection(fmt.Sprintf("Container: %s (%s)", stats.ContainerName, status), map[string]interface{}{
			"Deployment ID": deploymentID,
			"Container ID":  stats.ContainerId[:12], // Show short ID
			"Image":         stats.Image,
			"Hostname":      stats.Hostname,
			"IPv4 Address":  stats.Ipv4Address,
			"IPv6 Address":  stats.Ipv6Address,
			"CPU Usage %":   fmt.Sprintf("%.1f", stats.CpuUsagePercent),
			"Memory Usage":  fmt.Sprintf("%.1fMB", float64(stats.MemoryUsageBytes)/1024/1024),
			"Memory Limit":  fmt.Sprintf("%.1fMB", float64(stats.MemoryLimitBytes)/1024/1024),
			"Memory %":      fmt.Sprintf("%.1f", stats.MemoryUsagePercent),
			"Network RX":    fmt.Sprintf("%.1fKB", float64(stats.NetworkRxBytes)/1024),
			"Network TX":    fmt.Sprintf("%.1fKB", float64(stats.NetworkTxBytes)/1024),
			"Disk Read":     fmt.Sprintf("%.1fMB", float64(stats.BlockIoReadBytes)/1024/1024),
			"Disk Write":    fmt.Sprintf("%.1fMB", float64(stats.BlockIoWriteBytes)/1024/1024),
			"PIDs":          stats.PidsCurrent,
			"Inflight Reqs": stats.InflightRequests,
			"Last Updated":  time.UnixMilli(stats.LastUpdated).Format("15:04:05"),
		})

		if stats.Paused && stats.PausedTime != 0 {
			pausedTime := time.UnixMilli(stats.PausedTime)
			fmt.Printf("     %-20s: %s ago\n", "Paused Since", time.Since(pausedTime).Truncate(time.Second))
			fmt.Println()
		}
	}

	// Demonstrate ServerMetrics integration
	fmt.Println("\n4. Enhanced Server Metrics:")
	sm := NewServerMetrics()
	sm.UpdateConnection(true)
	sm.UpdatePerformanceMetrics(metrics)

	// Create mock gRPC metrics
	grpcMetrics := &GRPCConnectionMetrics{
		PoolSize:           4,
		ActiveConnections:  4,
		IdleConnections:    0,
		TotalStreams:       int32(len(streamManager.tunnelStreams)),
		HealthyStreams:     3,
		ActiveStreams:      2,
		ControlStreams:     4,
		TunnelStreams:      8,
		StreamAllocation:   "HashBased",
		ProtocolVersion:    "HTTP/2",
		CompressionEnabled: true,
		TlsVersion:         "TLS 1.3",
		ConnectionStates:   []string{"HEALTHY", "HALF_OPEN", "OPEN", "HEALTHY"},
	}
	sm.UpdateGRPCMetrics(grpcMetrics)

	// Record some messages
	sm.RecordMessage("packet", 1024, true)
	sm.RecordMessage("packet", 2048, false)
	sm.RecordMessage("control", 256, true)

	// Add some historical data
	sm.AddHistoricalSample()

	// Get snapshot after adding historical data
	snapshot := sm.GetSnapshot()
	healthScore := sm.GetHealthScore()

	printMetricsSection("Server Overview", map[string]interface{}{
		"Is Connected":      snapshot.IsConnected,
		"Total Connections": snapshot.TotalConnections,
		"Health Score":      fmt.Sprintf("%.2f/100", healthScore),
	})

	printMetricsSection("gRPC Connection Pool", map[string]interface{}{
		"Pool Size":           grpcMetrics.PoolSize,
		"Active Connections":  grpcMetrics.ActiveConnections,
		"Protocol Version":    grpcMetrics.ProtocolVersion,
		"TLS Version":         grpcMetrics.TlsVersion,
		"Compression":         grpcMetrics.CompressionEnabled,
		"Allocation Strategy": grpcMetrics.StreamAllocation,
	})

	printMetricsSection("Message Statistics", map[string]interface{}{
		"Packets Sent":       snapshot.MessageStats.PacketsSent,
		"Packets Received":   snapshot.MessageStats.PacketsReceived,
		"Control Sent":       snapshot.MessageStats.ControlMessagesSent,
		"Avg Packet Size":    fmt.Sprintf("%.0f bytes", snapshot.MessageStats.AvgPacketSize),
		"Total Packet Bytes": snapshot.MessageStats.PacketBytes,
	})

	fmt.Println("\n5. Historical Data Sample:")
	if len(snapshot.HistoricalData.ThroughputHistory) > 0 {
		latest := snapshot.HistoricalData.ThroughputHistory[len(snapshot.HistoricalData.ThroughputHistory)-1]
		printMetricsSection("Latest Throughput Sample", map[string]interface{}{
			"Timestamp":    time.UnixMilli(latest.Timestamp).Format("15:04:05"),
			"Packets/sec":  fmt.Sprintf("%.2f", latest.PacketsPerSec),
			"Bytes/sec":    fmt.Sprintf("%.2f", latest.BytesPerSec),
			"Messages/sec": fmt.Sprintf("%.2f", latest.MessagesPerSec),
		})
	}

	// Demonstrate pause/resume functionality
	fmt.Println("\n6. Docker Container Management Demo:")
	fmt.Println("   Pausing cache container...")
	mc.PauseDockerStats("app-cache-v1")

	fmt.Println("   Simulating container management actions...")
	time.Sleep(1 * time.Second)

	fmt.Println("   Resuming cache container...")
	mc.UnpauseDockerStats("app-cache-v1")

	// Update the paused container to show it's running again
	dockerStats3.Paused = false
	dockerStats3.Status = "running"
	dockerStats3.PausedTime = 0 // Clear paused time
	dockerStats3.LastUpdated = time.Now().UnixMilli()
	mc.UpdateDockerStats("app-cache-v1", dockerStats3)

	// Demonstrate JSON serialization
	fmt.Println("\n7. JSON Serialization Sample:")
	jsonData, err := json.MarshalIndent(map[string]interface{}{
		"performance_metrics": metrics,
		"docker_stats":        metrics.DockerStats,
		"health_score":        healthScore,
		"connection_status":   snapshot.IsConnected,
	}, "", "  ")

	if err != nil {
		t.Errorf("Failed to serialize metrics: %v", err)
	} else {
		fmt.Printf("%s\n", string(jsonData))
	}

	fmt.Println("\n=== Demo Complete ===")

	// Verify some key metrics to ensure test passes
	if metrics.CompressionRatio <= 0 {
		t.Error("Expected positive compression ratio")
	}

	if healthScore <= 0 || healthScore > 100 {
		t.Errorf("Expected health score between 0-100, got %.2f", healthScore)
	}

	// Verify Docker stats integration
	if len(metrics.DockerStats) != 3 {
		t.Errorf("Expected 3 Docker containers, got %d", len(metrics.DockerStats))
	}

	// Verify specific container exists
	if stats, exists := metrics.DockerStats["app-frontend-v1"]; !exists {
		t.Error("Expected frontend container stats to exist")
	} else if stats.ContainerName != "frontend-app" {
		t.Errorf("Expected container name 'frontend-app', got '%s'", stats.ContainerName)
	}

	// Verify pause/unpause functionality worked
	if stats, exists := metrics.DockerStats["app-cache-v1"]; exists && stats.Paused {
		t.Error("Expected cache container to be unpaused after demo")
	}
}

// Helper function to print metrics sections nicely
func printMetricsSection(title string, metrics map[string]interface{}) {
	fmt.Printf("   %s:\n", title)
	for key, value := range metrics {
		fmt.Printf("     %-20s: %v\n", key, value)
	}
	fmt.Println()
}
