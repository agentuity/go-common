package vip

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"testing"
)

// mustParseSubnet parses a CIDR string and returns the IPNet or panics.
func mustParseSubnet(cidr string) net.IPNet {
	_, subnet, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(fmt.Sprintf("bad CIDR in test: %s: %v", cidr, err))
	}
	return *subnet
}

func TestGenerateDeterministicIPV6_Determinism(t *testing.T) {
	t.Parallel()

	subnet := mustParseSubnet("fd15:d710:0300:0000:a1b2:1234::/96")
	srcIP := net.ParseIP("192.168.1.100")
	dstIP := net.ParseIP("10.0.0.1")

	ip1, err := GenerateDeterministicIPV6("container-abc", srcIP, 12345, dstIP, 80, subnet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Call again with the same inputs.
	ip2, err := GenerateDeterministicIPV6("container-abc", srcIP, 12345, dstIP, 80, subnet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !ip1.Equal(ip2) {
		t.Errorf("expected deterministic results, got %s and %s", ip1, ip2)
	}

	// Call a third time to be sure.
	ip3, err := GenerateDeterministicIPV6("container-abc", srcIP, 12345, dstIP, 80, subnet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ip1.Equal(ip3) {
		t.Errorf("expected deterministic results on third call, got %s and %s", ip1, ip3)
	}
}

func TestGenerateDeterministicIPV6_Uniqueness(t *testing.T) {
	t.Parallel()

	subnet := mustParseSubnet("fd15:d710:0300:0000:a1b2:1234::/96")
	srcIP := net.ParseIP("192.168.1.100")
	dstIP := net.ParseIP("10.0.0.1")

	tests := []struct {
		name        string
		containerID string
		srcIP       net.IP
		srcPort     uint16
		dstIP       net.IP
		dstPort     uint16
	}{
		{"base", "container-abc", srcIP, 12345, dstIP, 80},
		{"different container", "container-xyz", srcIP, 12345, dstIP, 80},
		{"different src port", "container-abc", srcIP, 54321, dstIP, 80},
		{"different dst port", "container-abc", srcIP, 12345, dstIP, 443},
		{"different src ip", "container-abc", net.ParseIP("10.10.10.10"), 12345, dstIP, 80},
		{"different dst ip", "container-abc", srcIP, 12345, net.ParseIP("172.16.0.1"), 80},
		{"all different", "container-def", net.ParseIP("1.2.3.4"), 9999, net.ParseIP("5.6.7.8"), 8080},
	}

	results := make(map[string]string)
	for _, tc := range tests {
		ip, err := GenerateDeterministicIPV6(tc.containerID, tc.srcIP, tc.srcPort, tc.dstIP, tc.dstPort, subnet)
		if err != nil {
			t.Fatalf("[%s] unexpected error: %v", tc.name, err)
		}
		key := ip.String()
		if prev, exists := results[key]; exists {
			t.Errorf("collision: %q and %q both produced %s", prev, tc.name, key)
		}
		results[key] = tc.name
	}
}

func TestGenerateDeterministicIPV6_SubnetContainment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		subnet string
	}{
		{"subnet A", "fd15:d710:0300:0000:a1b2:1234::/96"},
		{"subnet B", "2001:db8:abcd:ef01:0000:0000::/96"},
		{"subnet C", "fd00::/96"},
	}

	srcIP := net.ParseIP("192.168.1.1")
	dstIP := net.ParseIP("10.0.0.1")

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			subnet := mustParseSubnet(tc.subnet)
			ip, err := GenerateDeterministicIPV6("test-container", srcIP, 8080, dstIP, 443, subnet)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !subnet.Contains(ip) {
				t.Errorf("result %s is not within subnet %s", ip, subnet.String())
			}
		})
	}
}

