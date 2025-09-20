package network

// NetworkInterface defines the minimal interface required by the gravity client for network operations.
// Concrete implementations should be provided by consuming applications.
type NetworkInterface interface {
	// RouteTraffic configures routing for the specified network ranges
	RouteTraffic(nets []string) error

	// UnrouteTraffic removes all routing configurations
	UnrouteTraffic() error

	// Read reads a packet from the network interface into the provided buffer
	Read(buffer []byte) (int, error)

	// Write writes a packet to the network interface
	Write(packet []byte) (int, error)

	// Running returns true if the network interface is currently running
	Running() bool

	// Start starts the network interface and calls the handler with each outbound packet.
	// The packet passed to the handler is NOT a copy, so it must be copied if used after the handler returns.
	Start(handler func(packet []byte))
}
