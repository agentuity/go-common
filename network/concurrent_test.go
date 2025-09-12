package network

import (
	"fmt"
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConcurrentSubnetGeneration(t *testing.T) {
	const numGoroutines = 10
	const subnetsPerGoroutine = 50

	var wg sync.WaitGroup
	var mu sync.Mutex
	allSubnets := make(map[string]bool)
	errors := make([]error, 0)

	for i := range numGoroutines {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for range subnetsPerGoroutine {
				subnet, gateway, err := GenerateNonOverlappingIPv4Subnet([]*net.IPNet{}, 24)

				mu.Lock()
				if err != nil {
					errors = append(errors, err)
				} else {
					// Note: Duplicates are expected here because each call is independent
					// The function only avoids overlaps with the provided existingNetworks slice
					subnetStr := subnet.String()
					allSubnets[subnetStr] = true

					// Basic validation
					if subnet == nil || gateway == nil {
						t.Errorf("Got nil subnet or gateway")
					}
				}
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	// Check results
	assert.Empty(t, errors, "Should have no errors in concurrent generation")

	// With concurrent access and no shared state, duplicates are expected
	// The important thing is that the function doesn't crash or corrupt state
	totalCalls := numGoroutines * subnetsPerGoroutine
	uniqueSubnets := len(allSubnets)

	t.Logf("Generated %d unique subnets from %d total calls across %d goroutines",
		uniqueSubnets, totalCalls, numGoroutines)

	// We should have at least some subnets generated
	assert.Greater(t, uniqueSubnets, 0, "Should generate at least some subnets")
	assert.LessOrEqual(t, uniqueSubnets, totalCalls, "Unique subnets should not exceed total calls")
}

func TestConcurrentWithExistingSubnets(t *testing.T) {
	// Pre-generate some existing subnets
	var existingNetworks []*net.IPNet
	for range 100 {
		subnet, _, err := GenerateNonOverlappingIPv4Subnet(existingNetworks, 24)
		assert.NoError(t, err)
		existingNetworks = append(existingNetworks, subnet)
	}

	const numGoroutines = 5
	const subnetsPerGoroutine = 20

	var wg sync.WaitGroup
	var mu sync.Mutex
	allSubnets := make(map[string]bool)
	errors := make([]error, 0)

	for i := range numGoroutines {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for range subnetsPerGoroutine {
				subnet, gateway, err := GenerateNonOverlappingIPv4Subnet(existingNetworks, 24)

				mu.Lock()
				if err != nil {
					errors = append(errors, err)
				} else {
					subnetStr := subnet.String()

					// Check against existing networks - this should never happen
					for _, existing := range existingNetworks {
						if existing.String() == subnetStr {
							t.Errorf("Generated subnet overlaps with existing: %s", subnetStr)
						}
					}

					// Note: Duplicates between concurrent calls are expected
					// Each call is independent and doesn't know about other concurrent results
					allSubnets[subnetStr] = true

					// Basic validation
					if subnet == nil || gateway == nil {
						t.Errorf("Got nil subnet or gateway")
					}
				}
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	// Check results
	assert.Empty(t, errors, "Should have no errors in concurrent generation with existing subnets")

	// With concurrent access, some duplicates between goroutines are expected
	totalCalls := numGoroutines * subnetsPerGoroutine
	uniqueSubnets := len(allSubnets)

	// We should have at least some subnets generated
	assert.Greater(t, uniqueSubnets, 0, "Should generate at least some subnets")
	assert.LessOrEqual(t, uniqueSubnets, totalCalls, "Unique subnets should not exceed total calls")

	t.Logf("Generated %d unique subnets from %d total calls across %d goroutines with %d existing subnets",
		uniqueSubnets, totalCalls, numGoroutines, len(existingNetworks))
}

func TestConcurrentThreadSafety(t *testing.T) {
	// This test focuses on thread safety of the RNG, not uniqueness of results
	const numGoroutines = 20
	const iterations = 100

	var wg sync.WaitGroup
	var mu sync.Mutex
	errors := make([]error, 0)
	totalGenerated := 0

	for range i := numGoroutines {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			localErrors := make([]error, 0)
			localCount := 0

			for range iterations {
				subnet, gateway, err := GenerateNonOverlappingIPv4Subnet([]*net.IPNet{}, 24)

				if err != nil {
					localErrors = append(localErrors, err)
				} else if subnet == nil || gateway == nil {
					localErrors = append(localErrors, fmt.Errorf("got nil subnet or gateway"))
				} else {
					localCount++
				}
			}

			mu.Lock()
			errors = append(errors, localErrors...)
			totalGenerated += localCount
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	// Thread safety check - no errors should occur
	assert.Empty(t, errors, "Thread-safe RNG should not cause any errors")

	expectedTotal := numGoroutines * iterations
	assert.Equal(t, expectedTotal, totalGenerated, "Should generate expected total number of subnets")

	t.Logf("Successfully generated %d subnets across %d concurrent goroutines without errors",
		totalGenerated, numGoroutines)
}
