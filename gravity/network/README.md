# Gravity Network Interface

This package defines the minimal network interface required by the gravity client for network device operations.

Concrete implementations should be provided by consuming applications.

## Interface

The `NetworkInterface` provides methods for:
- Reading packets from the network device
- Writing packets to the network device
- Managing the network interface lifecycle

## Usage

Implement this interface in your application:

```go
type MyNetworkInterface struct {
    // your fields
}

func (t *MyNetworkInterface) Read() ([]byte, error) {
    // your implementation
}

func (t *MyNetworkInterface) Write(packet []byte) error {
    // your implementation
}
```
