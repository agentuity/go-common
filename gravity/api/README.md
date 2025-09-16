# Gravity API Utilities

This package provides API helper utilities used by the gravity client.

Currently contains functionality for:
- Public key retrieval from API endpoints
- HTTP request verification

## Usage

```go
import "github.com/agentuity/go-common/gravity/api"

// Get public key from API endpoint
publicKey, err := api.GetPublicKey(ctx, logger, apiURL)
if err != nil {
    return err
}
```
