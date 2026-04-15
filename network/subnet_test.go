package network

import (
	"net/netip"
	"testing"
)

func TestComputeSandboxSubnet_Consistency(t *testing.T) {
	region := RegionUSWest1
	machineID := "machine_xyz789"

	subnet1 := ComputeSandboxSubnet(region, machineID)
	subnet2 := ComputeSandboxSubnet(region, machineID)

	if subnet1 != subnet2 {
		t.Errorf("ComputeSandboxSubnet should produce consistent results:\n  got1: %s\n  got2: %s", subnet1, subnet2)
	}
}

func TestComputeSandboxSubnet_DifferentMachines(t *testing.T) {
	region := RegionUSWest1

	subnet1 := ComputeSandboxSubnet(region, "machine_001")
	subnet2 := ComputeSandboxSubnet(region, "machine_002")

	if subnet1 == subnet2 {
		t.Errorf("ComputeSandboxSubnet should produce different subnets for different machines:\n  got: %s", subnet1)
	}
}

func TestComputeSandboxSubnet_DifferentRegions(t *testing.T) {
	machineID := "machine_xyz789"

	subnet1 := ComputeSandboxSubnet(RegionUSWest1, machineID)
	subnet2 := ComputeSandboxSubnet(RegionUSEast1, machineID)

	if subnet1 == subnet2 {
		t.Errorf("ComputeSandboxSubnet should produce different subnets for different regions:\n  got: %s", subnet1)
	}
}

func TestComputeSandboxSubnet_ValidPrefix(t *testing.T) {
	region := RegionUSWest1
	machineID := "machine_xyz789"

	subnet := ComputeSandboxSubnet(region, machineID)

	if !subnet.IsValid() {
		t.Errorf("ComputeSandboxSubnet should produce valid prefix: got %s", subnet)
	}

	if subnet.Bits() != 64 {
		t.Errorf("ComputeSandboxSubnet should produce /64 prefix: got /%d", subnet.Bits())
	}
}

func TestComputeSandboxSubnet_NetworkType(t *testing.T) {
	region := RegionUSWest1
	machineID := "machine_xyz789"

	subnet := ComputeSandboxSubnet(region, machineID)
	addr := subnet.Addr().As16()

	networkType := (addr[5] >> 4) & 0x0f
	if networkType != byte(NetworkSandboxSubnet) {
		t.Errorf("Subnet should have NetworkSandboxSubnet (0x05) in byte 5 high nibble: got 0x%02x", networkType)
	}
}

func TestComputeSandboxSubnet_RegionInAddress(t *testing.T) {
	machineID := "machine_xyz789"

	for regionName, region := range Regions {
		if region == RegionGlobal {
			continue
		}
		subnet := ComputeSandboxSubnet(region, machineID)
		addr := subnet.Addr().As16()

		regionInAddr := addr[4]
		if regionInAddr != byte(region) {
			t.Errorf("Region %s: expected byte 4 = 0x%02x, got 0x%02x", regionName, region, regionInAddr)
		}
	}
}

func TestComputeSandboxVIP_WithinSubnet(t *testing.T) {
	region := RegionUSWest1
	machineID := "machine_xyz789"
	sandboxID := "sandbox_001"

	subnet := ComputeSandboxSubnet(region, machineID)
	vip := ComputeSandboxVIP(subnet, sandboxID)

	if !subnet.Contains(vip) {
		t.Errorf("ComputeSandboxVIP should produce address within subnet:\n  subnet: %s\n  vip: %s", subnet, vip)
	}
}

func TestComputeSandboxVIP_PanicsOnNon64Prefix(t *testing.T) {
	// Build a /80 prefix — ComputeSandboxVIP must reject it because
	// the host-bit layout is hardcoded for /64.
	b := make([]byte, 16)
	b[0] = 0xfd
	b[1] = 0x15
	addr, _ := netip.AddrFromSlice(b)
	prefix80 := netip.PrefixFrom(addr, 80)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("ComputeSandboxVIP should panic on non-/64 prefix")
		}
	}()

	ComputeSandboxVIP(prefix80, "sandbox_001")
}

func TestComputeSandboxVIP_DifferentSandboxes(t *testing.T) {
	region := RegionUSWest1
	machineID := "machine_xyz789"

	subnet := ComputeSandboxSubnet(region, machineID)

	vip1 := ComputeSandboxVIP(subnet, "sandbox_001")
	vip2 := ComputeSandboxVIP(subnet, "sandbox_002")

	if vip1 == vip2 {
		t.Errorf("ComputeSandboxVIP should produce different addresses for different sandboxes:\n  got: %s", vip1)
	}
}

