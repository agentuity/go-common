package dns

import (
	"context"
	"encoding/json"
	"fmt"

	pb "github.com/agentuity/go-common/dns/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type grpcTransport struct {
	client pb.DNSServiceClient
}

var _ transport = (*grpcTransport)(nil)

func (t *grpcTransport) Publish(ctx context.Context, action DNSAction) ([]byte, error) {
	switch a := action.(type) {
	case *DNSAddAction:
		resp, err := t.client.Add(ctx, a.ToProto())
		if err != nil {
			return nil, err
		}
		record, err := FromProtoAddResponse(resp)
		response := NewDNSResponse(a, record, err)
		return json.Marshal(response)

	case *DNSDeleteAction:
		resp, err := t.client.Delete(ctx, a.ToProto())
		if err != nil {
			return nil, err
		}
		deleteErr := FromProtoDeleteResponse(resp)
		var emptyString string
		response := NewDNSResponse(a, &emptyString, deleteErr)
		return json.Marshal(response)

	case *DNSCertAction:
		resp, err := t.client.RequestCert(ctx, a.ToProto())
		if err != nil {
			return nil, err
		}
		cert, err := FromProtoCertResponse(resp)
		response := NewDNSResponse(a, cert, err)
		return json.Marshal(response)

	default:
		return nil, fmt.Errorf("unsupported action type: %T", action)
	}
}

func (t *grpcTransport) PublishAsync(ctx context.Context, action DNSAction) error {
	switch a := action.(type) {
	case *DNSAddAction:
		_, err := t.client.Add(ctx, a.ToProto())
		return err

	case *DNSDeleteAction:
		_, err := t.client.Delete(ctx, a.ToProto())
		return err

	case *DNSCertAction:
		_, err := t.client.RequestCert(ctx, a.ToProto())
		return err

	default:
		return fmt.Errorf("unsupported action type: %T", action)
	}
}

// NewDNSServiceClient creates a new gRPC DNS service client.
// The caller is responsible for closing the returned connection.
func NewDNSServiceClient(ctx context.Context, address string, opts ...grpc.DialOption) (pb.DNSServiceClient, *grpc.ClientConn, error) {
	if len(opts) == 0 {
		opts = []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	}

	conn, err := grpc.NewClient(address, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create gRPC client: %w", err)
	}

	client := pb.NewDNSServiceClient(conn)
	return client, conn, nil
}
