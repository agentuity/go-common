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

	// Network type is packed into byte 4's low 3 bits: [RRRRR NNN]
	networkType := addr[4] & 0x07
	if networkType != byte(NetworkSandboxSubnet) {
		t.Errorf("Subnet should have NetworkSandboxSubnet (0x05) in byte 4 low 3 bits: got 0x%02x", networkType)
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

		// Region is packed into byte 4's high 5 bits: [RRRRR NNN]
		regionInAddr := addr[4] >> 3
		if regionInAddr != byte(region) {
			t.Errorf("Region %s: expected byte 4 >> 3 = 0x%02x, got 0x%02x", regionName, region, regionInAddr)
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

func TestComputeSandboxSubnet_ULAPrefix(t *testing.T) {
	// Bytes 0-3 must always be fd15:d710 regardless of region or machine.
	for regionName, region := range Regions {
		subnet := ComputeSandboxSubnet(region, "machine_abc")
		addr := subnet.Addr().As16()
		if addr[0] != 0xfd || addr[1] != 0x15 || addr[2] != 0xd7 || addr[3] != 0x10 {
			t.Errorf("Region %s: ULA prefix should be fd15:d710, got %02x%02x:%02x%02x",
				regionName, addr[0], addr[1], addr[2], addr[3])
		}
	}
}

func TestComputeSandboxSubnet_Byte4PackingRoundtrip(t *testing.T) {
	// Verify region and network can be independently extracted from byte 4
	// for every defined region. Byte 4 layout: [RRRRR NNN]
	allRegions := []struct {
		name   string
		region Region
	}{
		{"Global", RegionGlobal},
		{"USCentral1", RegionUSCentral1},
		{"USWest1", RegionUSWest1},
		{"USEast1", RegionUSEast1},
		{"USCentral2", RegionUSCentral2},
		{"USCentral3", RegionUSCentral3},
		{"USWest2", RegionUSWest2},
		{"USWest3", RegionUSWest3},
		{"USEast2", RegionUSEast2},
		{"USEast3", RegionUSEast3},
		{"USCentral4", RegionUSCentral4},
		{"USWest4", RegionUSWest4},
		{"USEast4", RegionUSEast4},
		{"EUEast1", RegionEUEast1},
		{"EUEast2", RegionEUEast2},
		{"EUEast3", RegionEUEast3},
		{"EUWest1", RegionEUWest1},
		{"EUWest2", RegionEUWest2},
		{"EUWest3", RegionEUWest3},
	}
	for _, tc := range allRegions {
		t.Run(tc.name, func(t *testing.T) {
			subnet := ComputeSandboxSubnet(tc.region, "machine_roundtrip")
			addr := subnet.Addr().As16()

			gotRegion := addr[4] >> 3
			gotNetwork := addr[4] & 0x07

			if gotRegion != byte(tc.region) {
				t.Errorf("region: expected 0x%02x, got 0x%02x (byte4=0x%02x)", tc.region, gotRegion, addr[4])
			}
			if gotNetwork != byte(NetworkSandboxSubnet) {
				t.Errorf("network: expected 0x%02x, got 0x%02x (byte4=0x%02x)", NetworkSandboxSubnet, gotNetwork, addr[4])
			}
		})
	}
}

func TestComputeSandboxSubnet_MaxRegionFits5Bits(t *testing.T) {
	// Guard against adding a region > 31 which would overflow 5 bits.
	maxRegion := RegionEUWest3 // currently 0x12 = 18
	if byte(maxRegion) > 31 {
		t.Fatalf("max region 0x%02x exceeds 5-bit limit (31)", maxRegion)
	}
	// Verify it round-trips correctly.
	subnet := ComputeSandboxSubnet(maxRegion, "machine_max")
	addr := subnet.Addr().As16()
	got := addr[4] >> 3
	if got != byte(maxRegion) {
		t.Errorf("max region round-trip: expected 0x%02x, got 0x%02x", maxRegion, got)
	}
}

func TestComputeSandboxSubnet_MachineHashUses3Bytes(t *testing.T) {
	// Verify byte 5 (the new high byte of the 24-bit hash) actually varies
	// across different machines, not just bytes 6-7.
	seen5 := make(map[byte]bool)
	for i := 0; i < 500; i++ {
		machineID := "machine_hash_spread_" + string(rune('A'+i%26)) + string(rune('a'+i/26%26)) + string(rune('0'+i/676))
		subnet := ComputeSandboxSubnet(RegionUSWest1, machineID)
		addr := subnet.Addr().As16()
		seen5[addr[5]] = true
	}
	// With 500 machines and 256 possible byte values, we should see good spread.
	// Require at least 50 distinct byte 5 values (very conservative).
	if len(seen5) < 50 {
		t.Errorf("byte 5 should vary across machines: only saw %d distinct values out of 500 machines", len(seen5))
	}
}

func TestComputeSandboxSubnet_HostBytesZero(t *testing.T) {
	// The /64 prefix must have bytes 8-15 zeroed (host part).
	subnet := ComputeSandboxSubnet(RegionUSEast1, "machine_hostcheck")
	addr := subnet.Addr().As16()
	for i := 8; i < 16; i++ {
		if addr[i] != 0 {
			t.Errorf("byte %d should be 0 in /64 prefix, got 0x%02x", i, addr[i])
		}
	}
}

func TestComputeSandboxVIP_WithinSubnet_AllRegions(t *testing.T) {
	// Verify VIPs land inside the subnet for every region, not just USWest1.
	for regionName, region := range Regions {
		if region == RegionGlobal {
			continue
		}
		subnet := ComputeSandboxSubnet(region, "machine_region_vip")
		vip := ComputeSandboxVIP(subnet, "sandbox_region_vip")
		if !subnet.Contains(vip) {
			t.Errorf("Region %s: VIP %s not within subnet %s", regionName, vip, subnet)
		}
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

func TestComputeDeploymentSubnet_Consistency(t *testing.T) {
	region := RegionUSWest1
	machineID := "machine_xyz789"

	subnet1 := ComputeDeploymentSubnet(region, machineID)
	subnet2 := ComputeDeploymentSubnet(region, machineID)

	if subnet1 != subnet2 {
		t.Errorf("ComputeDeploymentSubnet should produce consistent results:\n  got1: %s\n  got2: %s", subnet1, subnet2)
	}
}

func TestComputeDeploymentSubnet_DifferentMachines(t *testing.T) {
	region := RegionUSWest1

	subnet1 := ComputeDeploymentSubnet(region, "machine_001")
	subnet2 := ComputeDeploymentSubnet(region, "machine_002")

	if subnet1 == subnet2 {
		t.Errorf("ComputeDeploymentSubnet should produce different subnets for different machines:\n  got: %s", subnet1)
	}
}

func TestComputeDeploymentSubnet_ValidPrefix(t *testing.T) {
	region := RegionUSWest1
	machineID := "machine_xyz789"

	subnet := ComputeDeploymentSubnet(region, machineID)

	if !subnet.IsValid() {
		t.Errorf("ComputeDeploymentSubnet should produce valid prefix: got %s", subnet)
	}

	if subnet.Bits() != 96 {
		t.Errorf("ComputeDeploymentSubnet should produce /96 prefix: got /%d", subnet.Bits())
	}
}

func TestComputeDeploymentSubnet_NetworkType(t *testing.T) {
	region := RegionUSWest1
	machineID := "machine_xyz789"

	subnet := ComputeDeploymentSubnet(region, machineID)
	addr := subnet.Addr().As16()

	networkType := addr[4] & 0x07
	if networkType != byte(NetworkDeploymentSubnet) {
		t.Errorf("Subnet should have NetworkDeploymentSubnet (0x06) in byte 4 low 3 bits: got 0x%02x", networkType)
	}
}

func TestComputeDeploymentVIP_WithinSubnet(t *testing.T) {
	region := RegionUSWest1
	machineID := "machine_xyz789"
	deploymentID := "deployment_001"

	subnet := ComputeDeploymentSubnet(region, machineID)
	vip := ComputeDeploymentVIP(subnet, deploymentID)

	if !subnet.Contains(vip) {
		t.Errorf("ComputeDeploymentVIP should produce address within subnet:\n  subnet: %s\n  vip: %s", subnet, vip)
	}
}

func TestComputeDeploymentVIP_PanicsOnNon96Prefix(t *testing.T) {
	b := make([]byte, 16)
	b[0] = 0xfd
	b[1] = 0x15
	addr, _ := netip.AddrFromSlice(b)
	prefix80 := netip.PrefixFrom(addr, 80)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("ComputeDeploymentVIP should panic on non-/96 prefix")
		}
	}()

	ComputeDeploymentVIP(prefix80, "deployment_001")
}

func TestComputeDeploymentVIP_DifferentDeployments(t *testing.T) {
	region := RegionUSWest1
	machineID := "machine_xyz789"

	subnet := ComputeDeploymentSubnet(region, machineID)

	vip1 := ComputeDeploymentVIP(subnet, "deployment_001")
	vip2 := ComputeDeploymentVIP(subnet, "deployment_002")

	if vip1 == vip2 {
		t.Errorf("ComputeDeploymentVIP should produce different addresses for different deployments:\n  got: %s", vip1)
	}
}

func TestComputeDeploymentVIP_Consistency(t *testing.T) {
	region := RegionUSWest1
	machineID := "machine_xyz789"
	deploymentID := "deployment_001"

	subnet := ComputeDeploymentSubnet(region, machineID)

	vip1 := ComputeDeploymentVIP(subnet, deploymentID)
	vip2 := ComputeDeploymentVIP(subnet, deploymentID)

	if vip1 != vip2 {
		t.Errorf("ComputeDeploymentVIP should produce consistent results:\n  got1: %s\n  got2: %s", vip1, vip2)
	}
}
