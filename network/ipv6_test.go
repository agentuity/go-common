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

	// Test exact /32 prefix matching - only first two hextets matter
	exactMatch := net.ParseIP("fd15:d710:ffff:ffff:ffff:ffff:ffff:ffff")
	assert.True(t, IsAgentuityIPv6Prefix(exactMatch), "Should match exact /32 prefix fd15:d710")

	// Test that similar but different prefixes don't match
	closeButWrong1 := net.ParseIP("fd15:d711::1") // Third hextet differs by 1
	assert.False(t, IsAgentuityIPv6Prefix(closeButWrong1), "Should not match fd15:d711 (differs in second hextet)")

	closeButWrong2 := net.ParseIP("fd14:d710::1") // Second hextet differs by 1
	assert.False(t, IsAgentuityIPv6Prefix(closeButWrong2), "Should not match fd14:d710 (differs in first hextet)")
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
	// Parse the service subnet for validation
	_, serviceSubnet, err := net.ParseCIDR(InternalServiceSubnet)
	assert.NoError(t, err, "ServiceSubnet should be a valid CIDR")

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
		// Add a NetworkPrivateServices case to test against ServiceSubnet
		{"private services network", RegionGlobal, NetworkPrivateServices, "service-tenant", "198.51.100.1", "172.21.0.1"},
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

			// Verify containment in machine subnet
			assert.True(t, subnet.Contains(ip),
				"IPv6 address %s should be in subnet %s for case %s",
				addr.String(), machineSubnet, tc.name)

			// For NetworkPrivateServices, validate that the address and machine subnet
			// are within the ServiceSubnet
			if tc.network == NetworkPrivateServices {
				assert.True(t, serviceSubnet.Contains(ip),
					"IPv6 address %s should be contained in ServiceSubnet %s for NetworkPrivateServices",
					addr.String(), InternalServiceSubnet)

				// Verify that the machine subnet is within the service subnet
				// Parse the machine subnet IP to check if it's within service subnet
				machineSubnetIP := subnet.IP
				assert.True(t, serviceSubnet.Contains(machineSubnetIP),
					"Machine subnet IP %s should be contained in ServiceSubnet %s for NetworkPrivateServices",
					machineSubnetIP.String(), InternalServiceSubnet)
			}

			// For all cases, verify the address has the Agentuity prefix
			assert.True(t, IsAgentuityIPv6Prefix(ip),
				"IPv6 address should have Agentuity prefix for case %s", tc.name)
		})
	}
}

func TestServicesMapInServiceSubnet(t *testing.T) {
	// Parse the service subnet from the generated constants
	_, serviceSubnet, err := net.ParseCIDR(InternalServiceSubnet)
	assert.NoError(t, err, "InternalServiceSubnet should be a valid CIDR")

	// Validate each service IP in the Services map
	for ipStr, serviceName := range Services {
		t.Run(serviceName, func(t *testing.T) {
			// Parse the service IP address
			ip := net.ParseIP(ipStr)
			assert.NotNil(t, ip, "Service IP %s for %s should be valid", ipStr, serviceName)

			// Verify the service IP is contained within the service subnet
			assert.True(t, serviceSubnet.Contains(ip),
				"Service IP %s for %s should be contained in service subnet %s",
				ipStr, serviceName, InternalServiceSubnet)

			// Verify it's an IPv6 address (not IPv4)
			assert.NotNil(t, ip.To16(), "Service IP should be IPv6")
			assert.Nil(t, ip.To4(), "Service IP should not be IPv4")

			// Verify it has the Agentuity prefix
			assert.True(t, IsAgentuityIPv6Prefix(ip),
				"Service IP %s should have Agentuity IPv6 prefix", ipStr)
		})
	}
}

func TestAddressesMapConsistency(t *testing.T) {
	// Verify that Addresses map is consistent with Services map
	for serviceName, ip := range Addresses {
		t.Run(serviceName, func(t *testing.T) {
			// Check that the IP exists in Services map
			actualServiceName, exists := Services[ip.String()]
			assert.True(t, exists, "IP %s should exist in Services map", ip)
			assert.Equal(t, serviceName, actualServiceName,
				"Service name should match between Addresses and Services maps")
		})
	}

	// Verify reverse mapping consistency
	for ipStr, serviceName := range Services {
		t.Run(serviceName+"_reverse", func(t *testing.T) {
			// Check that the service name exists in Addresses map
			actualIP, exists := Addresses[serviceName]
			assert.True(t, exists, "Service %s should exist in Addresses map", serviceName)
			assert.Equal(t, ipStr, actualIP.String(),
				"IP address should match between Services and Addresses maps")
		})
	}
}

func TestServiceSubnetSize(t *testing.T) {
	_, serviceSubnet, err := net.ParseCIDR(InternalServiceSubnet)
	assert.NoError(t, err, "ServiceSubnet should be a valid CIDR")

	// Verify it's the expected size (/44)
	ones, bits := serviceSubnet.Mask.Size()
	assert.Equal(t, 44, ones, "Service subnet should be /44")
	assert.Equal(t, 128, bits, "Service subnet should be IPv6 (128 bits)")

	// Verify it has the Agentuity prefix
	assert.True(t, IsAgentuityIPv6Prefix(serviceSubnet.IP),
		"Service subnet should have Agentuity IPv6 prefix")
}

