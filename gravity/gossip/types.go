package gossip

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
)

// Schema hashes — computed at init() from each struct's field names + types.
// When a field is added, removed, or changes type, the hash changes
// automatically. Receivers compare hashes to detect version mismatches
// and apply defensive merge logic during rolling upgrades.
//
// Old peers that predate this field will produce V=0 (the JSON field is
// omitted and Go defaults the uint8 to zero on unmarshal).
var (
	EndpointMappingV   uint8
	EndpointTombstoneV uint8
	NATEntryV          uint8
	VIPHeartbeatV      uint8
	SubnetRouteV       uint8
)

func init() {
	EndpointMappingV = structHash(reflect.TypeOf(EndpointMapping{}))
	EndpointTombstoneV = structHash(reflect.TypeOf(EndpointTombstone{}))
	NATEntryV = structHash(reflect.TypeOf(NATEntry{}))
	VIPHeartbeatV = structHash(reflect.TypeOf(VIPHeartbeat{}))
	SubnetRouteV = structHash(reflect.TypeOf(SubnetRoute{}))
}

// structHash computes a stable uint8 fingerprint from the struct's exported
// field names and types (excluding the V field itself). Any structural change
// — adding, removing, or retyping a field — produces a different hash.
func structHash(t reflect.Type) uint8 {
	h := sha256.New()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() || f.Name == "V" {
			continue // skip unexported fields and the version field itself
		}
		fmt.Fprintf(h, "%s:%s;", f.Name, f.Type.String())
	}
	sum := h.Sum(nil)
	// Use first byte; 0 is reserved for legacy so map 0→1.
	v := sum[0]
	if v == 0 {
		v = 1
	}
	return v
}

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
//
// VERSIONING: The V field is a structural hash computed at init() from the
// field names and types of this struct. Any change to the struct layout
// (adding, removing, or retyping a field) automatically changes V.
// Receivers use V to detect schema mismatches during rolling upgrades
// and apply defensive merge logic to avoid silent data loss.
// Old peers that predate versioning will send V=0.
type EndpointMapping struct {
	V           uint8    `json:"v,omitempty"` // auto-computed structural hash; 0 = pre-versioning legacy
	GravityVIP  string   `json:"gravity_vip"`
	HadronVIP   string   `json:"hadron_vip"`
	ContainerID string   `json:"container_id"`
	MachineID   string   `json:"machine_id"`
	ProjectID   string   `json:"project_id"`
	DNSNames    []string `json:"dns_names"`
	Timestamp   int64    `json:"timestamp"`
	SNIHostname string   `json:"sni_hostname,omitempty"` // d-hash for deployments, sandbox hostname for sandboxes
}

// EndpointTombstone marks an endpoint as removed so peers can clean up stale
// mappings.
//
// VERSIONING: V is auto-computed. See EndpointMapping for details.
type EndpointTombstone struct {
	V           uint8  `json:"v,omitempty"` // auto-computed structural hash; 0 = pre-versioning legacy
	ContainerID string `json:"container_id"`
	MachineID   string `json:"machine_id"`
	Timestamp   int64  `json:"timestamp"`
}

// NATEntry represents a single NAT translation entry used to route packets
// between Gravity instances.
//
// VERSIONING: V is auto-computed. See EndpointMapping for details.
type NATEntry struct {
	V           uint8  `json:"v,omitempty"` // auto-computed structural hash; 0 = pre-versioning legacy
	Key         string `json:"key"`
	OriginalDst string `json:"original_dst"`
	OriginalSrc string `json:"original_src"`
	SrcPort     uint16 `json:"src_port"`
	DstPort     uint16 `json:"dst_port"`
	Timestamp   int64  `json:"timestamp"`
}

// VIPHeartbeat advertises ownership and recent changes for a VIP.
//
// VERSIONING: V is auto-computed. See EndpointMapping for details.
type VIPHeartbeat struct {
	V            uint8             `json:"v,omitempty"` // auto-computed structural hash; 0 = pre-versioning legacy
	VIP          string            `json:"vip"`
	Owners       map[string]int64  `json:"owners"`
	Timestamp    int64             `json:"timestamp"`
	NewNATs      []NATEntry        `json:"new_nats,omitempty"`
	NewEndpoints []EndpointMapping `json:"new_endpoints,omitempty"`
}

// SubnetRoute advertises ownership of a /64 subnet by a machine connected
// to this ion instance. Used to build cross-ion forwarding tables.
//
// VERSIONING: V is auto-computed. See EndpointMapping for details.
type SubnetRoute struct {
	V         uint8  `json:"v,omitempty"` // auto-computed structural hash; 0 = pre-versioning legacy
	Subnet    string `json:"subnet"`      // /64 prefix, e.g. "fd15:d710:02:05:a1b2::/64"
	MachineID string `json:"machine_id"`  // Machine that owns this subnet
	IonID     string `json:"ion_id"`      // Memberlist address of the ion that owns this subnet
	Timestamp int64  `json:"timestamp"`   // Unix timestamp
	Action    string `json:"action"`      // "add" or "remove"
}

// Envelope is the wire format for gossip messages. It wraps a MessageType
// together with a JSON-encoded payload so receivers can demux without
// knowing the concrete type up front. Schema versioning is per-struct
// (each payload carries its own V field), not per-envelope.
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

// stampVersion sets the V field on a gossip struct to its auto-computed
// structural hash before serialization. Returns the (possibly copied) value
// for marshaling and an error if the type is unhandled. This prevents new
// gossip structs from being added to messageTypeFor without also being
// handled here — Encode will return an error instead of silently sending
// unversioned messages.
func stampVersion(msg any) (any, error) {
	switch m := msg.(type) {
	case *EndpointMapping:
		m.V = EndpointMappingV
		return m, nil
	case EndpointMapping:
		m.V = EndpointMappingV
		return m, nil
	case *EndpointTombstone:
		m.V = EndpointTombstoneV
		return m, nil
	case EndpointTombstone:
		m.V = EndpointTombstoneV
		return m, nil
	case *NATEntry:
		m.V = NATEntryV
		return m, nil
	case NATEntry:
		m.V = NATEntryV
		return m, nil
	case *VIPHeartbeat:
		m.V = VIPHeartbeatV
		for i := range m.NewEndpoints {
			m.NewEndpoints[i].V = EndpointMappingV
		}
		for i := range m.NewNATs {
			m.NewNATs[i].V = NATEntryV
		}
		return m, nil
	case VIPHeartbeat:
		m.V = VIPHeartbeatV
		for i := range m.NewEndpoints {
			m.NewEndpoints[i].V = EndpointMappingV
		}
		for i := range m.NewNATs {
			m.NewNATs[i].V = NATEntryV
		}
		return m, nil
	case *SubnetRoute:
		m.V = SubnetRouteV
		return m, nil
	case SubnetRoute:
		m.V = SubnetRouteV
		return m, nil
	default:
		return nil, fmt.Errorf("gossip: stampVersion: unhandled type %T — add it to stampVersion when adding new gossip structs", msg)
	}
}

// Encode serialises msg into a JSON-encoded Envelope ready for the wire.
// V is auto-stamped on the payload struct before marshaling. Returns an
// error if the type is not handled by stampVersion, preventing new gossip
// structs from silently sending unversioned messages.
func Encode(msg any) ([]byte, error) {
	mt, err := messageTypeFor(msg)
	if err != nil {
		return nil, err
	}
	stamped, err := stampVersion(msg)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(stamped)
	if err != nil {
		return nil, fmt.Errorf("gossip: marshal payload: %w", err)
	}
	return json.Marshal(Envelope{Type: mt, Payload: payload})
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
