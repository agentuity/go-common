// Package vip provides deterministic virtual IP generation for Gravity.
//
// Instead of using a per-machine atomic counter (which requires coordination),
// this package hashes a connection's 5-tuple plus container ID to produce a
// deterministic IPv6 address within a /96 subnet. Any Gravity instance given
// the same inputs will compute the same VIP, enabling zero-coordination
// failover.
package vip

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net"
)

// GenerateDeterministicIPV6 produces a deterministic IPv6 address within the
// given /96 subnet by hashing the provided connection parameters.
//
// The function hashes containerID, srcIP, srcPort, dstIP, and dstPort using
// SHA-256, extracts a 32-bit host identifier from the hash, and embeds it
// into the last 32 bits of the subnet address.
//
// Parameters:
//   - containerID: unique identifier for the container
//   - srcIP: source IP address of the connection
//   - srcPort: source port of the connection
//   - dstIP: destination IP address of the connection
//   - dstPort: destination port of the connection
//   - subnet: a /96 IPv6 subnet to place the address in
//
// Returns the generated IPv6 address, or an error if the subnet is not /96.
func GenerateDeterministicIPV6(containerID string, srcIP net.IP, srcPort uint16, dstIP net.IP, dstPort uint16, subnet net.IPNet) (net.IP, error) {
	ones, bits := subnet.Mask.Size()
	if ones != 96 || bits != 128 {
		return nil, fmt.Errorf("subnet must be a /96 IPv6 network, got /%d (bits=%d)", ones, bits)
	}

	// Hash the 5-tuple plus container ID.
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d|%s|%d", containerID, srcIP, srcPort, dstIP, dstPort)))

	// Extract the first 4 bytes as a uint32 host identifier.
	hostID := binary.BigEndian.Uint32(hash[:4])

	// Skip zero to avoid the subnet's network address.
	if hostID == 0 {
		hostID = 1
	}

	// Embed the host ID into the last 32 bits of the /96 subnet.
	ip := make(net.IP, 16)
	copy(ip, subnet.IP.To16())
	binary.BigEndian.PutUint32(ip[12:], hostID)

	return ip, nil
}