func TestGenerateDeterministicIPV6_ZeroSkip(t *testing.T) {
	t.Parallel()

	// We cannot easily find inputs that hash to exactly zero, so we verify
	// the logic by checking that the implementation's zero-skip path is
	// correct. We manually compute the hash for known inputs and verify
	// that if it were zero, the result would be 1.
	//
	// Additionally, we verify the function never returns the subnet's
	// network address (host ID 0) for a large sample of inputs.
	subnet := mustParseSubnet("fd15:d710:0300:0000:a1b2:1234::/96")
	networkAddr := make(net.IP, 16)
	copy(networkAddr, subnet.IP.To16())

	for i := 0; i < 1000; i++ {
		containerID := fmt.Sprintf("container-%d", i)
		srcIP := net.ParseIP("192.168.1.1")
		dstIP := net.ParseIP("10.0.0.1")
		ip, err := GenerateDeterministicIPV6(containerID, srcIP, uint16(i), dstIP, 80, subnet)
		if err != nil {
			t.Fatalf("unexpected error at iteration %d: %v", i, err)
		}
		if ip.Equal(networkAddr) {
			t.Errorf("iteration %d produced the subnet network address (host=0)", i)
		}
	}

	// Verify the zero-skip logic directly: craft the hash check.
	// If the first 4 bytes of a SHA-256 hash are all zero, hostID should become 1.
	// We simulate this by checking: if hostID would be 0, the result has host byte = 1.
	// Find an input whose hash starts with 0x00000000 is impractical, so we test
	// the internal invariant: the last 4 bytes of the result are never all zero.
	for i := 0; i < 1000; i++ {
		containerID := fmt.Sprintf("zero-test-%d", i)
		srcIP := net.ParseIP("1.1.1.1")
		dstIP := net.ParseIP("2.2.2.2")
		ip, err := GenerateDeterministicIPV6(containerID, srcIP, uint16(i), dstIP, uint16(i+1), subnet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		hostID := binary.BigEndian.Uint32(ip[12:])
		if hostID == 0 {
			t.Errorf("host ID is zero for containerID=%s srcPort=%d dstPort=%d", containerID, i, i+1)
		}
	}
}

func TestGenerateDeterministicIPV6_ZeroSkipDirect(t *testing.T) {
	t.Parallel()

	// Directly verify the zero-skip logic by computing the hash ourselves
	// and checking that for any input whose hash starts with 4 zero bytes,
	// the function would use hostID=1 instead of 0.
	//
	// We brute force a small space to look for a near-zero hash (first 4 bytes == 0).
	// If we find one, great. If not, we at least verify the output matches
	// our expected computation for all tested inputs.
	subnet := mustParseSubnet("fd15:d710:0300:0000:a1b2:1234::/96")
	srcIP := net.ParseIP("192.168.1.1")
	dstIP := net.ParseIP("10.0.0.1")

	for i := 0; i < 10000; i++ {
		containerID := fmt.Sprintf("brute-%d", i)
		srcPort := uint16(i)
		dstPort := uint16(80)

		input := fmt.Sprintf("%s|%s|%d|%s|%d", containerID, srcIP, srcPort, dstIP, dstPort)
		hash := sha256.Sum256([]byte(input))
		expectedHostID := binary.BigEndian.Uint32(hash[:4])
		if expectedHostID == 0 {
			expectedHostID = 1
		}

		ip, err := GenerateDeterministicIPV6(containerID, srcIP, srcPort, dstIP, dstPort, subnet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		actualHostID := binary.BigEndian.Uint32(ip[12:])
		if actualHostID != expectedHostID {
			t.Errorf("input %d: expected hostID %d, got %d", i, expectedHostID, actualHostID)
		}
	}
}

func TestGenerateDeterministicIPV6_InvalidSubnet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		subnet string
	}{
		{"too small /64", "fd15:d710::/64"},
		{"too large /112", "fd15:d710:0300:0000:a1b2:1234:5678::/112"},
		{"ipv4 /24", "192.168.1.0/24"},
		{"full /128", "fd15:d710::1/128"},
	}

	srcIP := net.ParseIP("192.168.1.1")
	dstIP := net.ParseIP("10.0.0.1")

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			subnet := mustParseSubnet(tc.subnet)
			_, err := GenerateDeterministicIPV6("test", srcIP, 80, dstIP, 443, subnet)
			if err == nil {
				t.Errorf("expected error for subnet %s, got nil", tc.subnet)
			}
		})
	}
}

func TestGenerateDeterministicIPV6_ConcurrentSafety(t *testing.T) {
	t.Parallel()

	subnet := mustParseSubnet("fd15:d710:0300:0000:a1b2:1234::/96")
	srcIP := net.ParseIP("192.168.1.100")
	dstIP := net.ParseIP("10.0.0.1")

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	results := make([]net.IP, goroutines)
	errors := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			ip, err := GenerateDeterministicIPV6("container-concurrent", srcIP, 12345, dstIP, 80, subnet)
			results[idx] = ip
			errors[idx] = err
		}(i)
	}

	wg.Wait()

	// All goroutines should succeed and produce the same result.
	for i := 0; i < goroutines; i++ {
		if errors[i] != nil {
			t.Fatalf("goroutine %d failed: %v", i, errors[i])
		}
		if !results[0].Equal(results[i]) {
			t.Errorf("goroutine %d produced %s, expected %s", i, results[i], results[0])
		}
	}
}

