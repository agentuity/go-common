package gravity

import (
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	pb "github.com/agentuity/go-common/gravity/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Identify performs a one-shot authentication to retrieve the org ID.
// It connects using a self-signed certificate generated from the provided ECDSA private key.
func Identify(ctx context.Context, gravityURL string, instanceID string, privateKey *ecdsa.PrivateKey) (*pb.IdentifyResponse, error) {
	serverName, err := extractHostnameFromGravityURL(gravityURL)
	if err != nil {
		return nil, fmt.Errorf("failed to extract serverName from URL: %w", err)
	}

	selfSignedCert, err := createSelfSignedTLSConfig(privateKey, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to create self-signed certificate: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{*selfSignedCert},
		InsecureSkipVerify: true,
		ServerName:         serverName,
		MinVersion:         tls.VersionTLS13,
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return selfSignedCert, nil
		},
	}
	creds := credentials.NewTLS(tlsConfig)

	grpcURL := gravityURL
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
		InstanceId: instanceID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to identify: %w", err)
	}

	return response, nil
}
