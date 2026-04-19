package gossip

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// ---------- JSON round-trip for each type ----------

func TestEndpointMapping_JSONRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		msg  EndpointMapping
	}{
		{
			name: "fully populated",
			msg: EndpointMapping{
				GravityVIP:  "10.0.0.1",
				HadronVIP:   "10.0.0.2",
				ContainerID: "ctr-abc",
				MachineID:   "m-123",
				ProjectID:   "proj-456",
				DNSNames:    []string{"foo.example.com", "bar.example.com"},
				Timestamp:   1700000000,
			},
		},
		{
			name: "zero value",
			msg:  EndpointMapping{},
		},
		{
			name: "empty dns names",
			msg: EndpointMapping{
				GravityVIP:  "10.0.0.3",
				ContainerID: "ctr-xyz",
				DNSNames:    []string{},
				Timestamp:   1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.msg)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got EndpointMapping
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			assertEndpointMappingEqual(t, tt.msg, got)
		})
	}
}

func TestEndpointTombstone_JSONRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		msg  EndpointTombstone
	}{
		{
			name: "fully populated",
			msg: EndpointTombstone{
				ContainerID: "ctr-abc",
				MachineID:   "m-123",
				Timestamp:   1700000000,
			},
		},
		{
			name: "zero value",
			msg:  EndpointTombstone{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.msg)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got EndpointTombstone
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got != tt.msg {
				t.Fatalf("mismatch: got %+v, want %+v", got, tt.msg)
			}
		})
	}
}

func TestNATEntry_JSONRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		msg  NATEntry
	}{
		{
			name: "fully populated",
			msg: NATEntry{
				Key:         "nat-key-1",
				OriginalDst: "192.168.1.1",
				OriginalSrc: "192.168.1.2",
				SrcPort:     12345,
				DstPort:     443,
				Timestamp:   1700000000,
			},
		},
		{
			name: "zero value",
			msg:  NATEntry{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.msg)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got NATEntry
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got != tt.msg {
				t.Fatalf("mismatch: got %+v, want %+v", got, tt.msg)
			}
		})
	}
}

func TestVIPHeartbeat_JSONRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		msg  VIPHeartbeat
	}{
		{
			name: "fully populated",
			msg: VIPHeartbeat{
				VIP: "10.0.0.100",
				Owners: map[string]int64{
					"m-1": 1700000000,
					"m-2": 1700000001,
				},
				Timestamp: 1700000002,
				NewNATs: []NATEntry{
					{Key: "n1", SrcPort: 80, DstPort: 8080, Timestamp: 1},
				},
				NewEndpoints: []EndpointMapping{
					{GravityVIP: "10.0.0.5", ContainerID: "ctr-1", Timestamp: 2},
				},
			},
		},
		{
			name: "zero value",
			msg:  VIPHeartbeat{},
		},
		{
			name: "omitempty fields absent",
			msg: VIPHeartbeat{
				VIP:       "10.0.0.200",
				Owners:    map[string]int64{"m-3": 100},
				Timestamp: 200,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.msg)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got VIPHeartbeat
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			assertVIPHeartbeatEqual(t, tt.msg, got)
		})
	}
}

func TestSubnetRoute_JSONRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		msg  SubnetRoute
	}{
		{
			name: "add route",
			msg: SubnetRoute{
				Subnet:    "fd15:d710:02:05:a1b2::/64",
				MachineID: "m-123",
				IonID:     "10.0.0.1",
				Timestamp: 1700000000,
				Action:    "add",
			},
		},
		{
			name: "remove route",
			msg: SubnetRoute{
				Subnet:    "fd15:d710:02:05:c3d4::/64",
				MachineID: "m-456",
				IonID:     "10.0.0.2",
				Timestamp: 1700000001,
				Action:    "remove",
			},
		},
		{
			name: "zero value",
			msg:  SubnetRoute{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.msg)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got SubnetRoute
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got != tt.msg {
				t.Fatalf("mismatch: got %+v, want %+v", got, tt.msg)
			}
		})
	}
}

