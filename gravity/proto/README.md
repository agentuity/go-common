# Gravity Protocol Buffers

This directory contains the Protocol Buffer definitions for the Gravity control and tunnel protocols.

## Module Path

Generated Go code uses the import path: `github.com/agentuity/go-common/gravity/proto`

## Files

### Active Services

- `gravity_session.proto` - **NEW** Session-based authentication service (replaces Provision + EstablishTunnel)
- `gravity_session.pb.go` - Generated Go message types
- `gravity_session_grpc.pb.go` - Generated gRPC service stubs

### Legacy Services (Deprecated)

- `gravity.proto` - Legacy protocol definitions (deprecated, use GravitySessionService instead)
- `gravity.pb.go` - Generated Go message types
- `gravity_grpc.pb.go` - Generated gRPC service stubs

## Services

### GravitySessionService (New)

Uses self-signed client certificates authenticated against the org's registered public key.
Replaces the legacy Provision + EstablishTunnel two-step flow with a single-step session establishment.

- `EstablishSession` - Bidirectional control stream for session lifecycle
- `StreamPackets` - Bidirectional packet data streaming
- `GetDeploymentMetadata` - Retrieve deployment metadata
- `GetSandboxMetadata` - Retrieve sandbox metadata

### GravityControl + GravityTunnel (Deprecated)

Legacy services that require a two-step Provision + Connect flow with pre-created machine tokens.
These services are deprecated and will be removed in a future version.

## Regenerating Code

Use the same toolchain as the original graviton project:

```bash
# Install dependencies
make install-tools

# Generate code (from go-common root)
make gen-protoc
```

The generated code should be committed to version control.
