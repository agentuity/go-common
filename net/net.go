package net

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"syscall"
	"time"
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
		dnsServers: options.dnsServers,
	}

	if len(dialer.dnsServers) == 0 {
		return nil, fmt.Errorf("no dns servers provided")
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
