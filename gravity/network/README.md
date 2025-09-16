# Gravity Network Interface

This package defines the minimal network interface required by the gravity client for TUN device operations.

Concrete implementations should be provided by consuming applications.

## Interface

The `TUNInterface` provides methods for:
- Reading packets from the TUN device
- Writing packets to the TUN device
- Managing the TUN interface lifecycle

## Usage

Implement this interface in your application:

```go
type MyTUNInterface struct {
    // your fields
}

func (t *MyTUNInterface) Read() ([]byte, error) {
    // your implementation
}

func (t *MyTUNInterface) Write(packet []byte) error {
    // your implementation
}
```
