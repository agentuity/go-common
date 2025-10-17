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
}

// DefaultDNSConfig returns a default DNS configuration
func DefaultDNSConfig() DNSConfig {
	return DNSConfig{
		ListenAddress:       ":53",
		ManagedDomains:      DefaultManagedDomains,
		InternalNameservers: DefaultDNSServers,
		UpstreamNameservers: DefaultExternalDNSServers,
		QueryTimeout:        "5s",
		DefaultProtocol:     "udp",
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
