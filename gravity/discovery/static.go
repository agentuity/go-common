package discovery

import "context"

// StaticDiscoverer returns a fixed set of peer addresses.
// Useful for development, testing, and environments with known IPs.
type StaticDiscoverer struct {
	Peers []string
}

// Discover returns the configured peer addresses.
func (s *StaticDiscoverer) Discover(_ context.Context) ([]string, error) {
	if s.Peers == nil {
		return nil, nil
	}
	result := make([]string, len(s.Peers))
	copy(result, s.Peers)
	return result, nil
}

// Name returns "static".
func (s *StaticDiscoverer) Name() string {
	return "static"
}
