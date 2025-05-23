package dns

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// FetchTLSCert fetches a TLS certificate from aether
func FetchTLSCert(ctx context.Context, client *redis.Client, name string) (*tls.Certificate, time.Time, error) {
	cert, err := SendDNSAction(ctx, CertRequestDNSAction(name), WithRedis(client), WithTimeout(time.Second*15))
	if err != nil {
		return nil, time.Time{}, err
	}
	tlsCert, err := tls.X509KeyPair(cert.Certificate, cert.PrivateKey)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("error parsing tls certificate: %w", err)
	}
	return &tlsCert, cert.Expires, nil
}