func TestAllServiceConstants(t *testing.T) {
	// Test individual service constants are in the service subnet
	_, serviceSubnet, err := net.ParseCIDR(InternalServiceSubnet)
	assert.NoError(t, err, "ServiceSubnet should be a valid CIDR")

	testCases := []struct {
		name string
		ip   string
	}{
		{"aether", AetherServiceIP},
		{"catalyst", CatalystServiceIP},
		{"otel", OtelServiceIP},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			assert.NotNil(t, ip, "Service IP constant should be valid")

			assert.True(t, serviceSubnet.Contains(ip),
				"Service IP constant %s should be in service subnet %s",
				tc.ip, InternalServiceSubnet)

			assert.True(t, IsAgentuityIPv6Prefix(ip),
				"Service IP constant should have Agentuity prefix")

			// Verify consistency with maps
			serviceName, existsInServices := Services[tc.ip]
			assert.True(t, existsInServices, "Service IP should exist in Services map")
			assert.Equal(t, tc.name, serviceName, "Service name should match")

			addressIP, existsInAddresses := Addresses[tc.name]
			assert.True(t, existsInAddresses, "Service name should exist in Addresses map")
			assert.Equal(t, tc.ip, addressIP.String(), "Service IP should match")
		})
	}
}

func TestGetRegionGCP(t *testing.T) {
	testCases := []struct {
		region   string
		expected Region
	}{
		{"us-central1", RegionUSCentral1},
		{"us-central2", RegionUSCentral2},
		{"US-CENTRAL1", RegionUSCentral1},
		{"us-west1", RegionUSWest1},
		{"us-west2", RegionUSWest2},
		{"us-west3", RegionUSWest3},
		{"us-west4", RegionUSWest4},
		{"US-WEST1", RegionUSWest1},
		{"us-east1", RegionUSEast1},
		{"us-east4", RegionUSEast2},
		{"us-east5", RegionUSEast3},
		{"US-EAST1", RegionUSEast1},
	}

	for _, tc := range testCases {
		t.Run(tc.region, func(t *testing.T) {
			result := GetRegion(tc.region)
			assert.Equal(t, tc.expected, result, "GCP region %s should map to %v", tc.region, tc.expected)
		})
	}
}

func TestGetRegionAWS(t *testing.T) {
	testCases := []struct {
		region   string
		expected Region
	}{
		{"us-east-1", RegionUSEast1},
		{"us-east-2", RegionUSEast2},
		{"US-EAST-1", RegionUSEast1},
		{"us-west-1", RegionUSWest1},
		{"us-west-2", RegionUSWest2},
		{"US-WEST-1", RegionUSWest1},
	}

	for _, tc := range testCases {
		t.Run(tc.region, func(t *testing.T) {
			result := GetRegion(tc.region)
			assert.Equal(t, tc.expected, result, "AWS region %s should map to %v", tc.region, tc.expected)
		})
	}
}

func TestGetRegionAzure(t *testing.T) {
	testCases := []struct {
		region   string
		expected Region
	}{
		{"eastus", RegionUSEast1},
		{"eastus2", RegionUSEast2},
		{"EastUS", RegionUSEast1},
		{"EASTUS2", RegionUSEast2},
		{"westus", RegionUSWest1},
		{"westus2", RegionUSWest2},
		{"westus3", RegionUSWest3},
		{"WestUS", RegionUSWest1},
		{"centralus", RegionUSCentral1},
		{"northcentralus", RegionUSCentral2},
		{"southcentralus", RegionUSCentral3},
		{"CentralUS", RegionUSCentral1},
		{"westcentralus", RegionUSCentral1},
	}

	for _, tc := range testCases {
		t.Run(tc.region, func(t *testing.T) {
			result := GetRegion(tc.region)
			assert.Equal(t, tc.expected, result, "Azure region %s should map to %v", tc.region, tc.expected)
		})
	}
}

func TestGetRegionFallback(t *testing.T) {
	testCases := []struct {
		region   string
		expected Region
	}{
		{"europe-west1", RegionEUWest1},
		{"europe-west2", RegionEUWest2},
		{"europe-west3", RegionEUWest3},
		{"europe-north1", RegionEUEast1},
		{"eu-west-1", RegionEUWest1},
		{"eu-central-1", RegionEUEast1},
		{"westeurope", RegionEUWest1},
		{"asia-east1", RegionGlobal},
		{"australia-southeast1", RegionGlobal},
		{"unknown-region", RegionGlobal},
		{"", RegionGlobal},
		{"global", RegionGlobal},
		{"GLOBAL", RegionGlobal},
	}

	for _, tc := range testCases {
		t.Run(tc.region, func(t *testing.T) {
			result := GetRegion(tc.region)
			assert.Equal(t, tc.expected, result, "Region %s should map to %v", tc.region, tc.expected)
		})
	}
}

