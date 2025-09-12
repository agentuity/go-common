package network

import (
	"math/rand"
	"net"
	"testing"
)

// BenchmarkSubnetGeneration measures the performance of subnet generation
func BenchmarkSubnetGeneration(b *testing.B) {
	// Save and restore original RNG
	originalRNG := rng
	defer func() { rng = originalRNG }()

	// Use a seeded RNG for consistent benchmarking
	rng = rand.New(rand.NewSource(42))

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
	originalRNG := rng
	defer func() { rng = originalRNG }()

	// Use a seeded RNG for consistent benchmarking
	rng = rand.New(rand.NewSource(42))

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
	originalRNG := rng
	defer func() { rng = originalRNG }()

	// Use a seeded RNG for consistent benchmarking
	rng = rand.New(rand.NewSource(42))

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var existingNetworks []*net.IPNet

		// Generate 1000 subnets in each benchmark iteration
		for j := 0; j < 1000; j++ {
			subnet, _, err := GenerateNonOverlappingIPv4Subnet(existingNetworks, 24)
			if err != nil {
				b.Fatal(err)
			}
			existingNetworks = append(existingNetworks, subnet)
		}
	}
}
