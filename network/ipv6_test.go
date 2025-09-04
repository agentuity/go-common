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
