package network

import (
	"fmt"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIPv6Address(t *testing.T) {
	addr := NewIPv6Address(RegionUSCentral1, NetworkPrivateGravity, "1234567890", "192.168.1.1", "172.17.0.2")
	assert.Equal(t, "fd15:d710:10b:8100:8a4:e567:ac11:2", addr.String())
	assert.Equal(t, "fd15:d710:10b:8100:8a4:e567::/96", addr.MachineSubnet())
}

func TestIPv6Address2(t *testing.T) {
	addr := NewIPv6Address(RegionUSEast1, NetworkPrivateGravity, "f109669b95881dfaa9d28f02df411d7a", "192.168.1.1", "172.17.0.2")
	assert.Equal(t, "fd15:d710:30a:e320:8a4:3f5:ac11:2", addr.String())
	assert.Equal(t, "fd15:d710:30a:e320:8a4:3f5::/96", addr.MachineSubnet())
}

func TestIPv6AddressAgentuityTenant(t *testing.T) {
	addr := NewIPv6Address(RegionGlobal, NetworkPrivateGravity, AgentuityTenantID, "192.168.1.1", "172.17.0.2")
	assert.Equal(t, "fd15:d710:7:ef0:8a4:2f05:ac11:2", addr.String())
	assert.Equal(t, "fd15:d710:7:ef0:8a4:2f05::/96", addr.MachineSubnet())
}

func TestIPv6AddressAgentuityTenant2(t *testing.T) {
	addr := NewIPv6Address(RegionGlobal, NetworkPrivateGravity, AgentuityTenantID, "192.168.1.2", "172.17.0.2")
	assert.Equal(t, "fd15:d710:7:ef0:d5d:2f05:ac11:2", addr.String())
	assert.Equal(t, "fd15:d710:7:ef0:d5d:2f05::/96", addr.MachineSubnet())
	addr = NewIPv6Address(RegionGlobal, NetworkPrivateGravity, AgentuityTenantID, "192.168.1.2", "172.17.0.1")
	assert.Equal(t, "fd15:d710:7:ef0:d5d:2f05:ac11:1", addr.String())
	assert.Equal(t, "fd15:d710:7:ef0:d5d:2f05::/96", addr.MachineSubnet())
}

func TestIPv6AddressGravityReplicas(t *testing.T) {
	addr := NewIPv6Address(RegionGlobal, NetworkPrivateGravity, AgentuityTenantID, "1", "0")
	assert.Equal(t, "fd15:d710:7:ef0:a71c:2f05::", addr.String())
	assert.Equal(t, "fd15:d710:7:ef0:a71c:2f05::/96", addr.MachineSubnet())
	ip := net.ParseIP(addr.String())
	assert.Equal(t, "fd15:d710:7:ef0:a71c:2f05::", addr.String())
	assert.Equal(t, "fd15:d710:7:ef0:a71c:2f05::", ip.String())
	addr = NewIPv6Address(RegionGlobal, NetworkPrivateGravity, AgentuityTenantID, "2", "0")
	assert.Equal(t, "fd15:d710:7:ef0:abd5:2f05::", addr.String())
	assert.Equal(t, "fd15:d710:7:ef0:abd5:2f05::/96", addr.MachineSubnet())
}

func TestTenantID(t *testing.T) {
	tenantID := "f109669b95881dfaa9d28f02df411d7a"
	tenantHash := hashTo49Bits(tenantID)
	assert.Equal(t, "1c65407ea86ba", fmt.Sprintf("%x", tenantHash))
}

func TestIPv6HostIds(t *testing.T) {
	addr := NewIPv6Address(RegionGlobal, NetworkPrivateGravity, AgentuityTenantID, "1", "1234")
	assert.Equal(t, "fd15:d710:7:ef0:a71c:2f05:22:fd", addr.String())
	assert.Equal(t, "fd15:d710:7:ef0:a71c:2f05::/96", addr.MachineSubnet())

	addr = NewIPv6Address(RegionGlobal, NetworkPrivateGravity, AgentuityTenantID, "1", "")
	assert.Equal(t, "fd15:d710:7:ef0:a71c:2f05::", addr.String())
	assert.Equal(t, "fd15:d710:7:ef0:a71c:2f05::/96", addr.MachineSubnet())

	addr = NewIPv6Address(RegionGlobal, NetworkPrivateGravity, AgentuityTenantID, "1", "0")
	assert.Equal(t, "fd15:d710:7:ef0:a71c:2f05::", addr.String())
	assert.Equal(t, "fd15:d710:7:ef0:a71c:2f05::/96", addr.MachineSubnet())

	addr = NewIPv6Address(RegionGlobal, NetworkPrivateGravity, AgentuityTenantID, "1", "00000")
	assert.Equal(t, "fd15:d710:7:ef0:a71c:2f05:fe:7f", addr.String())
	assert.Equal(t, "fd15:d710:7:ef0:a71c:2f05::/96", addr.MachineSubnet())

	addr = NewIPv6Address(RegionGlobal, NetworkPrivateGravity, AgentuityTenantID, "1", "abcdefghijklmnopqrstuvwxyz0123456789")
	assert.Equal(t, "fd15:d710:7:ef0:a71c:2f05:88:cd", addr.String())
	assert.Equal(t, "fd15:d710:7:ef0:a71c:2f05::/96", addr.MachineSubnet())
}

func TestOrgIpv6(t *testing.T) {
	addr1 := NewIPv6Address(RegionUSCentral1, NetworkHadron, "orgid", "", "1")
	addr2 := NewIPv6Address(RegionUSCentral1, NetworkHadron, "orgid", "", "2")
	assert.Equal(t, addr1.MachineSubnet(), addr2.MachineSubnet())
}

func TestGenerateUniqueIPv6(t *testing.T) {
	// Test that function returns a valid IPv6 address
	ip, err := GenerateUniqueIPv6()
	assert.NoError(t, err)
	assert.NotNil(t, ip)
	assert.Equal(t, 16, len(ip))

	// Must be IPv6 and carry the Agentuity prefix
	assert.Nil(t, ip.To4())
	assert.True(t, IsAgentuityIPv6Prefix(ip))

	// Test that multiple calls return different addresses (randomness)
	ip1, err := GenerateUniqueIPv6()
	assert.NoError(t, err)
	ip2, err := GenerateUniqueIPv6()
	assert.NoError(t, err)

	// They should be different (extremely unlikely to be same with random data)
	assert.NotEqual(t, ip1.String(), ip2.String())

	// Both should be valid IPv6 addresses
	assert.True(t, ip1.To16() != nil)
	assert.True(t, ip2.To16() != nil)
}

func TestIsAgentuityIPv6Prefix(t *testing.T) {
	ip, err := GenerateUniqueIPv6()
	assert.NoError(t, err)
	assert.True(t, IsAgentuityIPv6Prefix(ip))

	notAgent := net.ParseIP("2001:db8::1")
	assert.False(t, IsAgentuityIPv6Prefix(notAgent))
}

func TestGenerateServerIPv6FromIPv4(t *testing.T) {
	ipv4 := net.ParseIP("192.168.1.100")

	// Test normal case
	ip, ipNet, err := GenerateServerIPv6FromIPv4(RegionUSCentral1, NetworkPrivateGravity, "test-tenant", ipv4)
	assert.NoError(t, err)
	assert.NotNil(t, ip)
	assert.NotNil(t, ipNet)

	// Check that it's a valid IPv6 address
	assert.True(t, ip.To16() != nil)

	// Check that subnet is /96
	ones, bits := ipNet.Mask.Size()
	assert.Equal(t, 96, ones)
	assert.Equal(t, 128, bits)

	// Test that same inputs produce same results (deterministic)
	ip2, ipNet2, err2 := GenerateServerIPv6FromIPv4(RegionUSCentral1, NetworkPrivateGravity, "test-tenant", ipv4)
	assert.NoError(t, err2)
	assert.Equal(t, ip.String(), ip2.String())
	assert.Equal(t, ipNet.String(), ipNet2.String())

	// Test that different inputs produce different results
	ip3, _, err3 := GenerateServerIPv6FromIPv4(RegionUSWest1, NetworkPrivateGravity, "test-tenant", ipv4)
	assert.NoError(t, err3)
	assert.NotEqual(t, ip.String(), ip3.String())

	ip4, _, err4 := GenerateServerIPv6FromIPv4(RegionUSCentral1, NetworkAgent, "test-tenant", ipv4)
	assert.NoError(t, err4)
	assert.NotEqual(t, ip.String(), ip4.String())

	ip5, _, err5 := GenerateServerIPv6FromIPv4(RegionUSCentral1, NetworkPrivateGravity, "different-tenant", ipv4)
	assert.NoError(t, err5)
	assert.NotEqual(t, ip.String(), ip5.String())

	differentIPv4 := net.ParseIP("10.0.0.1")
	ip6, _, err6 := GenerateServerIPv6FromIPv4(RegionUSCentral1, NetworkPrivateGravity, "test-tenant", differentIPv4)
	assert.NoError(t, err6)
	assert.NotEqual(t, ip.String(), ip6.String())
}

func TestIPv6AddressesAreInMachineSubnet(t *testing.T) {
	testCases := []struct {
		name      string
		region    Region
		network   Network
		tenantID  string
		machineID string
		hostIDs   []string
	}{
		{
			name:      "agentuity tenant with various host IDs",
			region:    RegionGlobal,
			network:   NetworkPrivateGravity,
			tenantID:  AgentuityTenantID,
			machineID: "192.168.1.1",
			hostIDs:   []string{"172.17.0.1", "172.17.0.2", "10.0.0.1", "127.0.0.1", ""},
		},
		{
			name:      "us-central1 gravity network",
			region:    RegionUSCentral1,
			network:   NetworkPrivateGravity,
			tenantID:  "test-tenant-12345",
			machineID: "10.0.1.100",
			hostIDs:   []string{"172.20.0.1", "172.20.0.2", "172.20.0.3", "0"},
		},
		{
			name:      "us-west1 agent network",
			region:    RegionUSWest1,
			network:   NetworkAgent,
			tenantID:  "f109669b95881dfaa9d28f02df411d7a",
			machineID: "192.168.100.50",
			hostIDs:   []string{"172.17.0.10", "172.17.0.20", "invalid-ip", "1234"},
		},
		{
			name:      "us-east1 hadron network",
			region:    RegionUSEast1,
			network:   NetworkHadron,
			tenantID:  "another-tenant-id",
			machineID: "10.10.10.10",
			hostIDs:   []string{"172.18.0.1", "172.18.0.2", "", "0"},
		},
		{
			name:      "external customer network",
			region:    RegionUSCentral1,
			network:   NetworkExternalCustomer,
			tenantID:  "customer-tenant",
			machineID: "203.0.113.1",
			hostIDs:   []string{"172.19.0.1", "172.19.0.2"},
		},
		{
			name:      "private services network",
			region:    RegionGlobal,
			network:   NetworkPrivateServices,
			tenantID:  "services-tenant",
			machineID: "198.51.100.1",
			hostIDs:   []string{"172.21.0.1", "172.21.0.2"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Get the machine subnet
			machineSubnet := buildIPv6MachineSubnet(tc.region, tc.network, tc.tenantID, tc.machineID)
			_, subnet, err := net.ParseCIDR(machineSubnet)
			assert.NoError(t, err, "machine subnet should be valid CIDR")

			// Test each host ID in this machine
			for _, hostID := range tc.hostIDs {
				t.Run(fmt.Sprintf("hostID_%s", hostID), func(t *testing.T) {
					// Create IPv6 address
					addr := NewIPv6Address(tc.region, tc.network, tc.tenantID, tc.machineID, hostID)

					// Parse the IPv6 address
					ip := net.ParseIP(addr.String())
					assert.NotNil(t, ip, "IPv6 address should be valid")

					// Verify the address is within the machine subnet
					assert.True(t, subnet.Contains(ip),
						"IPv6 address %s should be contained in machine subnet %s",
						addr.String(), machineSubnet)

					// Verify the machine subnet matches what the address reports
					assert.Equal(t, machineSubnet, addr.MachineSubnet(),
						"machine subnet should match between direct calculation and address method")
				})
			}
		})
	}
}

func TestIPv6AddressSubnetConsistency(t *testing.T) {
	// Test that addresses with same region/network/tenant/machine but different hosts
	// all share the same machine subnet
	region := RegionUSCentral1
	network := NetworkPrivateGravity
	tenantID := "consistency-test-tenant"
	machineID := "192.168.1.100"

	hostIDs := []string{"172.17.0.1", "172.17.0.2", "172.17.0.3", "10.0.0.1", ""}

	var expectedSubnet string

	for i, hostID := range hostIDs {
		addr := NewIPv6Address(region, network, tenantID, machineID, hostID)
		machineSubnet := addr.MachineSubnet()

		if i == 0 {
			expectedSubnet = machineSubnet
		} else {
			assert.Equal(t, expectedSubnet, machineSubnet,
				"all addresses with same machine parameters should have same subnet")
		}

		// Verify the address is in the subnet
		_, subnet, err := net.ParseCIDR(machineSubnet)
		assert.NoError(t, err)

		ip := net.ParseIP(addr.String())
		assert.True(t, subnet.Contains(ip),
			"address %s should be in subnet %s", addr.String(), machineSubnet)
	}
}

func TestIPv6AddressDifferentMachineSubnets(t *testing.T) {
	// Test that different machine IDs produce different subnets
	region := RegionUSCentral1
	network := NetworkPrivateGravity
	tenantID := "subnet-diff-test"
	hostID := "172.17.0.1"

	machineIDs := []string{"192.168.1.1", "192.168.1.2", "10.0.0.1", "203.0.113.1"}
	seenSubnets := make(map[string]bool)

	for _, machineID := range machineIDs {
		addr := NewIPv6Address(region, network, tenantID, machineID, hostID)
		subnet := addr.MachineSubnet()

		// Each machine should have a unique subnet
		assert.False(t, seenSubnets[subnet],
			"subnet %s should be unique for machine %s", subnet, machineID)
		seenSubnets[subnet] = true

		// Verify the address is in its subnet
		_, subnetNet, err := net.ParseCIDR(subnet)
		assert.NoError(t, err)

		ip := net.ParseIP(addr.String())
		assert.True(t, subnetNet.Contains(ip),
			"address %s should be in its machine subnet %s", addr.String(), subnet)
	}
}

func TestGenerateServerIPv6SubnetContainment(t *testing.T) {
	// Test that GenerateServerIPv6FromIPv4 produces IPs contained in their subnets
	testCases := []struct {
		region   Region
		network  Network
		tenantID string
		ipv4     string
	}{
		{RegionUSCentral1, NetworkPrivateGravity, "server-test-1", "192.168.1.100"},
		{RegionUSWest1, NetworkAgent, "server-test-2", "10.0.1.50"},
		{RegionUSEast1, NetworkHadron, AgentuityTenantID, "203.0.113.1"},
		{RegionGlobal, NetworkPrivateServices, "global-services", "198.51.100.10"},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%d_%d_%s", tc.region, tc.network, tc.tenantID), func(t *testing.T) {
			ipv4 := net.ParseIP(tc.ipv4)
			assert.NotNil(t, ipv4, "IPv4 should be valid")

			ip, ipNet, err := GenerateServerIPv6FromIPv4(tc.region, tc.network, tc.tenantID, ipv4)
			assert.NoError(t, err)
			assert.NotNil(t, ip)
			assert.NotNil(t, ipNet)

			// Verify the generated IP is contained in the returned subnet
			assert.True(t, ipNet.Contains(ip),
				"generated IPv6 %s should be contained in subnet %s", ip.String(), ipNet.String())

			// Verify subnet is /96
			ones, bits := ipNet.Mask.Size()
			assert.Equal(t, 96, ones)
			assert.Equal(t, 128, bits)
		})
	}
}

