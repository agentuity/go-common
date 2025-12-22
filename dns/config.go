package dns

import (
	"context"
	"fmt"
	"net"
	"strings"
)

// DNSConfig holds configuration for DNS resolver
type DNSConfig struct {
	// ListenAddress is the address to bind DNS server to (default: :53)
	ListenAddress string
	// ManagedDomains are domains that should be forwarded to internal nameservers
	ManagedDomains []string
	// InternalNameservers are the nameservers to forward managed domain queries to
	InternalNameservers []string
	// UpstreamNameservers are the nameservers to forward other queries to
	UpstreamNameservers []string
	// Timeout for DNS queries
	QueryTimeout string
	// DialContext is the function to use for dialing connections
	DialContext func(ctx context.Context, network, address string) (net.Conn, error)
	// DefaultProtocol is the default protocol to use for dialing connections which is UDP
	DefaultProtocol string
	// DefaultNegativeTTL is the TTL (in seconds) to use for negative responses when no SOA record
	// is present. Per RFC 2308, negative responses should include an SOA record, but some servers
	// don't comply. Set to 0 to disable caching when SOA is missing. Default is 30 seconds.
	DefaultNegativeTTL uint32
}

// DefaultDNSConfig returns a default DNS configuration.
// It automatically detects upstream nameservers from the system's /etc/resolv.conf
// before it gets overwritten, falling back to DefaultExternalDNSServers if parsing fails.
func DefaultDNSConfig() DNSConfig {
	return DNSConfig{
		ListenAddress:       ":53",
		ManagedDomains:      DefaultManagedDomains,
		InternalNameservers: DefaultDNSServers,
		UpstreamNameservers: GetSystemNameservers(),
		QueryTimeout:        "5s",
		DefaultProtocol:     "udp",
		DefaultNegativeTTL:  30, // 30 seconds default for negative caching without SOA
	}
}

// IsManagedDomain checks if a domain should be resolved by internal nameservers
func (c *DNSConfig) IsManagedDomain(domain string) bool {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))

	for _, managed := range c.ManagedDomains {
		managedLower := strings.ToLower(managed)
		if domain == managedLower || strings.HasSuffix(domain, "."+managedLower) {
			return true
		}
	}
	return false
}

// Validate checks if the DNS configuration is valid
func (c *DNSConfig) Validate() error {
	if len(c.ManagedDomains) == 0 {
		return fmt.Errorf("no managed domains configured")
	}
	if len(c.InternalNameservers) == 0 {
		return fmt.Errorf("no internal nameservers configured")
	}
	if len(c.UpstreamNameservers) == 0 {
		return fmt.Errorf("no upstream nameservers configured")
	}
	return nil
}
