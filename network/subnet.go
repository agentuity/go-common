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
// Address format: fd15:d710:RR:05:MMMM::/64
//   - RR = Region (8 bits, byte 4)
//   - 05 = NetworkSandboxSubnet in high nibble (4 bits) + 0 in low nibble (byte 5)
//   - MMMM = 16 bits of machine hash (bytes 6-7)
//
// Note: We use 16 bits for machine hash within the /64 prefix. This gives
// ~0.03% collision rate at 100 machines, which is acceptable.
func ComputeSandboxSubnet(region Region, machineID string) netip.Prefix {
	machineHash := hashTo32Bits(machineID)

	b := make([]byte, 16)
	b[0] = 0xfd
	b[1] = 0x15
	b[2] = 0xd7
	b[3] = 0x10
	b[4] = byte(region)
	b[5] = byte(NetworkSandboxSubnet) << 4
	b[6] = byte((machineHash >> 8) & 0xff)
	b[7] = byte(machineHash & 0xff)
	// Bytes 8-15 are zero for the prefix

	addr, _ := netip.AddrFromSlice(b)
	return netip.PrefixFrom(addr, 64)
}

// ComputeSandboxVIP returns a deterministic IPv6 address for a sandbox
// within its machine's subnet. The subnet must be a /64 prefix; the host
// bits (bytes 8-15) are derived from the sandboxID hash.
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
