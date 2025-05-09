package net

import (
	"context"
	"net"
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

var DefaultDialer = New()

type Dialer struct {
	Timeout       time.Duration
	Deadline      time.Time
	LocalAddr     net.Addr
	DualStack     bool
	FallbackDelay time.Duration
	KeepAlive     time.Duration
	Resolver      *net.Resolver
	Control       func(network, address string, c syscall.RawConn) error

	dnsServers []string
	counter    atomic.Uint64
}

type dialerOptions struct {
	dnsServers []string
}

type DialOption func(*dialerOptions)

func WithDNS(dnsServers ...string) DialOption {
	return func(opts *dialerOptions) {
		opts.dnsServers = dnsServers
	}
}

func New(opts ...DialOption) *Dialer {
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

	dialer.Resolver = &net.Resolver{
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

	return dialer
}

func (d *Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:       d.Timeout,
		Deadline:      d.Deadline,
		LocalAddr:     d.LocalAddr,
		DualStack:     d.DualStack,
		FallbackDelay: d.FallbackDelay,
		KeepAlive:     d.KeepAlive,
		Resolver:      d.Resolver,
		Control:       d.Control,
	}

	return dialer.DialContext(ctx, network, address)
}

func (d *Dialer) Dial(network, address string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, address)
}
