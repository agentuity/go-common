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
