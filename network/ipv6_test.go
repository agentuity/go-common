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
