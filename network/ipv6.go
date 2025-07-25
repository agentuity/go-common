package network

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net"
)

func hashTo49Bits(tenantID string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(tenantID))
	return h.Sum64() & 0x1FFFFFFFFFFFF // Mask to 49 bits
}

func hashTo16Bits(ipv4 string) uint16 {
	h := fnv.New32a()
	h.Write([]byte(ipv4))
	return uint16(h.Sum32() & 0xFFFF) // Mask to 16 bits
}

func ipv4ToHex(ipv4 string) string {
	ip := net.ParseIP(ipv4).To4()
	if ip == nil {
		return ":" // Invalid IPv4 fallback
	}
	return fmt.Sprintf("%02x%02x:%02x%02x", ip[0], ip[1], ip[2], ip[3])
}

type Region uint8

const (
	RegionGlobal     Region = 0x00
	RegionUSCentral1 Region = 0x01
	RegionUSWest1    Region = 0x02
	RegionUSEast1    Region = 0x03
)

type Network uint8

const (
	NetworkPrivateGravity   Network = 0x00
	NetworkExternalCustomer Network = 0x01
)

const AgentuityTenantID = "agentuity"

// this is the agentuity IPV6 ULA prefix
const agentuityIPV6ULAPrefix = "fd15:d710"

/*
 * IPv6 Address Format:
 *
 * Base: fd15:d710::/28 (28 bits).
 * - Compute the SHA-256 hash of "agentuity" => 15d71ecdfd86fe859f20f40a94a3a05511d0eb068089883837434e241b9cfa1a.
 * - For a ULA prefix, take the first 40 bits (10 hex digits) of the hash: 15d71ecdfd.
 * - the derived /28 ULA base prefix is fd15:d710::/28 (Non-routable to the internet)
 *
 * Subnet field (to /96):
 * - Region: 8 bits (256 values).
 * - Network: 4 bits (16 values).
 * - Tenant: 49 bits (hashed from 32-char tenant ID).
 * - Machine: 16 bits (hashed IPv4).
 * - Total: 8 + 4 + 49 + 16 = 77 bits (28 + 77 = 105 bits, but /96 aligns with hex groups).
 * - Subnet Format: fd15:d710:RRNT:TTTX:MMMM:TTTT::/96
 *   - RR = region ID (e.g., 03).
 *   - N = network ID (e.g., 0).
 *   - TTT = high 12 bits of 49-bit tenant hash.
 *   - X = next 4 bits of tenant hash.
 *   - MMMM = machine ID.
 *   - TTTT = next 16 bits of tenant hash.
 * Host ID (128 - 96 = 32 bits): Directly embeds container IPv4 or other hostid (e.g., ac11:0002 for 172.17.0.2).
 *
 * Calculation of cardinality for each field:
 *
 * Region: 8 bits
 *   - Cardinality: 2⁸ = 256 regions (00-ff in hex).
 *   - Example: 03 for us-west2.
 * Network: 4 bits
 *   - Cardinality: 2⁴ = 16 networks per region (0-f in hex).
 *   - Example: 0 for gravity.
 * Tenant: 49 bits
 *   - Cardinality: 2⁴⁹ ≈ 562,949,953,421,312 (~562 trillion) tenant IDs.
 *   - Derived from hashing 32-character tenant ID (e.g., f109669b95881dfaa9d28f02df411d7a).
 * Machine: 16 bits
 *   - Cardinality: 2¹⁶ = 65,536 machines per tenant/system/region (hashed from machine IPv4).
 *   - Example: 192.168.1.1 hashed to a1b2.
 * Host (Host ID): 32 bits
 *   - Cardinality: 2³² ≈ 4,294,967,296 hosts per machine (direct embedding of host/vm IPv4 address).
 *   - Example: 172.17.0.2 → ac11:0002.
 * Total Subnets:
 *   - /96 subnets: 256 × 16 × 562T × 65,536 ≈ 3,794,496,790,978,772,992 (~3.8 quintillion).
 *   - Each /96 subnet supports ~4.3 billion unique host IPv4 addresses.
 *
 * Subnet:
 *
 * - Mask for a unique machine in a region: fd15:d710:RRST:TTTX:MMMM::/96
 *
 */
func buildIPv6Address(region Region, network Network, tenantID string, machineID string, hostID string) string {
	tenantHash := hashTo49Bits(tenantID)
	ttt := (tenantHash >> 37) & 0xFFF // High 12 bits
	x := (tenantHash >> 33) & 0xF     // Next 4 bits
	tttt := (tenantHash >> 17) & 0xFFFF
	rrst := uint16(region)<<8 | uint16(network)<<4 | uint16(x)
	machineHash := hashTo16Bits(machineID)
	containerHex := ipv4ToHex(hostID)
	// reparse the validate and make sure we have a valid IP and formatted nicely
	val := net.ParseIP(fmt.Sprintf("%s:%s:%03x0:%s:%s:%s", agentuityIPV6ULAPrefix, padHex(uint64(rrst)), ttt, padHex(uint64(machineHash)), padHex(uint64(tttt)), containerHex))
	return val.String()
}

func padHex(hex uint64) string {
	val := fmt.Sprintf("%x", hex)
	return val
}

func buildIPv6MachineSubnet(region Region, network Network, tenantID string, machineID string) string {
	tenantHash := hashTo49Bits(tenantID)
	ttt := (tenantHash >> 37) & 0xFFF // High 12 bits
	x := (tenantHash >> 33) & 0xF
	tttt := (tenantHash >> 17) & 0xFFFF
	rrst := uint16(region)<<8 | uint16(network)<<4 | uint16(x)
	machineHash := hashTo16Bits(machineID)
	// reparse the validate and make sure we have a valid IP and formatted nicely
	val := fmt.Sprintf("%s:%s:%03x0:%s:%x::/96", agentuityIPV6ULAPrefix, padHex(uint64(rrst)), ttt, padHex(uint64(machineHash)), tttt)
	_, subnet, _ := net.ParseCIDR(val)
	return subnet.String()
}

type IPv6Address struct {
	// Region is the region of the IPv6 address.
	Region Region
	// Network is the network of the IPv6 address.
	Network Network
	// TenantID is the tenant ID of the IPv6 address.
	TenantID string
	// MachineID is the machine ID of the IPv6 address.
	MachineID string
	// HostID is the host ID of the IPv6 address.
	HostID string
	// ipv6Address is the pre-calculated IPv6 address of the IPv6 address.
	ipv6Address string
}

// NewIPv6Address creates a new IPv6Address struct with the given parameters.
// It calculates the IPv6 address based on the provided parameters and stores them in the struct.
// The struct is returned as a pointer to the IPv6Address type.
func NewIPv6Address(region Region, network Network, tenantID string, machineID string, hostID string) *IPv6Address {
	return &IPv6Address{
		Region:      region,
		Network:     network,
		TenantID:    tenantID,
		MachineID:   machineID,
		HostID:      hostID,
		ipv6Address: buildIPv6Address(region, network, tenantID, machineID, hostID),
	}
}

func (a *IPv6Address) String() string {
	return a.ipv6Address
}

func (a *IPv6Address) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.ipv6Address)
}

// MachineSubnet returns the subnet for the machine with a /96 mask.
func (a *IPv6Address) MachineSubnet() string {
	return buildIPv6MachineSubnet(a.Region, a.Network, a.TenantID, a.MachineID)
}
