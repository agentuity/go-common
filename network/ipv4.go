package network

import (
	"fmt"
	"math/rand"
	"net"
	"sync"
)

// rng is the random number generator used for subnet selection.
// Can be overridden in tests for deterministic behavior.
var rng = rand.New(rand.NewSource(rand.Int63()))

// rngMu protects concurrent access to rng
var rngMu sync.Mutex

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

	// Check if the requested prefix length is valid
	if prefixLen <= parentOnes {
		return nil
	}

	// Calculate how many subnets of the requested size can fit in the parent
	subnetBits := prefixLen - parentOnes
	maxSubnets := uint32(1) << subnetBits

	// Generate random subnet index
	rngMu.Lock()
	subnetIndex := uint32(rng.Intn(int(maxSubnets)))
	rngMu.Unlock()

	// Convert parent IP to 32-bit big-endian integer
	parentInt := uint32(parentIP[0])<<24 | uint32(parentIP[1])<<16 | uint32(parentIP[2])<<8 | uint32(parentIP[3])

	// Calculate the size of each subnet in terms of IP addresses
	subnetSize := uint32(1) << (32 - prefixLen)

	// Calculate the starting IP of the chosen subnet
	newIP := parentInt + (subnetIndex * subnetSize)

	// Convert back to 4 bytes and build IPNet directly
	ipBytes := net.IP{
		byte(newIP >> 24),
		byte(newIP >> 16),
		byte(newIP >> 8),
		byte(newIP),
	}
	mask := net.CIDRMask(prefixLen, 32)
	return &net.IPNet{IP: ipBytes.Mask(mask), Mask: mask}
}

// GenerateNonOverlappingIPv4Subnet generates a non-overlapping ipv4 subnet with the given prefix size within the given range.
func GenerateNonOverlappingIPv4Subnet(existingNetworks []*net.IPNet, prefixLen int) (*net.IPNet, *net.IP, error) {
	// Validate prefix length is within acceptable range
	if prefixLen < 9 || prefixLen > 30 {
		return nil, nil, fmt.Errorf("invalid prefix length %d: must be between 9 and 30", prefixLen)
	}

	// Create a shuffled copy of private ranges for better distribution
	ranges := make([]struct{ network *net.IPNet }, len(privateRanges))
	copy(ranges, privateRanges)
	rngMu.Lock()
	rng.Shuffle(len(ranges), func(i, j int) {
		ranges[i], ranges[j] = ranges[j], ranges[i]
	})
	rngMu.Unlock()

	for _, rangeSpec := range ranges {
		for range 1000 { // Try 1000 times to find non-overlapping subnet
			candidate := generateRandomSubnet(rangeSpec.network, prefixLen)
			if candidate != nil {
				hasOverlap := false
				for _, existing := range existingNetworks {
					if overlaps(existing, candidate) {
						hasOverlap = true
						break
					}
				}
				if !hasOverlap {
					ip := make(net.IP, 4)
					copy(ip, candidate.IP.To4())
					ip[3] = 0x1 // make the gateway the first ip
					return candidate, &ip, nil
				}
			}
		}
	}
	return nil, nil, fmt.Errorf("unable to generate non-overlapping subnet after many attempts")
}
