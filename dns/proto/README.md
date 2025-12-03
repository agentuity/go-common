# DNS Protocol Buffers

This directory contains the Protocol Buffer definitions for the DNS service.

## Module Path

Generated Go code uses the import path: `github.com/agentuity/go-common/dns/proto`

## Files

- `dns.proto` - DNS service definitions
- `dns.pb.go` - Generated Go message types
- `dns_grpc.pb.go` - Generated gRPC service stubs

## Service

The `DNSService` provides three RPC methods:
- `Add` - Add a DNS record
- `Delete` - Delete DNS records
- `RequestCert` - Request a certificate for a domain

## Regenerating Code

From the repository root:

```bash
make gen-protoc
```

Or manually from this directory:

```bash
protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    dns.proto
```

The generated code should be committed to version control.
