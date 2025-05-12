package tls

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/agentuity/go-common/dns"
	"github.com/redis/go-redis/v9"
)

// FetchTLSCert fetches a TLS certificate from aether
func FetchTLSCert(ctx context.Context, client *redis.Client, name string) (*tls.Certificate, time.Time, error) {
	cert, err := dns.SendDNSAction[dns.DNSCert](ctx, dns.CertRequestDNSAction(name), dns.WithRedis(client), dns.WithTimeout(time.Second*15))
	if err != nil {
		return nil, time.Time{}, err
	}
	tlsCert, err := tls.X509KeyPair(cert.Certificate, cert.PrivateKey)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("error parsing tls certificate: %w", err)
	}
	return &tlsCert, cert.Expires, nil
}
