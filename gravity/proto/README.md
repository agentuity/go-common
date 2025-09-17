# Gravity Protocol Buffers

This directory contains the Protocol Buffer definitions for the Gravity control and tunnel protocols.

## Module Path

Generated Go code uses the import path: `github.com/agentuity/go-common/gravity/proto`

## Files

- `gravity.proto` - Main protocol definitions
- `gravity.pb.go` - Generated Go message types
- `gravity_grpc.pb.go` - Generated gRPC service stubs

## Regenerating Code

Use the same toolchain as the original graviton project:

```bash
# Install dependencies
make install-tools

# Generate code
make gen
```

The generated code should be committed to version control.
