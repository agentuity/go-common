package network

import (
	"math/rand"
	"net"
	"testing"
)

// BenchmarkSubnetGeneration measures the performance of subnet generation
func BenchmarkSubnetGeneration(b *testing.B) {
	// Save and restore original RNG
	rngMu.Lock()
	originalRNG := rng
	rngMu.Unlock()

	defer func() {
		rngMu.Lock()
		rng = originalRNG
		rngMu.Unlock()
	}()

	// Use a seeded RNG for consistent benchmarking
	rngMu.Lock()
	rng = rand.New(rand.NewSource(42))
	rngMu.Unlock()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _, err := GenerateNonOverlappingIPv4Subnet([]*net.IPNet{}, 24)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSubnetGenerationWithExisting measures performance when avoiding existing subnets
func BenchmarkSubnetGenerationWithExisting(b *testing.B) {
	// Save and restore original RNG
	rngMu.Lock()
	originalRNG := rng
	rngMu.Unlock()

	defer func() {
		rngMu.Lock()
		rng = originalRNG
		rngMu.Unlock()
	}()

	// Use a seeded RNG for consistent benchmarking
	rngMu.Lock()
	rng = rand.New(rand.NewSource(42))
	rngMu.Unlock()

	// Create some existing networks
	var existingNetworks []*net.IPNet
	for i := 0; i < 100; i++ {
		subnet, _, err := GenerateNonOverlappingIPv4Subnet(existingNetworks, 24)
		if err != nil {
			b.Fatal(err)
		}
		existingNetworks = append(existingNetworks, subnet)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _, err := GenerateNonOverlappingIPv4Subnet(existingNetworks, 24)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkHighVolumeGeneration measures performance when generating many subnets
func BenchmarkHighVolumeGeneration(b *testing.B) {
	// Save and restore original RNG
	rngMu.Lock()
	originalRNG := rng
	rngMu.Unlock()

	defer func() {
		rngMu.Lock()
		rng = originalRNG
		rngMu.Unlock()
	}()

	// Use a seeded RNG for consistent benchmarking
	rngMu.Lock()
	rng = rand.New(rand.NewSource(42))
	rngMu.Unlock()

	// Pre-generate base set outside of benchmark timing
	var baseNetworks []*net.IPNet
	for j := 0; j < 900; j++ {
		subnet, _, err := GenerateNonOverlappingIPv4Subnet(baseNetworks, 24)
		if err != nil {
			b.Fatal(err)
		}
		baseNetworks = append(baseNetworks, subnet)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Copy base networks for isolation
		existingNetworks := make([]*net.IPNet, len(baseNetworks))
		copy(existingNetworks, baseNetworks)

		// Benchmark generating the next 100 subnets
		for j := 0; j < 100; j++ {
			subnet, _, err := GenerateNonOverlappingIPv4Subnet(existingNetworks, 24)
			if err != nil {
				b.Fatal(err)
			}
			existingNetworks = append(existingNetworks, subnet)
		}
	}
}