func TestComputeSandboxVIP_Consistency(t *testing.T) {
	region := RegionUSWest1
	machineID := "machine_xyz789"
	sandboxID := "sandbox_001"

	subnet := ComputeSandboxSubnet(region, machineID)

	vip1 := ComputeSandboxVIP(subnet, sandboxID)
	vip2 := ComputeSandboxVIP(subnet, sandboxID)

	if vip1 != vip2 {
		t.Errorf("ComputeSandboxVIP should produce consistent results:\n  got1: %s\n  got2: %s", vip1, vip2)
	}
}

func TestComputeSandboxVIP_ValidAddress(t *testing.T) {
	region := RegionUSWest1
	machineID := "machine_xyz789"
	sandboxID := "sandbox_001"

	subnet := ComputeSandboxSubnet(region, machineID)
	vip := ComputeSandboxVIP(subnet, sandboxID)

	if !vip.IsValid() {
		t.Errorf("ComputeSandboxVIP should produce valid address: got %s", vip)
	}

	if !vip.Is6() {
		t.Errorf("ComputeSandboxVIP should produce IPv6 address: got %s", vip)
	}
}

func TestComputeSandboxVIP_NonZeroLastByte(t *testing.T) {
	region := RegionUSWest1
	machineID := "machine_xyz789"
	sandboxID := "sandbox_001"

	subnet := ComputeSandboxSubnet(region, machineID)
	vip := ComputeSandboxVIP(subnet, sandboxID)
	addr := vip.As16()

	if addr[15] == 0 {
		t.Errorf("ComputeSandboxVIP should ensure non-zero last byte: got %s", vip)
	}
}

func TestHashTo32Bits_Consistency(t *testing.T) {
	val := "test_string_123"

	h1 := hashTo32Bits(val)
	h2 := hashTo32Bits(val)

	if h1 != h2 {
		t.Errorf("hashTo32Bits should produce consistent results: got %d and %d", h1, h2)
	}
}

func TestHashTo32Bits_DifferentInputs(t *testing.T) {
	h1 := hashTo32Bits("input1")
	h2 := hashTo32Bits("input2")

	if h1 == h2 {
		t.Errorf("hashTo32Bits should produce different results for different inputs: both got %d", h1)
	}
}

func TestComputeSandboxSubnet_CollisionResistance(t *testing.T) {
	region := RegionUSWest1

	seen := make(map[netip.Prefix]string)
	collisions := 0

	for i := 0; i < 1000; i++ {
		machineID := "machine_" + string(rune('A'+i%26)) + string(rune('0'+i/26))
		subnet := ComputeSandboxSubnet(region, machineID)

		if existing, ok := seen[subnet]; ok {
			t.Logf("Collision: subnet %s for machine %s and %s", subnet, existing, machineID)
			collisions++
		}
		seen[subnet] = machineID
	}

	collisionRate := float64(collisions) / 1000.0
	t.Logf("Collision rate at 1000 machines: %.4f%% (%d collisions)", collisionRate*100, collisions)

	if collisionRate > 0.01 {
		t.Errorf("Collision rate should be < 1%% at 1000 machines: got %.4f%%", collisionRate*100)
	}
}

func TestComputeSandboxVIP_CollisionResistance(t *testing.T) {
	region := RegionUSWest1
	machineID := "machine_xyz789"

	subnet := ComputeSandboxSubnet(region, machineID)
	seen := make(map[netip.Addr]string)
	collisions := 0

	for i := 0; i < 10000; i++ {
		sandboxID := "sandbox_" + string(rune('A'+i%26)) + string(rune('0'+i/26))
		vip := ComputeSandboxVIP(subnet, sandboxID)

		if existing, ok := seen[vip]; ok {
			t.Logf("Collision: VIP %s for sandbox %s and %s", vip, existing, sandboxID)
			collisions++
		}
		seen[vip] = sandboxID
	}

	collisionRate := float64(collisions) / 10000.0
	t.Logf("VIP collision rate at 10000 sandboxes: %.6f%% (%d collisions)", collisionRate*100, collisions)

	if collisionRate > 0.001 {
		t.Errorf("VIP collision rate should be < 0.1%% at 10000 sandboxes: got %.6f%%", collisionRate*100)
	}
}