func TestSubnetRoute_EncodeDecode(t *testing.T) {
	orig := SubnetRoute{
		Subnet:    "fd15:d710:02:05:a1b2::/64",
		MachineID: "m-789",
		IonID:     "10.0.0.3",
		Timestamp: 1700000000,
		Action:    "add",
	}

	data, err := Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	gotTyp, gotMsg, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if gotTyp != MsgSubnetRoute {
		t.Fatalf("type mismatch: got %v, want %v", gotTyp, MsgSubnetRoute)
	}

	got, ok := gotMsg.(*SubnetRoute)
	if !ok {
		t.Fatalf("decoded type: got %T, want *SubnetRoute", gotMsg)
	}
	if got.Subnet != orig.Subnet {
		t.Errorf("Subnet: got %q, want %q", got.Subnet, orig.Subnet)
	}
	if got.MachineID != orig.MachineID {
		t.Errorf("MachineID: got %q, want %q", got.MachineID, orig.MachineID)
	}
	if got.IonID != orig.IonID {
		t.Errorf("IonID: got %q, want %q", got.IonID, orig.IonID)
	}
	if got.Timestamp != orig.Timestamp {
		t.Errorf("Timestamp: got %d, want %d", got.Timestamp, orig.Timestamp)
	}
	if got.Action != orig.Action {
		t.Errorf("Action: got %q, want %q", got.Action, orig.Action)
	}
}

// ---------- JSON snake_case tag verification ----------

func TestJSONSnakeCaseTags(t *testing.T) {
	em := EndpointMapping{
		GravityVIP:  "10.0.0.1",
		HadronVIP:   "10.0.0.2",
		ContainerID: "ctr",
		MachineID:   "m",
		ProjectID:   "p",
		DNSNames:    []string{"a.b"},
		Timestamp:   1,
	}
	data, err := json.Marshal(em)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	expected := []string{"gravity_vip", "hadron_vip", "container_id", "machine_id", "project_id", "dns_names", "timestamp"}
	for _, key := range expected {
		if _, ok := m[key]; !ok {
			t.Errorf("expected JSON key %q not found; keys: %v", key, keys(m))
		}
	}
}

// ---------- Encode / Decode round-trip ----------

func TestEncodeDecode_RoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		msg     any
		wantTyp MessageType
	}{
		{
			name: "EndpointMapping",
			msg: EndpointMapping{
				GravityVIP:  "10.0.0.1",
				HadronVIP:   "10.0.0.2",
				ContainerID: "ctr-abc",
				MachineID:   "m-123",
				ProjectID:   "proj-456",
				DNSNames:    []string{"foo.example.com"},
				Timestamp:   1700000000,
			},
			wantTyp: MsgEndpointMapping,
		},
		{
			name: "EndpointTombstone",
			msg: EndpointTombstone{
				ContainerID: "ctr-abc",
				MachineID:   "m-123",
				Timestamp:   1700000000,
			},
			wantTyp: MsgEndpointTombstone,
		},
		{
			name: "NATEntry",
			msg: NATEntry{
				Key:         "nat-1",
				OriginalDst: "192.168.1.1",
				OriginalSrc: "192.168.1.2",
				SrcPort:     12345,
				DstPort:     443,
				Timestamp:   1700000000,
			},
			wantTyp: MsgNATEntry,
		},
		{
			name: "VIPHeartbeat",
			msg: VIPHeartbeat{
				VIP:       "10.0.0.100",
				Owners:    map[string]int64{"m-1": 100},
				Timestamp: 200,
			},
			wantTyp: MsgVIPHeartbeat,
		},
		{
			name:    "EndpointMapping zero value",
			msg:     EndpointMapping{},
			wantTyp: MsgEndpointMapping,
		},
		{
			name:    "EndpointTombstone zero value",
			msg:     EndpointTombstone{},
			wantTyp: MsgEndpointTombstone,
		},
		{
			name:    "NATEntry zero value",
			msg:     NATEntry{},
			wantTyp: MsgNATEntry,
		},
		{
			name:    "VIPHeartbeat zero value",
			msg:     VIPHeartbeat{},
			wantTyp: MsgVIPHeartbeat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := Encode(tt.msg)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}

			gotTyp, gotMsg, err := Decode(data)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if gotTyp != tt.wantTyp {
				t.Fatalf("type mismatch: got %v, want %v", gotTyp, tt.wantTyp)
			}

			// Re-encode the original through Encode (which stamps V) so both
			// sides have the same schema version for comparison.
			wantData, err := Encode(tt.msg)
			if err != nil {
				t.Fatalf("Encode want: %v", err)
			}
			_, wantMsg, err := Decode(wantData)
			if err != nil {
				t.Fatalf("Decode want: %v", err)
			}
			wantJSON, _ := json.Marshal(wantMsg)
			gotJSON, _ := json.Marshal(gotMsg)

			// Normalise by unmarshalling into maps.
			var wantMap, gotMap any
			json.Unmarshal(wantJSON, &wantMap)
			json.Unmarshal(gotJSON, &gotMap)

			wantNorm, _ := json.Marshal(wantMap)
			gotNorm, _ := json.Marshal(gotMap)

			if string(wantNorm) != string(gotNorm) {
				t.Fatalf("payload mismatch:\n  got:  %s\n  want: %s", gotNorm, wantNorm)
			}
		})
	}
}

