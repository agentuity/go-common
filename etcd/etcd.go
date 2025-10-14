package etcd

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"time"

	"github.com/agentuity/go-common/logger"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/namespace"
	"go.uber.org/zap"
)

// EtcdClient is a simple wrapper around the etcd client
type EtcdClient struct {
	client *clientv3.Client
}

// Close the connection to etcd
func (c *EtcdClient) Close() error {
	return c.client.Close()
}

type opt struct {
	ctx       context.Context
	logger    logger.Logger
	endpoints []string
	caCertPEM []byte
	caKeyPEM  []byte
	expiry    time.Duration
	prefix    string
}

type optFun func(opt *opt)

// WithEndpoints allows overriding the default etcd endpoints
func WithEndpoints(endpoints []string) optFun {
	return func(opt *opt) {
		opt.endpoints = endpoints
	}
}

// WithCA allows overriding the CA PEM
func WithCA(caCertPEM []byte, caKeyPEM []byte) optFun {
	return func(opt *opt) {
		opt.caCertPEM = caCertPEM
		opt.caKeyPEM = caKeyPEM
	}
}

// WithPrefix allows overriding the default prefix (which is empty)
func WithPrefix(prefix string) optFun {
	return func(opt *opt) {
		opt.prefix = prefix
	}
}

// WithURL will override the default etcd endpoints with a single URL if provided (can be empty which means no override)
func WithURL(url string) optFun {
	return func(opt *opt) {
		if url == "" {
			return
		}
		opt.endpoints = []string{url}
	}
}

// WithContext allows overriding the default context (which is nil)
func WithContext(ctx context.Context) optFun {
	return func(opt *opt) {
		opt.ctx = ctx
	}
}

// WithLogger allows overriding the default logger (which is nil)
func WithLogger(logger logger.Logger) optFun {
	return func(opt *opt) {
		opt.logger = logger
	}
}

// New creates a new EtcdClient instance with the provided options
func New(opts ...optFun) (*EtcdClient, error) {
	var client EtcdClient
	var opt opt

	opt.expiry = time.Hour * 24 * 30 * 12 // ~1 year

	opt.endpoints = []string{
		"https://etcd1.agentuity.com:2379",
		"https://etcd2.agentuity.com:2379",
		"https://etcd3.agentuity.com:2379",
	}

	if val, ok := os.LookupEnv("AGENTUITY_CA_CERTIFICATE"); ok && val != "" {
		buf, err := base64.StdEncoding.DecodeString(val)
		if err != nil {
			return nil, fmt.Errorf("failed to decode CA certificate: %w", err)
		}
		opt.caCertPEM = buf
	}

	if val, ok := os.LookupEnv("AGENTUITY_CA_KEY"); ok && val != "" {
		buf, err := base64.StdEncoding.DecodeString(val)
		if err != nil {
			return nil, fmt.Errorf("failed to decode CA key: %w", err)
		}
		opt.caKeyPEM = buf
	}

	for _, optfn := range opts {
		optfn(&opt)
	}

	var tlsConfig *tls.Config

	if len(opt.caCertPEM) > 0 {
		if len(opt.caKeyPEM) == 0 {
			return nil, fmt.Errorf("CA Cert PEM is configured but not the CA Private Key")
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(opt.caCertPEM) {
			return nil, fmt.Errorf("failed to append CA cert")
		}

		caCertBlock, _ := pem.Decode(opt.caCertPEM)
		caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse CA cert: %v", err)
		}

		caKeyBlock, _ := pem.Decode(opt.caKeyPEM)
		caKey, err := x509.ParseECPrivateKey(caKeyBlock.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse CA key: %v", err)
		}

		// Generate client private key (EC to match CA)
		clientKey, err := ecdsa.GenerateKey(caKey.Curve, rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("failed to generate client key: %v", err)
		}

		commonName := "etcd-client"
		if opt.prefix != "" {
			commonName = fmt.Sprintf("%s (%s)", commonName, opt.prefix)
		}

		// Create client certificate template
		serialNumber, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		clientCertTemplate := &x509.Certificate{
			SerialNumber: serialNumber,
			Subject: pkix.Name{
				CommonName:   commonName,
				Organization: []string{"Agentuity, Inc"},
				Country:      []string{"US"},
			},
			NotBefore:             time.Now(),
			NotAfter:              time.Now().Add(opt.expiry),
			KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
			ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
			BasicConstraintsValid: true,
		}

		// Sign the client certificate with CA
		clientCertDER, err := x509.CreateCertificate(rand.Reader, clientCertTemplate, caCert, &clientKey.PublicKey, caKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create client cert: %v", err)
		}

		// Create TLS certificate from generated cert and key
		clientCert, err := x509.ParseCertificate(clientCertDER)
		if err != nil {
			return nil, fmt.Errorf("failed to parse client cert: %v", err)
		}

		tlsCert := tls.Certificate{
			Certificate: [][]byte{clientCertDER},
			PrivateKey:  clientKey,
			Leaf:        clientCert,
		}

		// Configure TLS
		tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
			RootCAs:      caCertPool,
			MinVersion:   tls.VersionTLS12,
		}
	}

	// create an adapter for zap logger
	var l *zap.Logger
	if opt.logger != nil {
		l = logger.ToZap(opt.logger)
	}

	// Create etcd client
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   opt.endpoints,
		TLS:         tlsConfig,
		DialTimeout: 5 * time.Second,
		Context:     opt.ctx,
		Logger:      l,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to connect to etcd: %v", err)
	}

	if opt.prefix != "" {
		cli.KV = namespace.NewKV(cli.KV, opt.prefix)
		cli.Watcher = namespace.NewWatcher(cli.Watcher, opt.prefix)
		cli.Lease = namespace.NewLease(cli.Lease, opt.prefix)
	}

	client.client = cli

	return &client, nil
}
