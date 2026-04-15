package gossip

import (
	"encoding/json"
	"fmt"
)

// MessageType identifies the kind of gossip message on the wire.
type MessageType uint8

const (
	// MsgEndpointMapping carries a new or updated endpoint mapping.
	MsgEndpointMapping MessageType = iota + 1
	// MsgEndpointTombstone signals removal of an endpoint.
	MsgEndpointTombstone
	// MsgNATEntry carries a NAT translation entry.
	MsgNATEntry
	// MsgVIPHeartbeat carries a VIP ownership heartbeat.
	MsgVIPHeartbeat
	// MsgSubnetRoute carries a subnet ownership announcement.
	MsgSubnetRoute
)

// String returns a human-readable name for the message type.
func (m MessageType) String() string {
	switch m {
	case MsgEndpointMapping:
		return "EndpointMapping"
	case MsgEndpointTombstone:
		return "EndpointTombstone"
	case MsgNATEntry:
		return "NATEntry"
	case MsgVIPHeartbeat:
		return "VIPHeartbeat"
	case MsgSubnetRoute:
		return "SubnetRoute"
	default:
		return fmt.Sprintf("Unknown(%d)", m)
	}
}

// EndpointMapping describes the mapping between a container and its network
// endpoints within the Gravity mesh.
type EndpointMapping struct {
	GravityVIP  string   `json:"gravity_vip"`
	HadronVIP   string   `json:"hadron_vip"`
	ContainerID string   `json:"container_id"`
	MachineID   string   `json:"machine_id"`
	ProjectID   string   `json:"project_id"`
	DNSNames    []string `json:"dns_names"`
	Timestamp   int64    `json:"timestamp"`
}

// EndpointTombstone marks an endpoint as removed so peers can clean up stale
// mappings.
type EndpointTombstone struct {
	ContainerID string `json:"container_id"`
	MachineID   string `json:"machine_id"`
	Timestamp   int64  `json:"timestamp"`
}

// NATEntry represents a single NAT translation entry used to route packets
// between Gravity instances.
type NATEntry struct {
	Key         string `json:"key"`
	OriginalDst string `json:"original_dst"`
	OriginalSrc string `json:"original_src"`
	SrcPort     uint16 `json:"src_port"`
	DstPort     uint16 `json:"dst_port"`
	Timestamp   int64  `json:"timestamp"`
}

// VIPHeartbeat advertises ownership and recent changes for a VIP.
type VIPHeartbeat struct {
	VIP          string            `json:"vip"`
	Owners       map[string]int64  `json:"owners"`
	Timestamp    int64             `json:"timestamp"`
	NewNATs      []NATEntry        `json:"new_nats,omitempty"`
	NewEndpoints []EndpointMapping `json:"new_endpoints,omitempty"`
}

// SubnetRoute advertises ownership of a /64 subnet by a machine connected
// to this ion instance. Used to build cross-ion forwarding tables.
type SubnetRoute struct {
	Subnet    string `json:"subnet"`     // /64 prefix, e.g. "fd15:d710:02:05:a1b2::/64"
	MachineID string `json:"machine_id"` // Machine that owns this subnet
	IonID     string `json:"ion_id"`     // Memberlist address of the ion that owns this subnet
	Timestamp int64  `json:"timestamp"`  // Unix timestamp
	Action    string `json:"action"`     // "add" or "remove"
}

// Envelope is the wire format for gossip messages. It wraps a MessageType
// together with a JSON-encoded payload so receivers can demux without
// knowing the concrete type up front.
type Envelope struct {
	Type    MessageType     `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// messageTypeFor returns the MessageType for a known Go type.
func messageTypeFor(msg any) (MessageType, error) {
	switch msg.(type) {
	case EndpointMapping, *EndpointMapping:
		return MsgEndpointMapping, nil
	case EndpointTombstone, *EndpointTombstone:
		return MsgEndpointTombstone, nil
	case NATEntry, *NATEntry:
		return MsgNATEntry, nil
	case VIPHeartbeat, *VIPHeartbeat:
		return MsgVIPHeartbeat, nil
	case SubnetRoute, *SubnetRoute:
		return MsgSubnetRoute, nil
	default:
		return 0, fmt.Errorf("gossip: unsupported message type %T", msg)
	}
}

// Encode serialises msg into a JSON-encoded Envelope ready for the wire.
func Encode(msg any) ([]byte, error) {
	mt, err := messageTypeFor(msg)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("gossip: marshal payload: %w", err)
	}
	env := Envelope{
		Type:    mt,
		Payload: payload,
	}
	return json.Marshal(env)
}

// Decode deserialises a JSON-encoded Envelope and returns the MessageType
// together with the concrete Go value. The returned value is a pointer to the
// decoded struct (e.g. *EndpointMapping).
func Decode(data []byte) (MessageType, any, error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return 0, nil, fmt.Errorf("gossip: unmarshal envelope: %w", err)
	}

	var msg any
	switch env.Type {
	case MsgEndpointMapping:
		var v EndpointMapping
		msg = &v
	case MsgEndpointTombstone:
		var v EndpointTombstone
		msg = &v
	case MsgNATEntry:
		var v NATEntry
		msg = &v
	case MsgVIPHeartbeat:
		var v VIPHeartbeat
		msg = &v
	case MsgSubnetRoute:
		var v SubnetRoute
		msg = &v
	default:
		return 0, nil, fmt.Errorf("gossip: unknown message type %d", env.Type)
	}

	if err := json.Unmarshal(env.Payload, msg); err != nil {
		return 0, nil, fmt.Errorf("gossip: unmarshal payload for %s: %w", env.Type, err)
	}
	return env.Type, msg, nil
}
