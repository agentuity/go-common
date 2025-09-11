package network

import (
	"fmt"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateNonOverlappingIPv4Subnet(t *testing.T) {
	// Test basic functionality with no existing networks
	subnet, gateway, err := GenerateNonOverlappingIPv4Subnet([]*net.IPNet{}, 24)
	assert.NoError(t, err)
	assert.NotNil(t, subnet)
	assert.NotNil(t, gateway)

	// Check that subnet has correct prefix length
	ones, bits := subnet.Mask.Size()
	assert.Equal(t, 24, ones)
	assert.Equal(t, 32, bits)

	// Check that gateway is the first IP in the subnet (.1)
	assert.Equal(t, byte(1), (*gateway)[3])

	// Check that generated subnet is within one of the private ranges
	isInPrivateRange := false
	for _, rng := range privateRanges {
		if rng.network.Contains(subnet.IP) {
			isInPrivateRange = true
			break
		}
	}
	assert.True(t, isInPrivateRange, "Generated subnet should be within private IP ranges")
}

func TestGenerateNonOverlappingIPv4SubnetWithExistingNetworks(t *testing.T) {
	// Create some existing networks
	_, existing1, _ := net.ParseCIDR("192.168.1.0/24")
	_, existing2, _ := net.ParseCIDR("192.168.2.0/24")
	existingNetworks := []*net.IPNet{existing1, existing2}

	subnet, gateway, err := GenerateNonOverlappingIPv4Subnet(existingNetworks, 24)
	assert.NoError(t, err)
	assert.NotNil(t, subnet)
	assert.NotNil(t, gateway)

	// Check that generated subnet doesn't overlap with existing ones
	for _, existing := range existingNetworks {
		assert.False(t, overlaps(existing, subnet), "Generated subnet should not overlap with existing networks")
	}

	// Gateway should be first IP in subnet
	assert.Equal(t, byte(1), (*gateway)[3])
}

func TestGenerateNonOverlappingIPv4SubnetDifferentPrefixLengths(t *testing.T) {
	testCases := []int{16, 20, 24, 28}

	for _, prefixLen := range testCases {
		t.Run(fmt.Sprintf("prefix_%d", prefixLen), func(t *testing.T) {
			subnet, gateway, err := GenerateNonOverlappingIPv4Subnet([]*net.IPNet{}, prefixLen)
			assert.NoError(t, err)
			assert.NotNil(t, subnet)
			assert.NotNil(t, gateway)

			// Check prefix length
			ones, bits := subnet.Mask.Size()
			assert.Equal(t, prefixLen, ones)
			assert.Equal(t, 32, bits)

			// Gateway should be first IP
			assert.Equal(t, byte(1), (*gateway)[3])
		})
	}
}

func TestGenerateNonOverlappingIPv4SubnetDeterministicGateway(t *testing.T) {
	// Test multiple generations to ensure gateway is always .1
	for range 5 {
		subnet, gateway, err := GenerateNonOverlappingIPv4Subnet([]*net.IPNet{}, 24)
		assert.NoError(t, err)
		assert.NotNil(t, subnet)
		assert.NotNil(t, gateway)

		// Gateway should always be the first IP (.1)
		subnetIP := subnet.IP.To4()
		gatewayIP := (*gateway).To4()

		// First 3 octets should match the subnet
		assert.Equal(t, subnetIP[0], gatewayIP[0])
		assert.Equal(t, subnetIP[1], gatewayIP[1])
		assert.Equal(t, subnetIP[2], gatewayIP[2])

		// Last octet should be 1
		assert.Equal(t, byte(1), gatewayIP[3])
	}
}

func TestGenerateNonOverlappingIPv4SubnetWithManyExisting(t *testing.T) {
	// This test now passes after fixing the overlap detection logic

	var existingNetworks []*net.IPNet
	for i := range 10 {
		_, network, _ := net.ParseCIDR(fmt.Sprintf("192.168.%d.0/24", i))
		existingNetworks = append(existingNetworks, network)
	}

	subnet, gateway, err := GenerateNonOverlappingIPv4Subnet(existingNetworks, 24)
	assert.NoError(t, err)
	assert.NotNil(t, subnet)
	assert.NotNil(t, gateway)

	// This should pass when the bug is fixed
	for _, existing := range existingNetworks {
		assert.False(t, overlaps(existing, subnet), "Generated subnet should not overlap with any existing networks")
	}
}

func TestGenerate100NonOverlapping24Subnets(t *testing.T) {
	var existingNetworks []*net.IPNet
	generatedSubnets := make(map[string]*net.IPNet)

	// Generate 100 /24 subnets - should be easy within private ranges
	for i := 0; i < 100; i++ {
		subnet, gateway, err := GenerateNonOverlappingIPv4Subnet(existingNetworks, 24)
		assert.NoError(t, err, "Failed to generate subnet %d", i+1)
		assert.NotNil(t, subnet, "Subnet %d is nil", i+1)
		assert.NotNil(t, gateway, "Gateway %d is nil", i+1)

		// Ensure subnet is unique
		subnetStr := subnet.String()
		_, exists := generatedSubnets[subnetStr]
		assert.False(t, exists, "Duplicate subnet generated: %s", subnetStr)
		generatedSubnets[subnetStr] = subnet

		// Verify no overlap with existing networks
		for _, existing := range existingNetworks {
			assert.False(t, overlaps(existing, subnet), "New subnet %s overlaps with existing %s", subnet.String(), existing.String())
		}

		// Add to existing networks for next iteration
		existingNetworks = append(existingNetworks, subnet)
	}

	t.Logf("Successfully generated %d non-overlapping /24 subnets", len(generatedSubnets))
}

func TestGenerate500NonOverlapping24Subnets(t *testing.T) {
	var existingNetworks []*net.IPNet
	generatedSubnets := make(map[string]*net.IPNet)

	// Generate 500 /24 subnets - still should be manageable
	for i := 0; i < 500; i++ {
		subnet, gateway, err := GenerateNonOverlappingIPv4Subnet(existingNetworks, 24)
		assert.NoError(t, err, "Failed to generate subnet %d", i+1)
		assert.NotNil(t, subnet, "Subnet %d is nil", i+1)
		assert.NotNil(t, gateway, "Gateway %d is nil", i+1)

		// Ensure subnet is unique
		subnetStr := subnet.String()
		_, exists := generatedSubnets[subnetStr]
		assert.False(t, exists, "Duplicate subnet generated: %s", subnetStr)
		generatedSubnets[subnetStr] = subnet

		// Add to existing networks for next iteration
		existingNetworks = append(existingNetworks, subnet)
		
		// Log progress every 100 subnets
		if (i+1)%100 == 0 {
			t.Logf("Generated %d subnets so far...", i+1)
		}
	}

	t.Logf("Successfully generated %d non-overlapping /24 subnets", len(generatedSubnets))
}

func TestGenerate1000NonOverlapping24Subnets(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long test in short mode")
	}

	var existingNetworks []*net.IPNet
	generatedSubnets := make(map[string]*net.IPNet)

	// Generate 1000 /24 subnets - this should expose any bugs in the algorithm
	for i := 0; i < 1000; i++ {
		subnet, gateway, err := GenerateNonOverlappingIPv4Subnet(existingNetworks, 24)
		assert.NoError(t, err, "Failed to generate subnet %d", i+1)
		assert.NotNil(t, subnet, "Subnet %d is nil", i+1)
		assert.NotNil(t, gateway, "Gateway %d is nil", i+1)

		// Ensure subnet is unique
		subnetStr := subnet.String()
		_, exists := generatedSubnets[subnetStr]
		assert.False(t, exists, "Duplicate subnet generated: %s", subnetStr)
		generatedSubnets[subnetStr] = subnet

		// Add to existing networks for next iteration
		existingNetworks = append(existingNetworks, subnet)
		
		// Log progress every 200 subnets
		if (i+1)%200 == 0 {
			t.Logf("Generated %d subnets so far...", i+1)
		}
	}

	t.Logf("Successfully generated %d non-overlapping /24 subnets", len(generatedSubnets))
}

