package network

import (
	"fmt"
	"math/rand"
	"net"
)

var privateRanges = []struct {
	network *net.IPNet
}{
	{mustParseCIDR("192.168.0.0/16")},
	{mustParseCIDR("172.16.0.0/12")},
	{mustParseCIDR("10.0.0.0/8")},
}

func mustParseCIDR(cidr string) *net.IPNet {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	return network
}

// Check if two subnets overlap
func overlaps(a, b *net.IPNet) bool {
	return a.Contains(b.IP) || b.Contains(a.IP)
}

// Generate a random subnet with the given prefix size within the given range
func generateRandomSubnet(parent *net.IPNet, prefixLen int) *net.IPNet {
	parentIP := parent.IP.Mask(parent.Mask)
	parentOnes, _ := parent.Mask.Size()

	hostBits := 32 - prefixLen
	if hostBits < 0 {
		return nil
	}

	// Convert parent IP to 32-bit big-endian integer
	parentInt := uint32(parentIP[0])<<24 | uint32(parentIP[1])<<16 | uint32(parentIP[2])<<8 | uint32(parentIP[3])

	// Mask to parent prefix to get the base network
	parentMask := ^uint32(0) << (32 - parentOnes)
	baseNetwork := parentInt & parentMask

	// Generate random offset within the available host bits for the new prefix
	maxOffset := uint32(1) << hostBits
	offset := uint32(rand.Intn(int(maxOffset)))

	// Add offset to base network
	newIP := baseNetwork + offset

	// Convert back to 4 bytes
	randBytes := []byte{
		byte(newIP >> 24),
		byte(newIP >> 16),
		byte(newIP >> 8),
		byte(newIP),
	}

	newSubnet := fmt.Sprintf("%s/%d", net.IP(randBytes).String(), prefixLen)
	_, result, _ := net.ParseCIDR(newSubnet)
	return result
}

// GenerateNonOverlappingIPv4Subnet generates a non-overlapping ipv4 subnet with the given prefix size within the given range.
func GenerateNonOverlappingIPv4Subnet(existingNetworks []*net.IPNet, prefixLen int) (*net.IPNet, *net.IP, error) {
	// Validate prefix length is within acceptable range
	if prefixLen < 8 || prefixLen > 30 {
		return nil, nil, fmt.Errorf("invalid prefix length %d: must be between 8 and 30", prefixLen)
	}

	for _, rng := range privateRanges {
		for range 1000 { // Try 1000 times to find non-overlapping subnet
			candidate := generateRandomSubnet(rng.network, prefixLen)
			if candidate != nil {
				hasOverlap := false
				for _, existing := range existingNetworks {
					if overlaps(existing, candidate) {
						hasOverlap = true
						break
					}
				}
				if !hasOverlap {
					network := candidate.IP.To4()
					ip := make(net.IP, 4)
					copy(ip, network)
					ip[3] = 0x1 // make the gateway the first ip and copy since we have a shared copy we need to change
					return candidate, &ip, nil
				}
			}
		}
	}
	return nil, nil, fmt.Errorf("unable to generate non-overlapping subnet after many attempts")
}