func TestRegionsMapConsistency(t *testing.T) {
	for regionStr, expectedRegion := range Regions {
		t.Run(regionStr, func(t *testing.T) {
			result := GetRegion(regionStr)
			assert.Equal(t, expectedRegion, result, "Regions map entry %s should be consistent with GetRegion", regionStr)
		})
	}
}

func TestRegionNormalizationInIPv6(t *testing.T) {
	tenantID := AgentuityTenantID
	machineID := "192.168.1.1"
	hostID := "172.17.0.1"

	testCases := []struct {
		name            string
		gcpRegion       string
		awsRegion       string
		azureRegion     string
		expectedRegion  Region
		shouldMatchAddr bool
	}{
		{
			name:            "US East 1 regions all map to same",
			gcpRegion:       "us-east1",
			awsRegion:       "us-east-1",
			azureRegion:     "eastus",
			expectedRegion:  RegionUSEast1,
			shouldMatchAddr: true,
		},
		{
			name:            "US West 1 regions all map to same",
			gcpRegion:       "us-west1",
			awsRegion:       "us-west-1",
			azureRegion:     "westus",
			expectedRegion:  RegionUSWest1,
			shouldMatchAddr: true,
		},
		{
			name:            "US Central 1 regions all map to same",
			gcpRegion:       "us-central1",
			awsRegion:       "",
			azureRegion:     "centralus",
			expectedRegion:  RegionUSCentral1,
			shouldMatchAddr: true,
		},
		{
			name:            "US East 2 regions all map to same",
			gcpRegion:       "us-east4",
			awsRegion:       "us-east-2",
			azureRegion:     "eastus2",
			expectedRegion:  RegionUSEast2,
			shouldMatchAddr: true,
		},
		{
			name:            "US West 2 regions all map to same",
			gcpRegion:       "us-west2",
			awsRegion:       "us-west-2",
			azureRegion:     "westus2",
			expectedRegion:  RegionUSWest2,
			shouldMatchAddr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gcpAddr := NewIPv6Address(GetRegion(tc.gcpRegion), NetworkPrivateGravity, tenantID, machineID, hostID)
			gcpRegionResult := GetRegion(tc.gcpRegion)
			assert.Equal(t, tc.expectedRegion, gcpRegionResult, "GCP region should normalize to %v", tc.expectedRegion)

			if tc.awsRegion != "" {
				awsAddr := NewIPv6Address(GetRegion(tc.awsRegion), NetworkPrivateGravity, tenantID, machineID, hostID)
				awsRegionResult := GetRegion(tc.awsRegion)
				assert.Equal(t, tc.expectedRegion, awsRegionResult, "AWS region should normalize to %v", tc.expectedRegion)

				if tc.shouldMatchAddr {
					assert.Equal(t, gcpAddr.String(), awsAddr.String(), "GCP and AWS equivalent regions should produce same IPv6")
					assert.Equal(t, gcpAddr.MachineSubnet(), awsAddr.MachineSubnet(), "GCP and AWS equivalent regions should produce same subnet")
				}
			}

			if tc.azureRegion != "" {
				azureAddr := NewIPv6Address(GetRegion(tc.azureRegion), NetworkPrivateGravity, tenantID, machineID, hostID)
				azureRegionResult := GetRegion(tc.azureRegion)
				assert.Equal(t, tc.expectedRegion, azureRegionResult, "Azure region should normalize to %v", tc.expectedRegion)

				if tc.shouldMatchAddr {
					assert.Equal(t, gcpAddr.String(), azureAddr.String(), "GCP and Azure equivalent regions should produce same IPv6")
					assert.Equal(t, gcpAddr.MachineSubnet(), azureAddr.MachineSubnet(), "GCP and Azure equivalent regions should produce same subnet")
				}
			}
		})
	}
}

func TestRegionCaseInsensitivity(t *testing.T) {
	testCases := []struct {
		lowerCase string
		upperCase string
		mixedCase string
		expected  Region
	}{
		{"us-central1", "US-CENTRAL1", "Us-CeNtRaL1", RegionUSCentral1},
		{"us-east1", "US-EAST1", "Us-EaSt1", RegionUSEast1},
		{"us-west1", "US-WEST1", "Us-WeSt1", RegionUSWest1},
		{"eastus", "EASTUS", "EaStUs", RegionUSEast1},
		{"westus", "WESTUS", "WESTus", RegionUSWest1},
		{"centralus", "CENTRALUS", "CentralUS", RegionUSCentral1},
	}

	for _, tc := range testCases {
		t.Run(tc.lowerCase, func(t *testing.T) {
			lowerResult := GetRegion(tc.lowerCase)
			upperResult := GetRegion(tc.upperCase)
			mixedResult := GetRegion(tc.mixedCase)

			assert.Equal(t, tc.expected, lowerResult)
			assert.Equal(t, tc.expected, upperResult)
			assert.Equal(t, tc.expected, mixedResult)
			assert.Equal(t, lowerResult, upperResult, "Case variations should produce same result")
			assert.Equal(t, lowerResult, mixedResult, "Case variations should produce same result")
		})
	}
}
