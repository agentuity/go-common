package network

import (
	"fmt"
	"net/netip"
)

// NetworkSandboxSubnet is the network type for sandbox subnets.
// This is separate from NetworkHadron (0x03) to avoid address collisions.
const NetworkSandboxSubnet Network = 0x05

// ComputeSandboxSubnet returns a deterministic /64 IPv6 subnet for a machine.
// Both gravity and hadron must call this function with the same parameters
// (region, machineID) to produce identical results.
//
// The machineID is already derived from orgID + instanceID (see auth.DeterministicMachineID),
// so the org is implicitly included in the subnet computation.
//
// Address format: fd15:d710:RNMM:MMMM::/64
//   - Byte 4: Region (5 bits, max 31) | Network (3 bits, max 7)
//   - Bytes 5-7: Machine hash (24 bits, 16M buckets)
//
// With 24-bit machine hash (16M buckets per region), birthday-paradox
// collision probability is per-region (not fleet-wide), since the region
// is encoded separately in byte 4:
//   - 100 machines/region: ~0.03%
//   - 1000 machines/region: ~2.9%
//   - 5000 machines/region: ~52%
func ComputeSandboxSubnet(region Region, machineID string) netip.Prefix {
	machineHash := hashTo32Bits(machineID)

	b := make([]byte, 16)
	b[0] = 0xfd
	b[1] = 0x15
	b[2] = 0xd7
	b[3] = 0x10
	b[4] = (byte(region) << 3) | (byte(NetworkSandboxSubnet) & 0x07)
	b[5] = byte((machineHash >> 16) & 0xff)
	b[6] = byte((machineHash >> 8) & 0xff)
	b[7] = byte(machineHash & 0xff)
	// Bytes 8-15 are zero for the prefix

	addr, _ := netip.AddrFromSlice(b)
	return netip.PrefixFrom(addr, 64)
}

// ComputeSandboxVIP returns a deterministic IPv6 address for a sandbox
// within its machine's subnet. The subnet must be a /64 prefix; the host
// bits (bytes 8-15) are derived from the sandboxID hash.
//
// Note: bytes 12-14 reuse overlapping bit ranges from the same 32-bit
// FNV-1a hash, so the effective entropy is ~32 bits spread across 7 bytes
// (byte 15 is forced odd). This is sufficient for current scale — 0
// collisions at 10k sandboxes in testing. For very large sandbox counts
// (100k+), consider switching to a 64-bit hash function.
func ComputeSandboxVIP(subnet netip.Prefix, sandboxID string) netip.Addr {
	if subnet.Bits() != 64 {
		panic(fmt.Sprintf("ComputeSandboxVIP requires a /64 prefix, got /%d", subnet.Bits()))
	}
	h := hashTo32Bits(sandboxID)
	base := subnet.Addr().As16()

	base[8] = byte((h >> 24) & 0xff)
	base[9] = byte((h >> 16) & 0xff)
	base[10] = byte((h >> 8) & 0xff)
	base[11] = byte(h & 0xff)
	base[12] = byte((h >> 12) & 0xff)
	base[13] = byte((h >> 4) & 0xff)
	base[14] = byte((h >> 20) & 0xff)
	base[15] = byte(h&0xff) | 1

	addr, _ := netip.AddrFromSlice(base[:])
	return addr
}

// hashTo32Bits returns a 32-bit FNV-1a hash of the input string.
func hashTo32Bits(val string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(val); i++ {
		h ^= uint32(val[i])
		h *= 16777619
	}
	return h
}
