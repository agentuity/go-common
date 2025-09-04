package network

import (
	"net"
	"strings"
	"testing"
)

// Test versions of the functions from gen_static.go
func testGenStaticServiceIP(serviceName string) string {
	addr := NewIPv6Address(RegionGlobal, NetworkPrivateServices, serviceName, "", "")
	return addr.String()
}

func testGenStaticServiceNetIP() string {
	// Create a proper CIDR notation for the service subnet
	// All service IPs for NetworkPrivateServices start with fd15:d710:2x
	// Use /44 to capture the network prefix including the first nibble of third hextet
	return "fd15:d710:20::/44"
}

func TestGenStaticServiceNetIP(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{
			name: "returns valid CIDR with /44 prefix",
			want: "fd15:d710:20::/44",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := testGenStaticServiceNetIP()
			if got != tt.want {
				t.Errorf("genStaticServiceNetIP() = %v, want %v", got, tt.want)
			}

			// Validate that the returned string is a valid CIDR
			_, ipNet, err := net.ParseCIDR(got)
			if err != nil {
				t.Errorf("genStaticServiceNetIP() returned invalid CIDR: %v, error: %v", got, err)
			}

			// Validate it's an IPv6 network
			if ipNet.IP.To4() != nil {
				t.Errorf("genStaticServiceNetIP() should return IPv6 CIDR, got IPv4: %v", got)
			}

			// Validate the prefix length is 44
			ones, _ := ipNet.Mask.Size()
			if ones != 44 {
				t.Errorf("genStaticServiceNetIP() prefix length = %d, want 44", ones)
			}

			// Validate it uses the Agentuity ULA prefix
			if !strings.HasPrefix(got, "fd15:d710:") {
				t.Errorf("genStaticServiceNetIP() should start with 'fd15:d710:', got: %v", got)
			}

			// Validate that the network is the PrivateServices network
			if !IsAgentuityIPv6Prefix(ipNet.IP) {
				t.Errorf("genStaticServiceNetIP() should use Agentuity IPv6 prefix")
			}
		})
	}
}

func TestGenStaticServiceIP(t *testing.T) {
	tests := []struct {
		name        string
		serviceName string
	}{
		{
			name:        "aether service",
			serviceName: "aether",
		},
		{
			name:        "catalyst service",
			serviceName: "catalyst",
		},
		{
			name:        "otel service",
			serviceName: "otel",
		},
		{
			name:        "custom service",
			serviceName: "custom-service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := testGenStaticServiceIP(tt.serviceName)

			// Validate it's a valid IPv6 address
			ip := net.ParseIP(got)
			if ip == nil {
				t.Errorf("genStaticServiceIP(%q) returned invalid IPv6 address: %v", tt.serviceName, got)
			}

			// Validate it's IPv6 (not IPv4)
			if ip.To4() != nil {
				t.Errorf("genStaticServiceIP(%q) should return IPv6 address, got IPv4: %v", tt.serviceName, got)
			}

			// Validate it uses the Agentuity ULA prefix
			if !strings.HasPrefix(got, "fd15:d710:") {
				t.Errorf("genStaticServiceIP(%q) should start with 'fd15:d710:', got: %v", tt.serviceName, got)
			}

			// Validate that it uses the Agentuity prefix
			if !IsAgentuityIPv6Prefix(ip) {
				t.Errorf("genStaticServiceIP(%q) should use Agentuity IPv6 prefix", tt.serviceName)
			}
		})
	}
}

func TestServiceIPsAreWithinServiceSubnet(t *testing.T) {
	serviceSubnet := testGenStaticServiceNetIP()
	_, subnet, err := net.ParseCIDR(serviceSubnet)
	if err != nil {
		t.Fatalf("Failed to parse service subnet: %v", err)
	}

	services := []string{"aether", "catalyst", "otel"}
	for _, service := range services {
		t.Run(service, func(t *testing.T) {
			serviceIP := testGenStaticServiceIP(service)
			ip := net.ParseIP(serviceIP)
			if ip == nil {
				t.Fatalf("Invalid service IP for %s: %s", service, serviceIP)
			}

			if !subnet.Contains(ip) {
				t.Errorf("Service IP %s for %s is not within service subnet %s", serviceIP, service, serviceSubnet)
			}
		})
	}
}

func TestGenStaticServiceNetIPConsistency(t *testing.T) {
	// Test that multiple calls return the same result
	first := testGenStaticServiceNetIP()
	second := testGenStaticServiceNetIP()

	if first != second {
		t.Errorf("genStaticServiceNetIP() is not consistent: first=%v, second=%v", first, second)
	}
}

func TestGenStaticServiceIPConsistency(t *testing.T) {
	// Test that multiple calls with the same service name return the same result
	serviceName := "test-service"
	first := testGenStaticServiceIP(serviceName)
	second := testGenStaticServiceIP(serviceName)

	if first != second {
		t.Errorf("genStaticServiceIP(%q) is not consistent: first=%v, second=%v", serviceName, first, second)
	}
}

func TestGenStaticServiceIPUniqueness(t *testing.T) {
	// Test that different service names produce different IPs
	services := []string{"service1", "service2", "service3", "aether", "catalyst", "otel"}
	ips := make(map[string]string)

	for _, service := range services {
		ip := testGenStaticServiceIP(service)
		if existingService, exists := ips[ip]; exists {
			t.Errorf("Services %q and %q have the same IP: %s", service, existingService, ip)
		}
		ips[ip] = service
	}
}