func TestSubnetGenerationDistribution(t *testing.T) {
	// Test that subnets are generated across all private ranges
	rangeCounts := map[string]int{
		"192.168.0.0/16": 0,
		"172.16.0.0/12":  0,
		"10.0.0.0/8":     0,
	}

	// Generate 300 subnets to see distribution
	for i := 0; i < 300; i++ {
		subnet, _, err := GenerateNonOverlappingIPv4Subnet([]*net.IPNet{}, 24)
		assert.NoError(t, err)
		assert.NotNil(t, subnet)

		// Determine which private range this subnet falls into
		for _, rng := range privateRanges {
			if rng.network.Contains(subnet.IP) {
				rangeCounts[rng.network.String()]++
				break
			}
		}
	}

	t.Logf("Subnet distribution across private ranges:")
	for rangeStr, count := range rangeCounts {
		t.Logf("  %s: %d subnets", rangeStr, count)
	}

	// At least some subnets should be generated in each range
	// (This might fail if there's a bug in range selection)
	totalGenerated := rangeCounts["192.168.0.0/16"] + rangeCounts["172.16.0.0/12"] + rangeCounts["10.0.0.0/8"]
	assert.Equal(t, 300, totalGenerated, "All generated subnets should be in private ranges")
	
	// With randomization, we should have better distribution
	// Each range should have at least some subnets (not expecting perfect distribution due to randomness)
	for rangeStr, count := range rangeCounts {
		assert.Greater(t, count, 50, "Range %s should have a reasonable number of subnets with randomization", rangeStr)
	}
}

