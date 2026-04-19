package dns

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/agentuity/go-common/cache"
)

// DefaultInternalDomain is the default internal domain.
var DefaultInternalDomain = "agentuity.internal"

// DefaultManagedDomains is the default list of managed domains that will use the internal DNS servers otherwise will go to the external DNS servers.
var DefaultManagedDomains = []string{
	DefaultInternalDomain,
	"agentuity.cloud",
	"agentuity.cloud.internal",
	"agentuity.run",
	"agentuity.run.internal",
	"agentuity.live",
	"agentuity.live.internal",
	"agentuity-us.live",
	"agentuity-us.live.internal",
	"agentuity.app",
	"agentuity.app.internal",
}

// DefaultDNSServers is the default list of internal DNS servers.
var DefaultDNSServers = []string{
	"ns0.agentuity.com:53",
	"ns1.agentuity.com:53",
	"ns2.agentuity.com:53",
}

// DefaultExternalDNSServers is the default list of external DNS servers.
var DefaultExternalDNSServers = []string{
	"9.9.9.9:53",
	"1.1.1.1:53",
	"8.8.8.8:53",
}

// ErrInvalidIP is returned when an invalid IP address is resolved for a hostname.
var ErrInvalidIP = fmt.Errorf("invalid ip address resolved for hostname")

// DefaultDNS is the default DNS resolver.
var DefaultDNS = NewResolver()

type DNS interface {
	// Lookup performs a DNS lookup for the given hostname and returns a valid IP address for the A record.
	Lookup(ctx context.Context, hostname string) (bool, *net.IP, error)
}

type RecordType uint8

const (
	A     RecordType = 1
	CNAME RecordType = 5
)

type StatusType uint8

const (
	NoError  StatusType = 0
	FormErr  StatusType = 1
	ServFail StatusType = 2
	NXDomain StatusType = 3
	Refused  StatusType = 5
	NotAuth  StatusType = 9
	NotZone  StatusType = 10
)

func (s StatusType) String() string {
	switch s {
	case NoError:
		return "Success"
	case FormErr:
		return "Format Error"
	case ServFail:
		return "Server Fail"
	case NXDomain:
		return "Non-Existent Domain"
	case Refused:
		return "Query Refused"
	case NotAuth:
		return "Server Not Authoritative for zone"
	case NotZone:
		return "Name not contained in zone"
	default:
		return "Unknown DNS error"
	}
}

type Result struct {
	Status StatusType `json:"Status"`
	Answer []Answer   `json:"Answer"`
}

type Answer struct {
	Name string     `json:"name"`
	Type RecordType `json:"type"`
	TTL  uint       `json:"ttl"`
	Data string     `json:"data"`
}

type dnsConfig struct {
	FailIfLocal bool
	Cache       cache.Cache
}

type Dns struct {
	cache   cache.Cache
	isLocal bool
}

var _ DNS = (*Dns)(nil)

var ipv4 = regexp.MustCompile(`^(((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)(\.|$)){4})`)
var magicIpAddress = "169.254.169.254"
var googleMetadataHostname = "metadata.google.internal"
var cloudId = os.Getenv("AGENTUITY_CLOUD_ID")

func isPrivateIP(ip string) bool {
	ipAddress := net.ParseIP(ip)
	return ipAddress.IsPrivate() || ipAddress.IsLoopback()
}

func formatFQDN(hostname string) string {
	if strings.Contains(hostname, ".") {
		return hostname
	}
	if cloudId != "" && !strings.HasSuffix(hostname, "-"+cloudId) {
		return fmt.Sprintf("%s-%s.%s", hostname, cloudId, DefaultInternalDomain)
	}
	return fmt.Sprintf("%s.%s", hostname, DefaultInternalDomain)
}