func TestIPv6EdgeCasesInSubnet(t *testing.T) {
	// Test edge cases to ensure they're still contained in subnets
	testCases := []struct {
		name      string
		region    Region
		network   Network
		tenantID  string
		machineID string
		hostID    string
	}{
		{"empty host ID", RegionGlobal, NetworkPrivateGravity, AgentuityTenantID, "192.168.1.1", ""},
		{"zero host ID", RegionGlobal, NetworkPrivateGravity, AgentuityTenantID, "192.168.1.1", "0"},
		{"invalid IPv4 host", RegionUSCentral1, NetworkAgent, "test-tenant", "10.0.0.1", "invalid-ip"},
		{"numeric host ID", RegionUSWest1, NetworkHadron, "numeric-test", "172.16.1.1", "12345"},
		{"long string host", RegionUSEast1, NetworkExternalCustomer, "long-test", "203.0.113.5", "very-long-host-identifier-string"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			addr := NewIPv6Address(tc.region, tc.network, tc.tenantID, tc.machineID, tc.hostID)

			// Get machine subnet
			machineSubnet := addr.MachineSubnet()
			_, subnet, err := net.ParseCIDR(machineSubnet)
			assert.NoError(t, err)

			// Parse the IPv6 address
			ip := net.ParseIP(addr.String())
			assert.NotNil(t, ip, "IPv6 address should be parseable")

			// Verify containment
			assert.True(t, subnet.Contains(ip),
				"IPv6 address %s should be in subnet %s for case %s",
				addr.String(), machineSubnet, tc.name)
		})
	}
}
