package gravity

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	pb "github.com/agentuity/go-common/gravity/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

type ProvisionRequest struct {
	Context          context.Context
	GravityURL       string
	InstanceID       string
	Region           string
	AvailabilityZone string
	Provider         string
	PrivateIP        string
	Token            string
	PublicKey        string
	Hostname         string
	ErrorMessage     string
	Ephemeral        bool
	Capabilities     *pb.ClientCapabilities
}

// Provision calls the Provision gRPC method with TLS verification disabled
// This is a standalone method that should be used for initial provisioning
func Provision(request ProvisionRequest) (*pb.ProvisionResponse, error) {
	// Parse gRPC URL
	serverName, err := extractHostnameFromGravityURL(request.GravityURL)
	if err != nil {
		return nil, fmt.Errorf("failed to extract serverName from URL: %w", err)
	}

	// Create TLS config with verification disabled for initial provisioning
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, // Skip verification since we don't have certs yet
		ServerName:         serverName,
		MinVersion:         tls.VersionTLS13,
	}
	creds := credentials.NewTLS(tlsConfig)

	// Convert URL format (ws:// or wss:// → grpc://)
	grpcURL := request.GravityURL
	if strings.HasPrefix(grpcURL, "grpc://") {
		grpcURL = strings.Replace(grpcURL, "grpc://", "", 1)
	}

	// Create gRPC connection
	conn, err := grpc.NewClient(grpcURL, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC client for connecting to gravity: %w", err)
	}
	defer conn.Close()

	// Create control client
	client := pb.NewGravityControlClient(conn)

	// Create provision request
	req := &pb.ProvisionRequest{
		InstanceId:       request.InstanceID,
		Region:           request.Region,
		AvailabilityZone: request.AvailabilityZone,
		Provider:         request.Provider,
		PrivateIpv4:      request.PrivateIP,
		PublicKey:        request.PublicKey,
		Hostname:         request.Hostname,
		ErrorMessage:     request.ErrorMessage,
		Ephemeral:        request.Ephemeral,
		Capabilities:     request.Capabilities,
	}

	// Call Provision with a timeout and JWT token
	pctx, cancel := context.WithTimeout(request.Context, 30*time.Second)
	defer cancel()

	// Add JWT token as Authorization header in gRPC metadata
	if request.Token != "" {
		md := metadata.Pairs("authorization", "Bearer "+request.Token)
		pctx = metadata.NewOutgoingContext(pctx, md)
	}

	response, err := client.Provision(pctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to provision: %w", err)
	}

	return response, nil
}
