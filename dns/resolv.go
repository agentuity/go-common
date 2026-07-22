package dns

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
)

// DefaultResolveConfFilename is the default filename for the resolv.conf file.
var DefaultResolveConfFilename = "/etc/resolv.conf"

// ParsedResolvConf represents the parsed contents of a resolv.conf file
type ParsedResolvConf struct {
	Nameservers []string
	Search      []string
}

// ParseResolvConf parses a resolv.conf file and returns the nameservers and search domains.
// It skips nameservers that are unsuitable as recursive upstreams:
//   - loopback (127.0.0.0/8, ::1) to avoid circular references to a local stub
//   - cloud metadata / link-local (169.254.169.254 and 169.254.0.0/16) which are
//     not portable recursive resolvers (on Azure, 169.254.169.254 is IMDS, not DNS)
//
// If filename is empty, it defaults to /etc/resolv.conf
func ParseResolvConf(filename string) (*ParsedResolvConf, error) {
	if filename == "" {
		filename = DefaultResolveConfFilename
	}

	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", filename, err)
	}
	defer file.Close()

	result := &ParsedResolvConf{}
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		switch fields[0] {
		case "nameserver":
			ns := fields[1]
			// Skip addresses that must not be used as recursive upstreams.
			if isUnsuitableUpstreamNameserver(ns) {
				continue
			}
			// Add port 53 if not specified
			// Handle IPv6 addresses which contain colons as part of the address
			if strings.HasPrefix(ns, "[") {
				// IPv6 with brackets - check if port is present (e.g., [::1]:53)
				if !strings.Contains(ns, "]:") {
					ns = ns + ":53"
				}
			} else if ip := net.ParseIP(ns); ip != nil {
				// Valid IP address without port
				if ip.To4() == nil {
					// IPv6 without brackets - wrap and add port
					ns = "[" + ns + "]:53"
				} else {
					// IPv4 without port
					ns = ns + ":53"
				}
			} else if !strings.Contains(ns, ":") {
				// Hostname without port
				ns = ns + ":53"
			}
			result.Nameservers = append(result.Nameservers, ns)
		case "search":
			result.Search = append(result.Search, fields[1:]...)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", filename, err)
	}

	return result, nil
}

// nameserverHost extracts the host portion of a nameserver address that may
// include a port (e.g. "8.8.8.8:53", "[2001:db8::1]:53", or bare "8.8.8.8").
func nameserverHost(addr string) string {
	host := addr
	if strings.Contains(addr, ":") {
		h, _, err := net.SplitHostPort(addr)
		if err == nil {
			return h
		}
		// Bare IPv6 without brackets/port, or other non-host:port form.
	}
	return host
}

// isLoopbackAddress checks if an address is a loopback address (127.0.0.0/8 or ::1)
func isLoopbackAddress(addr string) bool {
	ip := net.ParseIP(nameserverHost(addr))
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// isCloudMetadataNameserver reports whether addr is a cloud instance-metadata
// or link-local address that must not be used as a recursive DNS upstream.
//
// 169.254.169.254 is the multi-cloud metadata endpoint (IMDS). On GCP it
// sometimes answers DNS; on Azure it is HTTP metadata only and UDP/53 times
// out. Preferring it as the first upstream burns the local stub's query
// budget and breaks multi-level CNAME resolution (e.g. Upstash Redis hosts).
//
// Broader 169.254.0.0/16 (IPv4 link-local) and fe80::/10 (IPv6 link-local)
// are also unsuitable as portable recursive resolvers.
func isCloudMetadataNameserver(addr string) bool {
	ip := net.ParseIP(nameserverHost(addr))
	if ip == nil {
		return false
	}
	if ip.IsLinkLocalUnicast() {
		return true
	}
	// Explicit metadata well-known address (also covered by link-local, kept
	// for clarity and in case of future non-link-local metadata DNS).
	return ip.Equal(net.IPv4(169, 254, 169, 254))
}

// isUnsuitableUpstreamNameserver reports whether addr should be excluded from
// recursive upstream lists (loopback stub or cloud metadata / link-local).
func isUnsuitableUpstreamNameserver(addr string) bool {
	return isLoopbackAddress(addr) || isCloudMetadataNameserver(addr)
}

// GetSystemNameservers returns a merged list of nameservers: first the nameservers
// from the system's resolv.conf (excluding loopback and cloud-metadata addresses),
// then the DefaultExternalDNSServers as fallbacks. Duplicates are removed.
func GetSystemNameservers() []string {
	parsed, err := ParseResolvConf(DefaultResolveConfFilename)

	var result []string
	seen := make(map[string]bool)

	// Add system nameservers first (if any)
	if err == nil {
		for _, ns := range parsed.Nameservers {
			if isUnsuitableUpstreamNameserver(ns) {
				continue
			}
			if !seen[ns] {
				seen[ns] = true
				result = append(result, ns)
			}
		}
	}

	// Add default external nameservers as fallbacks
	for _, ns := range DefaultExternalDNSServers {
		if !seen[ns] {
			seen[ns] = true
			result = append(result, ns)
		}
	}

	return result
}

// WriteResolvConf writes a resolv.conf file that points clients at the local
// DNS stub (127.0.0.1). A public recursive server from DefaultExternalDNSServers
// is included as a last-resort fallback when the local stub is unavailable.
//
// Cloud metadata (169.254.169.254) is intentionally not used: on Azure it is
// IMDS, not recursive DNS, and caused intermittent lookup timeouts.
//
// If filename is empty, it defaults to /etc/resolv.conf
func WriteResolvConf(filename string) error {
	if filename == "" {
		filename = DefaultResolveConfFilename
	}

	var b strings.Builder
	b.WriteString("# Generated by agentuity/go-common/dns\n")
	b.WriteString("nameserver 127.0.0.1\n")
	if fallback := resolvConfNameserverHost(DefaultExternalDNSServers); fallback != "" {
		b.WriteString("nameserver ")
		b.WriteString(fallback)
		b.WriteString("\n")
	}

	// Write the file with appropriate permissions (0644)
	if err := os.WriteFile(filename, []byte(b.String()), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", filename, err)
	}

	return nil
}

// resolvConfNameserverHost returns the first DefaultExternalDNSServers entry
// in resolv.conf form (host only, no port). Empty if none configured.
func resolvConfNameserverHost(servers []string) string {
	for _, ns := range servers {
		host := nameserverHost(ns)
		if host == "" || isUnsuitableUpstreamNameserver(host) {
			continue
		}
		// resolv.conf wants a bare IP or hostname, not host:port.
		if ip := net.ParseIP(host); ip != nil {
			return host
		}
		if !strings.Contains(host, ":") {
			return host
		}
	}
	return ""
}