// Lookup performs a DNS lookup for the given hostname and returns a valid IP address for the A record.
func (d *Dns) LookupMulti(ctx context.Context, hostname string) (bool, []net.IP, error) {
	if (hostname == "localhost" || hostname == "127.0.0.1") && d.isLocal {
		return true, []net.IP{{127, 0, 0, 1}}, nil
	}
	if hostname == googleMetadataHostname {
		val := net.ParseIP(magicIpAddress)
		if val == nil {
			return false, nil, ErrInvalidIP
		}
		return true, []net.IP{val}, nil
	}
	if isPrivateIP(hostname) && !d.isLocal {
		return false, nil, ErrInvalidIP
	}
	if hostname == magicIpAddress {
		return false, nil, ErrInvalidIP
	}
	if ipv4.MatchString(hostname) {
		ip := net.ParseIP(hostname)
		if ip == nil {
			return false, nil, fmt.Errorf("failed to parse ip address: %s", hostname)
		}
		return true, []net.IP{ip}, nil
	}
	// Check /etc/hosts before DNS-over-HTTPS. The system hosts file takes
	// precedence over external resolvers (matching nsswitch.conf "files dns"
	// order). This is critical in environments where /etc/hosts has the
	// correct mapping but external DNS returns a different address.
	if ip := resolveFromHostsFile(hostname); ip != nil {
		if !d.isLocal && (ip.IsPrivate() || ip.IsLoopback()) {
			return false, nil, fmt.Errorf("hostname %s resolved to local/private address %s via /etc/hosts", hostname, ip)
		}
		return true, []net.IP{ip}, nil
	}
	hostname = formatFQDN(hostname)
	var cacheKey string
	if d.cache != nil {
		cacheKey = fmt.Sprintf("dns:%s", hostname)
		ok, ips, _ := cache.GetContext[[]net.IP](ctx, d.cache, cacheKey)
		if ok {
			return true, ips, nil
		}
	}
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(c, "GET", "https://cloudflare-dns.com/dns-query?name="+url.QueryEscape(hostname), nil)
	if err != nil {
		return false, nil, err
	}
	req.Header.Set("accept", "application/dns-json")
	req.Header.Set("user-agent", "Agentuity (+https://agentuity.com)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return false, nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	var res Result
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return false, nil, fmt.Errorf("failed to decode dns json response: %w", err)
	}
	if res.Status != NoError {
		return false, nil, fmt.Errorf("dns lookup failed: %s", res.Status)
	}
	var ips []net.IP
	var minTTL uint
	for _, a := range res.Answer {
		if a.Type == A {
			if minTTL == 0 || a.TTL < minTTL {
				minTTL = a.TTL
			}
			ip := net.ParseIP(a.Data)
			if ip == nil {
				return false, nil, fmt.Errorf("failed to parse ip address: %s", a.Data)
			}
			ips = append(ips, ip)
		}
	}
	if len(ips) == 0 {
		return false, nil, fmt.Errorf("no A records found for %s", hostname)
	}
	if !d.isLocal {
		var validIPs []net.IP
		for _, ip := range ips {
			if !ip.IsPrivate() && !ip.IsLoopback() {
				validIPs = append(validIPs, ip)
			}
		}
		if len(validIPs) == 0 {
			return false, nil, ErrInvalidIP
		}
		ips = validIPs
	}
	if d.cache != nil {
		expires := time.Duration(minTTL) * time.Second
		expires = min(expires, time.Hour*24)
		d.cache.SetContext(ctx, cacheKey, ips, expires)
	}
	return true, ips, nil
}

// Lookup performs a DNS lookup for the given hostname and returns a valid IP address for the A record.
func (d *Dns) Lookup(ctx context.Context, hostname string) (bool, *net.IP, error) {
	ok, ips, err := d.LookupMulti(ctx, hostname)
	if ok && len(ips) > 0 {
		if len(ips) > 1 {
			// more than 1 ip, return a random one
			i := rand.Int31n(int32(len(ips)))
			return ok, &ips[i], nil
		}
		return ok, &ips[0], nil
	}
	return ok, nil, err
}

// NewResolver creates a new DNS caching resolver.
func NewResolver(opts ...WithConfig) *Dns {
	var config dnsConfig
	for _, opt := range opts {
		opt(&config)
	}
	val := &Dns{
		cache:   config.Cache,
		isLocal: !config.FailIfLocal,
	}
	return val
}

type WithConfig func(config *dnsConfig)

// WithFailIfLocal will cause the DNS resolver to fail if the hostname is a local hostname.
func WithFailIfLocal() WithConfig {
	return func(config *dnsConfig) {
		config.FailIfLocal = true
	}
}

// WithCache will set the cache for the DNS resolver.
func WithCache(cache cache.Cache) WithConfig {
	return func(config *dnsConfig) {
		config.Cache = cache
	}
}

// hostsFileCache caches parsed /etc/hosts entries to avoid reading
// the file on every DNS lookup. Refreshed every 30 seconds.
var (
	hostsFileMu       sync.RWMutex
	hostsFileEntries  map[string]net.IP // lowercase hostname → IP
	hostsFileLoadedAt time.Time
	hostsFileTTL      = 30 * time.Second
)

// ResolveFromHostsFile looks up a hostname in /etc/hosts (cached).
// Returns the first matching IP, or nil if not found.
func ResolveFromHostsFile(hostname string) net.IP {
	return resolveFromHostsFile(hostname)
}

func resolveFromHostsFile(hostname string) net.IP {
	hostsFileMu.RLock()
	if hostsFileEntries != nil && time.Since(hostsFileLoadedAt) < hostsFileTTL {
		ip := hostsFileEntries[strings.ToLower(strings.TrimSuffix(hostname, "."))]
		hostsFileMu.RUnlock()
		return ip
	}
	hostsFileMu.RUnlock()

	// Reload
	hostsFileMu.Lock()
	defer hostsFileMu.Unlock()

	// Double-check after acquiring write lock
	if hostsFileEntries != nil && time.Since(hostsFileLoadedAt) < hostsFileTTL {
		return hostsFileEntries[strings.ToLower(strings.TrimSuffix(hostname, "."))]
	}

	data, err := os.ReadFile("/etc/hosts")
	if err != nil {
		hostsFileEntries = make(map[string]net.IP)
		hostsFileLoadedAt = time.Now()
		return nil
	}

	entries := make(map[string]net.IP)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		ip := net.ParseIP(fields[0])
		if ip == nil {
			continue
		}
		for _, name := range fields[1:] {
			lower := strings.ToLower(name)
			if _, exists := entries[lower]; !exists {
				entries[lower] = ip
			}
		}
	}
	hostsFileEntries = entries
	hostsFileLoadedAt = time.Now()

	return entries[strings.ToLower(strings.TrimSuffix(hostname, "."))]
}