func TestRandomizedRangeSelection(t *testing.T) {
	// Test multiple runs to verify range selection is randomized
	rangeFirstSelected := map[string]int{
		"192.168.0.0/16": 0,
		"172.16.0.0/12":  0,
		"10.0.0.0/8":     0,
	}

	// Run 30 tests and see which range gets the first subnet
	for run := 0; run < 30; run++ {
		subnet, _, err := GenerateNonOverlappingIPv4Subnet([]*net.IPNet{}, 24)
		assert.NoError(t, err)
		assert.NotNil(t, subnet)

		// Determine which range this first subnet came from
		for _, rng := range privateRanges {
			if rng.network.Contains(subnet.IP) {
				rangeFirstSelected[rng.network.String()]++
				break
			}
		}
	}

	t.Logf("First subnet selections across ranges:")
	for rangeStr, count := range rangeFirstSelected {
		t.Logf("  %s: %d times selected first", rangeStr, count)
	}

	// With randomization, each range should be selected first at least once in 30 runs
	// (this has a very small chance of failing due to randomness, but very unlikely)
	totalSelections := 0
	for _, count := range rangeFirstSelected {
		totalSelections += count
	}
	assert.Equal(t, 30, totalSelections, "Should have 30 total selections")
	
	// Each range should be selected at least a few times (not zero)
	nonZeroRanges := 0
	for _, count := range rangeFirstSelected {
		if count > 0 {
			nonZeroRanges++
		}
	}
	assert.GreaterOrEqual(t, nonZeroRanges, 2, "At least 2 ranges should be selected first across 30 runs")
}

func TestSubnetGenerationWithMixed24Networks(t *testing.T) {
	// Start with some existing /24 networks in different ranges
	existingNetworks := []*net.IPNet{}
	
	// Add some 192.168.x.0/24 networks
	for i := 0; i < 10; i++ {
		_, network, _ := net.ParseCIDR(fmt.Sprintf("192.168.%d.0/24", i))
		existingNetworks = append(existingNetworks, network)
	}
	
	// Add some 172.16.x.0/24 networks
	for i := 0; i < 10; i++ {
		_, network, _ := net.ParseCIDR(fmt.Sprintf("172.16.%d.0/24", i))
		existingNetworks = append(existingNetworks, network)
	}
	
	// Add some 10.x.0.0/24 networks
	for i := 0; i < 10; i++ {
		_, network, _ := net.ParseCIDR(fmt.Sprintf("10.%d.0.0/24", i))
		existingNetworks = append(existingNetworks, network)
	}

	t.Logf("Starting with %d existing /24 networks", len(existingNetworks))

	// Now generate 100 more non-overlapping subnets
	generatedCount := 0
	for i := 0; i < 100; i++ {
		subnet, gateway, err := GenerateNonOverlappingIPv4Subnet(existingNetworks, 24)
		assert.NoError(t, err, "Failed to generate subnet %d with %d existing networks", i+1, len(existingNetworks))
		assert.NotNil(t, subnet)
		assert.NotNil(t, gateway)

		// Verify no overlap
		for _, existing := range existingNetworks {
			assert.False(t, overlaps(existing, subnet), "Generated subnet %s overlaps with existing %s", subnet.String(), existing.String())
		}

		existingNetworks = append(existingNetworks, subnet)
		generatedCount++
	}

	assert.Equal(t, 100, generatedCount, "Should have generated 100 additional subnets")
	t.Logf("Successfully generated %d additional /24 subnets", generatedCount)
}