func TestGenerateDeterministicIPV6_DifferentSubnets(t *testing.T) {
	t.Parallel()

	// Same flow parameters but different subnets should produce different IPs.
	subnetA := mustParseSubnet("fd15:d710:0300:0000:a1b2:1234::/96")
	subnetB := mustParseSubnet("fd15:d710:0400:0000:b2c3:5678::/96")

	srcIP := net.ParseIP("192.168.1.100")
	dstIP := net.ParseIP("10.0.0.1")

	ipA, err := GenerateDeterministicIPV6("container-abc", srcIP, 12345, dstIP, 80, subnetA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ipB, err := GenerateDeterministicIPV6("container-abc", srcIP, 12345, dstIP, 80, subnetB)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ipA.Equal(ipB) {
		t.Errorf("expected different IPs for different subnets, both got %s", ipA)
	}

	// Both should still be in their respective subnets.
	if !subnetA.Contains(ipA) {
		t.Errorf("IP %s not in subnet %s", ipA, subnetA.String())
	}
	if !subnetB.Contains(ipB) {
		t.Errorf("IP %s not in subnet %s", ipB, subnetB.String())
	}
}

func TestCollisionCheck_NoCollision(t *testing.T) {
	t.Parallel()
	_, subnet, err := net.ParseCIDR("fd00:1::/96")
	if err != nil {
		t.Fatalf("ParseCIDR: %v", err)
	}
	srcIP := net.ParseIP("fd00::1")
	dstIP := net.ParseIP("fd00::2")

	ip, err := GenerateDeterministicIPV6WithCollisionCheck("machine-1", srcIP, 1000, dstIP, 80, *subnet, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !subnet.Contains(ip) {
		t.Fatalf("IP %s not in subnet %s", ip, subnet)
	}
}

func TestCollisionCheck_RehashesOnCollision(t *testing.T) {
	t.Parallel()
	_, subnet, err := net.ParseCIDR("fd00:1::/96")
	if err != nil {
		t.Fatalf("ParseCIDR: %v", err)
	}
	srcIP := net.ParseIP("fd00::1")
	dstIP := net.ParseIP("fd00::2")

	// Get the base IP (no collision check)
	baseIP, err := GenerateDeterministicIPV6("machine-1", srcIP, 1000, dstIP, 80, *subnet)
	if err != nil {
		t.Fatalf("GenerateDeterministicIPV6: %v", err)
	}

	// Force collision on the base IP — should rehash with salt
	calls := 0
	ip, err := GenerateDeterministicIPV6WithCollisionCheck("machine-1", srcIP, 1000, dstIP, 80, *subnet,
		func(candidate net.IP) bool {
			calls++
			return candidate.Equal(baseIP) // first attempt collides
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip.Equal(baseIP) {
		t.Fatal("expected different IP after collision rehash")
	}
	if calls < 2 {
		t.Fatalf("expected at least 2 collision check calls, got %d", calls)
	}
	if !subnet.Contains(ip) {
		t.Fatalf("rehashed IP %s not in subnet %s", ip, subnet)
	}
}

func TestCollisionCheck_ExhaustsAttempts(t *testing.T) {
	t.Parallel()
	_, subnet, err := net.ParseCIDR("fd00:1::/96")
	if err != nil {
		t.Fatalf("ParseCIDR: %v", err)
	}
	srcIP := net.ParseIP("fd00::1")
	dstIP := net.ParseIP("fd00::2")

	// Always collide — verify the loop runs exactly maxAttempts (8) times
	calls := 0
	_, err = GenerateDeterministicIPV6WithCollisionCheck("machine-1", srcIP, 1000, dstIP, 80, *subnet,
		func(net.IP) bool { calls++; return true },
	)
	if err == nil {
		t.Fatal("expected error after exhausting collision attempts")
	}
	if calls != 8 {
		t.Fatalf("expected 8 collision check calls, got %d", calls)
	}
}
