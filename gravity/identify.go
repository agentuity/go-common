package gravity

import (
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"strings"
	"time"

	pb "github.com/agentuity/go-common/gravity/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// IdentifyConfig contains configuration for the Identify one-shot call.
type IdentifyConfig struct {
	GravityURL        string
	InstanceID        string
	ECDSAPrivateKey   *ecdsa.PrivateKey
	CACert            string // Optional: additional CA certificate PEM to trust
	DefaultServerName string // Optional: fallback TLS ServerName when connecting via IP address
}

// Identify performs a one-shot authentication to retrieve the org ID.
// It connects using a self-signed certificate generated from the provided ECDSA private key.
func Identify(ctx context.Context, config IdentifyConfig) (*pb.IdentifyResponse, error) {
	if config.GravityURL == "" {
		return nil, fmt.Errorf("GravityURL is required")
	}
	if config.InstanceID == "" {
		return nil, fmt.Errorf("InstanceID is required")
	}
	if config.ECDSAPrivateKey == nil {
		return nil, fmt.Errorf("ECDSAPrivateKey is required")
	}

	serverName, err := extractHostnameFromGravityURL(config.GravityURL, config.DefaultServerName)
	if err != nil {
		return nil, fmt.Errorf("failed to extract serverName from URL: %w", err)
	}

	selfSignedCert, err := createSelfSignedTLSConfig(config.ECDSAPrivateKey, config.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to create self-signed certificate: %w", err)
	}

	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if config.CACert != "" {
		pool.AppendCertsFromPEM([]byte(config.CACert))
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{*selfSignedCert},
		ServerName:   serverName,
		MinVersion:   tls.VersionTLS13,
		RootCAs:      pool,
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return selfSignedCert, nil
		},
	}
	creds := credentials.NewTLS(tlsConfig)

	grpcURL := config.GravityURL
	if strings.HasPrefix(grpcURL, "grpc://") {
		grpcURL = strings.Replace(grpcURL, "grpc://", "", 1)
	}

	conn, err := grpc.NewClient(grpcURL, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC client: %w", err)
	}
	defer conn.Close()

	client := pb.NewGravitySessionServiceClient(conn)

	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	response, err := client.Identify(callCtx, &pb.IdentifyRequest{
		InstanceId: config.InstanceID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to identify: %w", err)
	}

	return response, nil
}
