package discovery

import "context"

// PeerDiscoverer discovers memberlist peer addresses for Gravity cluster formation.
type PeerDiscoverer interface {
	// Discover returns peer addresses (host:port or just host).
	Discover(ctx context.Context) ([]string, error)
	// Name returns a human-readable discoverer name for logging.
	Name() string
}
