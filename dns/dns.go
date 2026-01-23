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
	"agentuity.ai",
	"agentuity.ai.internal",
	"agentuity.app",
	"agentuity.app.internal",
	"agentuity.dev",
	"agentuity.dev.internal",
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
func (d *Dns) Lookup(ctx context.Context, hostname string) (bool, *net.IP, error) {
	if (hostname == "localhost" || hostname == "127.0.0.1") && d.isLocal {
		return true, &net.IP{127, 0, 0, 1}, nil
	}
	if hostname == googleMetadataHostname {
		val := net.ParseIP(magicIpAddress)
		if val == nil {
			return false, nil, ErrInvalidIP
		}
		return true, &val, nil
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
		return true, &ip, nil
	}
	hostname = formatFQDN(hostname)
	var cacheKey string
	if d.cache != nil {
		cacheKey = fmt.Sprintf("dns:%s", hostname)
		ok, val, _ := d.cache.Get(cacheKey)
		if ok {
			ip, ok := val.([]net.IP)
			if ok {
				// only 1 ip, return it
				if len(ip) == 1 {
					return true, &ip[0], nil
				}
				// more than 1 ip, return a random one
				i := rand.Int31n(int32(len(ip)))
				return true, &ip[i], nil
			}
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
	if (ips[0].IsPrivate() || ips[0].IsLoopback()) && !d.isLocal {
		return false, nil, ErrInvalidIP
	}
	if d.cache != nil {
		expires := time.Duration(minTTL) * time.Second
		expires = min(expires, time.Hour*24)
		d.cache.Set(cacheKey, ips, expires)
	}
	return true, &ips[0], nil
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