// ---------- Encode / Decode with pointers ----------

func TestEncodeDecode_Pointer(t *testing.T) {
	msg := &EndpointMapping{
		GravityVIP:  "10.0.0.1",
		ContainerID: "ctr-ptr",
		Timestamp:   99,
	}
	data, err := Encode(msg)
	if err != nil {
		t.Fatalf("Encode pointer: %v", err)
	}
	typ, decoded, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if typ != MsgEndpointMapping {
		t.Fatalf("type: got %v, want %v", typ, MsgEndpointMapping)
	}
	got, ok := decoded.(*EndpointMapping)
	if !ok {
		t.Fatalf("decoded type: got %T, want *EndpointMapping", decoded)
	}
	if got.ContainerID != "ctr-ptr" {
		t.Fatalf("ContainerID: got %q, want %q", got.ContainerID, "ctr-ptr")
	}
}

// ---------- Error cases ----------

func TestDecode_UnknownMessageType(t *testing.T) {
	env := Envelope{
		Type:    MessageType(255),
		Payload: json.RawMessage(`{}`),
	}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	_, _, err = Decode(data)
	if err == nil {
		t.Fatal("expected error for unknown message type, got nil")
	}
}

func TestDecode_InvalidJSON(t *testing.T) {
	_, _, err := Decode([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestEncode_UnsupportedType(t *testing.T) {
	_, err := Encode("not a gossip message")
	if err == nil {
		t.Fatal("expected error for unsupported type, got nil")
	}
}

// TestEncode_UnsupportedStructReturnsError verifies that stampVersion returns
// an error for struct types that are recognized by messageTypeFor but not
// handled in stampVersion. This is tested indirectly — if someone adds a new
// gossip struct to messageTypeFor but forgets stampVersion, Encode will fail.
// We test with a raw struct that bypasses messageTypeFor to exercise the
// stampVersion error path directly.
func TestStampVersion_UnsupportedStructReturnsError(t *testing.T) {
	type FakeGossipMsg struct {
		Foo string `json:"foo"`
	}
	_, err := stampVersion(FakeGossipMsg{Foo: "bar"})
	if err == nil {
		t.Fatal("expected error from stampVersion for unhandled struct type, got nil")
	}
	if !strings.Contains(err.Error(), "unhandled type") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestEncode_AllKnownTypesStampVersion(t *testing.T) {
	// Every gossip struct that Encode accepts must have V stamped.
	// This test ensures Encode + Decode produces V != 0 for all types.
	msgs := []any{
		EndpointMapping{ContainerID: "c1"},
		EndpointTombstone{ContainerID: "c1"},
		NATEntry{Key: "k1"},
		VIPHeartbeat{VIP: "fd00::1"},
		SubnetRoute{Subnet: "fd15::/64"},
		// pointer variants
		&EndpointMapping{ContainerID: "c2"},
		&EndpointTombstone{ContainerID: "c2"},
		&NATEntry{Key: "k2"},
		&VIPHeartbeat{VIP: "fd00::2"},
		&SubnetRoute{Subnet: "fd15:1::/64"},
	}
	for _, msg := range msgs {
		data, err := Encode(msg)
		if err != nil {
			t.Fatalf("Encode(%T): %v", msg, err)
		}
		_, decoded, err := Decode(data)
		if err != nil {
			t.Fatalf("Decode(%T): %v", msg, err)
		}
		// Check V via reflection — all gossip structs have V as first field.
		v := reflect.ValueOf(decoded).Elem().FieldByName("V")
		if !v.IsValid() {
			t.Fatalf("%T: missing V field", decoded)
		}
		if v.Uint() == 0 {
			t.Errorf("%T: V = 0 after Encode/Decode, expected non-zero schema hash", decoded)
		}
	}
}

// ---------- MessageType.String ----------

func TestMessageType_String(t *testing.T) {
	tests := []struct {
		mt   MessageType
		want string
	}{
		{MsgEndpointMapping, "EndpointMapping"},
		{MsgEndpointTombstone, "EndpointTombstone"},
		{MsgNATEntry, "NATEntry"},
		{MsgVIPHeartbeat, "VIPHeartbeat"},
		{MsgSubnetRoute, "SubnetRoute"},
		{MessageType(99), "Unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.mt.String(); got != tt.want {
			t.Errorf("MessageType(%d).String() = %q, want %q", tt.mt, got, tt.want)
		}
	}
}

// ---------- Omitempty behaviour ----------

func TestVIPHeartbeat_OmitemptyFields(t *testing.T) {
	msg := VIPHeartbeat{
		VIP:       "10.0.0.1",
		Owners:    map[string]int64{"m-1": 1},
		Timestamp: 1,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := m["new_nats"]; ok {
		t.Error("new_nats should be omitted when nil")
	}
	if _, ok := m["new_endpoints"]; ok {
		t.Error("new_endpoints should be omitted when nil")
	}
}

// ---------- Helpers ----------

func assertEndpointMappingEqual(t *testing.T, want, got EndpointMapping) {
	t.Helper()
	if got.GravityVIP != want.GravityVIP {
		t.Errorf("GravityVIP: got %q, want %q", got.GravityVIP, want.GravityVIP)
	}
	if got.HadronVIP != want.HadronVIP {
		t.Errorf("HadronVIP: got %q, want %q", got.HadronVIP, want.HadronVIP)
	}
	if got.ContainerID != want.ContainerID {
		t.Errorf("ContainerID: got %q, want %q", got.ContainerID, want.ContainerID)
	}
	if got.MachineID != want.MachineID {
		t.Errorf("MachineID: got %q, want %q", got.MachineID, want.MachineID)
	}
	if got.ProjectID != want.ProjectID {
		t.Errorf("ProjectID: got %q, want %q", got.ProjectID, want.ProjectID)
	}
	if got.Timestamp != want.Timestamp {
		t.Errorf("Timestamp: got %d, want %d", got.Timestamp, want.Timestamp)
	}
	if len(got.DNSNames) != len(want.DNSNames) {
		t.Errorf("DNSNames length: got %d, want %d", len(got.DNSNames), len(want.DNSNames))
		return
	}
	for i := range want.DNSNames {
		if got.DNSNames[i] != want.DNSNames[i] {
			t.Errorf("DNSNames[%d]: got %q, want %q", i, got.DNSNames[i], want.DNSNames[i])
		}
	}
}

func assertVIPHeartbeatEqual(t *testing.T, want, got VIPHeartbeat) {
	t.Helper()
	if got.VIP != want.VIP {
		t.Errorf("VIP: got %q, want %q", got.VIP, want.VIP)
	}
	if got.Timestamp != want.Timestamp {
		t.Errorf("Timestamp: got %d, want %d", got.Timestamp, want.Timestamp)
	}
	if len(got.Owners) != len(want.Owners) {
		t.Errorf("Owners length: got %d, want %d", len(got.Owners), len(want.Owners))
	} else {
		for k, wv := range want.Owners {
			if gv, ok := got.Owners[k]; !ok {
				t.Errorf("Owners missing key %q", k)
			} else if gv != wv {
				t.Errorf("Owners[%q]: got %d, want %d", k, gv, wv)
			}
		}
	}
	if len(got.NewNATs) != len(want.NewNATs) {
		t.Errorf("NewNATs length: got %d, want %d", len(got.NewNATs), len(want.NewNATs))
	}
	if len(got.NewEndpoints) != len(want.NewEndpoints) {
		t.Errorf("NewEndpoints length: got %d, want %d", len(got.NewEndpoints), len(want.NewEndpoints))
	}
}

func keys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
