# Gravity Provider Interface

This package defines the minimal provider interface required by the gravity client.

Concrete implementations should be provided by consuming applications (e.g., hadron, other tools).

## Interface

The `Provider` interface defines methods for:
- Provisioning deployments
- Deprovisioning deployments  
- Setting metrics collectors

## Usage

Implement this interface in your application:

```go
type MyProvider struct {
    // your fields
}

func (p *MyProvider) Provision(ctx context.Context, req *ProvisionRequest) (*Resource, error) {
    // your implementation
}

func (p *MyProvider) Deprovision(ctx context.Context, deploymentID string, reason DeprovisionReason) error {
    // your implementation
}

func (p *MyProvider) SetMetricsCollector(collector MetricsCollector) {
    // your implementation
}
```
