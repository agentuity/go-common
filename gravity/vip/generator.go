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
// The function hashes machineID, srcIP, srcPort, dstIP, and dstPort using
// SHA-256, extracts a 32-bit host identifier from the hash, and embeds it
// into the last 32 bits of the subnet address.
//
// Parameters:
//   - machineID: unique identifier for the Hadron machine (NOT the container/deployment ID).
//     Using the machine ID ensures that the same deployment scaled across multiple
//     Hadrons produces different unique IPs for the same flow, preventing NAT collisions.
//   - srcIP: source IP address of the connection
//   - srcPort: source port of the connection
//   - dstIP: destination IP address of the connection
//   - dstPort: destination port of the connection
//   - subnet: a /96 IPv6 subnet to place the address in
//
// Returns the generated IPv6 address, or an error if the subnet is not /96.
func GenerateDeterministicIPV6(machineID string, srcIP net.IP, srcPort uint16, dstIP net.IP, dstPort uint16, subnet net.IPNet) (net.IP, error) {
	ones, bits := subnet.Mask.Size()
	if ones != 96 || bits != 128 {
		return nil, fmt.Errorf("subnet must be a /96 IPv6 network, got /%d (bits=%d)", ones, bits)
	}

	// Hash the 5-tuple plus machine ID.
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d|%s|%d", machineID, srcIP, srcPort, dstIP, dstPort)))

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

// GenerateDeterministicIPV6WithCollisionCheck generates a unique IP and verifies
// it doesn't collide with an existing allocation via the isCollision callback.
// isCollision receives each candidate IP and should return true if that IP is
// already in use by a different flow. On collision the function rehashes with
// an incrementing salt, up to 8 attempts, before returning an error.
func GenerateDeterministicIPV6WithCollisionCheck(
	machineID string,
	srcIP net.IP, srcPort uint16,
	dstIP net.IP, dstPort uint16,
	subnet net.IPNet,
	isCollision func(ip net.IP) bool,
) (net.IP, error) {
	ones, bits := subnet.Mask.Size()
	if ones != 96 || bits != 128 {
		return nil, fmt.Errorf("subnet must be a /96 IPv6 network, got /%d (bits=%d)", ones, bits)
	}

	const maxAttempts = 8
	for salt := 0; salt < maxAttempts; salt++ {
		input := fmt.Sprintf("%s|%s|%d|%s|%d", machineID, srcIP, srcPort, dstIP, dstPort)
		if salt > 0 {
			input = fmt.Sprintf("%s|%d", input, salt)
		}
		hash := sha256.Sum256([]byte(input))
		hostID := binary.BigEndian.Uint32(hash[:4])
		if hostID == 0 {
			hostID = 1
		}

		ip := make(net.IP, 16)
		copy(ip, subnet.IP.To16())
		binary.BigEndian.PutUint32(ip[12:], hostID)

		if isCollision == nil || !isCollision(ip) {
			return ip, nil
		}
	}

	return nil, fmt.Errorf("VIP collision after %d rehash attempts", maxAttempts)
}
