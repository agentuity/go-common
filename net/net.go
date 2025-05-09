package net

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	cdns "github.com/agentuity/go-common/dns"
)

var DefaultDNSServers = []string{
	"ns1.agentuity.com:53",
	"ns2.agentuity.com:53",
	"ns3.agentuity.com:53",
}

const DefaultDialTimeout = 30 * time.Second

var DefaultDialer *Dialer

func init() {
	var err error
	DefaultDialer, err = New()
	if err != nil {
		panic(err)
	}
}

type Dialer struct {
	Timeout       time.Duration
	Deadline      time.Time
	LocalAddr     net.Addr
	DualStack     bool
	FallbackDelay time.Duration
	KeepAlive     time.Duration
	Control       func(network, address string, c syscall.RawConn) error

	dnsServers []string
	resolver   *net.Resolver
	counter    atomic.Uint64
}

type dialerOptions struct {
	dnsServers []string
}

type DialOption func(*dialerOptions)

// WithDNS sets the DNS servers to use for the dialer instead of the default ones.
func WithDNS(dnsServers ...string) DialOption {
	return func(opts *dialerOptions) {
		opts.dnsServers = dnsServers
	}
}

func New(opts ...DialOption) (*Dialer, error) {
	options := &dialerOptions{
		dnsServers: DefaultDNSServers,
	}

	for _, opt := range opts {
		opt(options)
	}

	dialer := &Dialer{
		Timeout:    DefaultDialTimeout,
		dnsServers: []string{},
	}

	var wg sync.WaitGroup
	errors := make(chan error, len(options.dnsServers))
	addresses := make([]string, 0)
	var mu sync.Mutex

	for _, dnsServer := range options.dnsServers {
		wg.Add(1)
		go func(dnsServer string) {
			defer wg.Done()
			host, port, err := net.SplitHostPort(dnsServer)
			if err != nil {
				errors <- err
				return
			}
			if port == "" {
				port = "53"
			}
			ok, ip, err := cdns.DefaultDNS.Lookup(context.Background(), host)
			if err != nil {
				errors <- err
			} else {
				if ok {
					mu.Lock()
					addresses = append(addresses, ip.String()+":"+port)
					mu.Unlock()
				}
			}
		}(dnsServer)
	}

	wg.Wait()
	close(errors)

	select {
	case err := <-errors:
		if err != nil {
			// if we have at least one address, don't fail
			if len(addresses) == 0 {
				return nil, err
			}
		}
	default:
	}

	dialer.dnsServers = addresses

	if len(dialer.dnsServers) == 0 {
		return nil, fmt.Errorf("no dns servers found")
	}

	dialer.resolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			idx := dialer.counter.Add(1) % uint64(len(dialer.dnsServers))
			dnsServer := dialer.dnsServers[idx]

			d := net.Dialer{
				Timeout: dialer.Timeout,
			}
			return d.DialContext(ctx, "udp", dnsServer)
		},
	}

	return dialer, nil
}

func (d *Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:       d.Timeout,
		Deadline:      d.Deadline,
		LocalAddr:     d.LocalAddr,
		DualStack:     d.DualStack,
		FallbackDelay: d.FallbackDelay,
		KeepAlive:     d.KeepAlive,
		Resolver:      d.resolver,
		Control:       d.Control,
	}
	return dialer.DialContext(ctx, network, address)
}

func (d *Dialer) Dial(network, address string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, address)
}

var DefaultClient *http.Client

type defaultRoundTripper struct {
	next http.RoundTripper
}

func (d *defaultRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "Agentuity (https://agentuity.com)")
	}
	return d.next.RoundTrip(req)
}

func init() {
	DefaultClient = &http.Client{
		Transport: &defaultRoundTripper{
			next: &http.Transport{
				DialContext: DefaultDialer.DialContext,
			},
		},
	}
}