func TestSubnetCapacityCalculation(t *testing.T) {
	// Test that we can generate many /24 subnets without exhausting capacity
	testCases := []struct {
		name             string
		expectedCapacity int
	}{
		{"small_set", 100},
		{"medium_set", 500},
		{"large_set", 1000},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var existingNetworks []*net.IPNet
			generatedCount := 0
			
			for i := 0; i < tc.expectedCapacity; i++ {
				subnet, gateway, err := GenerateNonOverlappingIPv4Subnet(existingNetworks, 24)
				if err != nil {
					t.Logf("Failed to generate subnet %d: %v", i+1, err)
					break
				}
				
				assert.NotNil(t, subnet)
				assert.NotNil(t, gateway)
				
				// Verify subnet is in private ranges
				isInPrivateRange := false
				for _, rng := range privateRanges {
					if rng.network.Contains(subnet.IP) {
						isInPrivateRange = true
						break
					}
				}
				assert.True(t, isInPrivateRange, "Generated subnet %s should be in private ranges", subnet.String())
				
				existingNetworks = append(existingNetworks, subnet)
				generatedCount++
			}
			
			t.Logf("Generated %d/%d subnets", generatedCount, tc.expectedCapacity)
			assert.Equal(t, tc.expectedCapacity, generatedCount, "Should generate expected number of subnets")
		})
	}
}

func TestExhaustSingleRange(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping exhaustive test in short mode")
	}
	
	// Fill up the 192.168.0.0/16 range completely (256 /24 subnets)
	var existingNetworks []*net.IPNet
	
	// Generate all 256 possible /24 subnets in 192.168.0.0/16
	for i := 0; i < 256; i++ {
		_, network, _ := net.ParseCIDR(fmt.Sprintf("192.168.%d.0/24", i))
		existingNetworks = append(existingNetworks, network)
	}
	
	t.Logf("Filled 192.168.0.0/16 with %d subnets", len(existingNetworks))
	
	// Now generate more - should use other ranges
	for i := 0; i < 100; i++ {
		subnet, gateway, err := GenerateNonOverlappingIPv4Subnet(existingNetworks, 24)
		assert.NoError(t, err, "Should be able to generate subnet %d after exhausting 192.168.0.0/16", i+1)
		assert.NotNil(t, subnet)
		assert.NotNil(t, gateway)
		
		// Should not be in 192.168.0.0/16
		_, range192, _ := net.ParseCIDR("192.168.0.0/16")
		assert.False(t, range192.Contains(subnet.IP), "Subnet %s should not be in exhausted 192.168.0.0/16 range", subnet.String())
		
		// Should be in 172.16.0.0/12 or 10.0.0.0/8
		_, range172, _ := net.ParseCIDR("172.16.0.0/12")
		_, range10, _ := net.ParseCIDR("10.0.0.0/8")
		inOtherRange := range172.Contains(subnet.IP) || range10.Contains(subnet.IP)
		assert.True(t, inOtherRange, "Subnet %s should be in 172.16.0.0/12 or 10.0.0.0/8", subnet.String())
		
		existingNetworks = append(existingNetworks, subnet)
	}
	
	t.Logf("Successfully generated 100 additional subnets after exhausting 192.168.0.0/16")
}

func TestOverlaps(t *testing.T) {
	_, net1, _ := net.ParseCIDR("192.168.1.0/24")
	_, net2, _ := net.ParseCIDR("192.168.2.0/24")
	_, net3, _ := net.ParseCIDR("192.168.1.0/25") // Overlaps with net1
	_, net4, _ := net.ParseCIDR("192.168.0.0/16") // Contains net1 and net2

	// Test non-overlapping networks
	assert.False(t, overlaps(net1, net2))
	assert.False(t, overlaps(net2, net1))

	// Test overlapping networks
	assert.True(t, overlaps(net1, net3))
	assert.True(t, overlaps(net3, net1))

	// Test containing networks
	assert.True(t, overlaps(net1, net4))
	assert.True(t, overlaps(net4, net1))
	assert.True(t, overlaps(net2, net4))
	assert.True(t, overlaps(net4, net2))
}

func TestMustParseCIDR(t *testing.T) {
	// Test valid CIDR
	network := mustParseCIDR("192.168.1.0/24")
	assert.NotNil(t, network)
	assert.Equal(t, "192.168.1.0", network.IP.String())

	ones, bits := network.Mask.Size()
	assert.Equal(t, 24, ones)
	assert.Equal(t, 32, bits)

	// Test panic on invalid CIDR
	assert.Panics(t, func() {
		mustParseCIDR("invalid-cidr")
	})
}
