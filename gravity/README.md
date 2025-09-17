# Gravity Client

Generic gRPC client for connecting to Gravity servers. This package was extracted from the hadron project to enable reuse across multiple tools and projects.

## Package Structure

- `gravity/` - Main gravity client implementation
- `gravity/proto/` - Protocol buffer definitions and generated code
- `gravity/provider/` - Provider interface definitions
- `gravity/network/` - Network interface definitions
- `gravity/api/` - API helper utilities

## Module Path Conventions

All packages use the base module path: `github.com/agentuity/go-common`

- Proto package: `github.com/agentuity/go-common/gravity/proto`
- Provider interfaces: `github.com/agentuity/go-common/gravity/provider`
- Network interfaces: `github.com/agentuity/go-common/gravity/network`
- API utilities: `github.com/agentuity/go-common/gravity/api`

## Usage

```go
import (
    "github.com/agentuity/go-common/gravity"
    "github.com/agentuity/go-common/gravity/provider"
    "github.com/agentuity/go-common/gravity/network"
)

// Create gravity client with your provider and network implementations
config := gravity.GravityConfig{
    Provider:     myProvider,
    NetworkInterface: myNetworkInterface,
    // ... other config
}

client, err := gravity.New(config)
if err != nil {
    return err
}

if err := client.Start(); err != nil {
    return err
}
```
