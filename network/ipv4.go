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

	hostBits := prefixLen - parentOnes
	if hostBits < 0 {
		return nil
	}

	randBytes := make([]byte, 4)
	copy(randBytes, parentIP.To4())
	// Randomly add offset in the host bits
	offset := rand.Intn(1 << hostBits)
	randBytes[3] += byte(offset)

	newSubnet := fmt.Sprintf("%s/%d", net.IP(randBytes).String(), prefixLen)
	_, result, _ := net.ParseCIDR(newSubnet)
	return result
}

// GenerateNonOverlappingIPv4Subnet generates a non-overlapping ipv4 subnet with the given prefix size within the given range.
func GenerateNonOverlappingIPv4Subnet(existingNetworks []*net.IPNet, prefixLen int) (*net.IPNet, *net.IP, error) {
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
