package gravity

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"
)

// FlowKey identifies a network flow by its 5-tuple.
// Uses fixed-size arrays for efficient map key comparison.
type FlowKey struct {
	SrcIP   [16]byte
	DstIP   [16]byte
	SrcPort uint16
	DstPort uint16
	Proto   uint8
}

// TunnelBinding tracks which endpoint a flow is pinned to.
type TunnelBinding struct {
	Endpoint *GravityEndpoint
	LastUsed time.Time
}

// EndpointSelector manages sticky flow-to-endpoint bindings.
type EndpointSelector struct {
	mu       sync.RWMutex
	bindings map[FlowKey]*TunnelBinding
	ttl      time.Duration
	rrIndex  atomic.Uint64
}

// NewEndpointSelector creates a selector with the given binding TTL.
func NewEndpointSelector(ttl time.Duration) *EndpointSelector {
	if ttl <= 0 {
		ttl = DefaultBindingTTL
	}
	return &EndpointSelector{
		bindings: make(map[FlowKey]*TunnelBinding),
		ttl:      ttl,
	}
}

// Select picks an endpoint for the given packet. It extracts the flow key
// from the IPv6 packet header and checks for an existing binding.
// If the binding exists and is within TTL, the same endpoint is returned.
// Otherwise, a new endpoint is selected via round-robin.
func (s *EndpointSelector) Select(packet []byte, endpoints []*GravityEndpoint) *GravityEndpoint {
	key := ExtractFlowKey(packet)
	now := time.Now()

	// Fast path: read existing binding.
	s.mu.RLock()
	binding, ok := s.bindings[key]
	s.mu.RUnlock()

	if ok && now.Sub(binding.LastUsed) < s.ttl && binding.Endpoint.IsHealthy() {
		// Update LastUsed under write lock to avoid races.
		s.mu.Lock()
		if current, ok := s.bindings[key]; ok && current.Endpoint == binding.Endpoint && now.Sub(current.LastUsed) < s.ttl && current.Endpoint.IsHealthy() {
			current.LastUsed = now
			s.mu.Unlock()
			return current.Endpoint
		}
		s.mu.Unlock()
	}

	// Slow path: pick new endpoint, store binding.
	ep := s.pickHealthy(endpoints)
	if ep == nil {
		return nil
	}

	s.mu.Lock()
	s.bindings[key] = &TunnelBinding{
		Endpoint: ep,
		LastUsed: now,
	}
	s.mu.Unlock()

	return ep
}

// ExtractFlowKey reads the 5-tuple directly from an IPv6 packet.
// Reads fixed offsets — no gopacket parsing overhead on hot path.
func ExtractFlowKey(packet []byte) FlowKey {
	var key FlowKey
	if len(packet) < 44 { // IPv6 header (40) + ports (4)
		return key
	}
	copy(key.SrcIP[:], packet[8:24])
	copy(key.DstIP[:], packet[24:40])

	nextHeader := packet[6]
	if nextHeader == 6 || nextHeader == 17 { // TCP or UDP
		key.SrcPort = binary.BigEndian.Uint16(packet[40:42])
		key.DstPort = binary.BigEndian.Uint16(packet[42:44])
		key.Proto = nextHeader
	}
	return key
}

// pickHealthy selects a healthy endpoint via round-robin.
func (s *EndpointSelector) pickHealthy(endpoints []*GravityEndpoint) *GravityEndpoint {
	n := len(endpoints)
	if n == 0 {
		return nil
	}

	start := int(s.rrIndex.Add(1))
	for i := 0; i < n; i++ {
		ep := endpoints[(start+i)%n]
		if ep.IsHealthy() {
			return ep
		}
	}
	return nil // all unhealthy
}

// ExpireBindings removes bindings older than the TTL.
// Called periodically from a background goroutine.
func (s *EndpointSelector) ExpireBindings() {
	now := time.Now()
	s.mu.Lock()
	for key, binding := range s.bindings {
		if now.Sub(binding.LastUsed) >= s.ttl {
			delete(s.bindings, key)
		}
	}
	s.mu.Unlock()
}

// Len returns the current number of active bindings.
func (s *EndpointSelector) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.bindings)
}
