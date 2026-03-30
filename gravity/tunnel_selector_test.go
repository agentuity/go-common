package gravity

import (
	"encoding/binary"
	"sync"
	"testing"
	"time"
)

func makeIPv6TCPPacket(srcIP, dstIP [16]byte, srcPort, dstPort uint16) []byte {
	packet := make([]byte, 60) // IPv6 header (40) + TCP header (20)
	packet[0] = 0x60           // Version 6
	packet[6] = 6              // Next header: TCP
	copy(packet[8:24], srcIP[:])
	copy(packet[24:40], dstIP[:])
	binary.BigEndian.PutUint16(packet[40:42], srcPort)
	binary.BigEndian.PutUint16(packet[42:44], dstPort)
	return packet
}

func makeIPv6UDPPacket(srcIP, dstIP [16]byte, srcPort, dstPort uint16) []byte {
	packet := make([]byte, 48)
	packet[0] = 0x60
	packet[6] = 17 // UDP
	copy(packet[8:24], srcIP[:])
	copy(packet[24:40], dstIP[:])
	binary.BigEndian.PutUint16(packet[40:42], srcPort)
	binary.BigEndian.PutUint16(packet[42:44], dstPort)
	return packet
}

func makeEndpoint(url string, healthy bool) *GravityEndpoint {
	ep := &GravityEndpoint{URL: url}
	ep.healthy.Store(healthy)
	ep.lastHeartbeat.Store(time.Now().Unix())
	return ep
}

func TestExtractFlowKey(t *testing.T) {
	src := [16]byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	dst := [16]byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2}
	packet := makeIPv6TCPPacket(src, dst, 12345, 443)

	key := ExtractFlowKey(packet)

	if key.SrcIP != src {
		t.Fatalf("src IP mismatch: got=%v want=%v", key.SrcIP, src)
	}
	if key.DstIP != dst {
		t.Fatalf("dst IP mismatch: got=%v want=%v", key.DstIP, dst)
	}
	if key.SrcPort != 12345 {
		t.Fatalf("src port mismatch: got=%d want=%d", key.SrcPort, 12345)
	}
	if key.DstPort != 443 {
		t.Fatalf("dst port mismatch: got=%d want=%d", key.DstPort, 443)
	}
	if key.Proto != 6 {
		t.Fatalf("proto mismatch: got=%d want=%d", key.Proto, 6)
	}
}

func TestExtractFlowKey_UDP(t *testing.T) {
	src := [16]byte{1}
	dst := [16]byte{2}
	packet := makeIPv6UDPPacket(src, dst, 5353, 53)

	key := ExtractFlowKey(packet)
	if key.Proto != 17 {
		t.Fatalf("proto mismatch: got=%d want=%d", key.Proto, 17)
	}
	if key.SrcPort != 5353 || key.DstPort != 53 {
		t.Fatalf("port mismatch: got=(%d,%d) want=(%d,%d)", key.SrcPort, key.DstPort, 5353, 53)
	}
}

func TestExtractFlowKey_ShortPacket(t *testing.T) {
	key := ExtractFlowKey(make([]byte, 10))
	if key != (FlowKey{}) {
		t.Fatalf("expected zero FlowKey for short packet, got=%+v", key)
	}
}

func TestEndpointSelector_SameFlowSameEndpoint(t *testing.T) {
	selector := NewEndpointSelector(200 * time.Millisecond)
	endpoints := []*GravityEndpoint{
		makeEndpoint("g1", true),
		makeEndpoint("g2", true),
	}

	packet := makeIPv6TCPPacket([16]byte{1}, [16]byte{2}, 1000, 2000)
	first := selector.Select(packet, endpoints)
	second := selector.Select(packet, endpoints)

	if first == nil || second == nil {
		t.Fatal("expected non-nil endpoint selections")
	}
	if first != second {
		t.Fatalf("expected same endpoint within TTL, got %s then %s", first.URL, second.URL)
	}
}

func TestEndpointSelector_ExpiredBinding(t *testing.T) {
	selector := NewEndpointSelector(20 * time.Millisecond)
	endpoints := []*GravityEndpoint{
		makeEndpoint("g1", true),
		makeEndpoint("g2", true),
	}

	packet := makeIPv6TCPPacket([16]byte{1}, [16]byte{3}, 1111, 2222)
	first := selector.Select(packet, endpoints)
	time.Sleep(30 * time.Millisecond)
	second := selector.Select(packet, endpoints)

	if first == nil || second == nil {
		t.Fatal("expected non-nil endpoint selections")
	}
	if first == second {
		t.Fatalf("expected endpoint rebinding after TTL expiry, got same endpoint %s", first.URL)
	}
}

func TestEndpointSelector_UnhealthySkipped(t *testing.T) {
	selector := NewEndpointSelector(time.Second)
	bad := makeEndpoint("bad", false)
	good := makeEndpoint("good", true)

	packet := makeIPv6TCPPacket([16]byte{4}, [16]byte{5}, 3000, 4000)
	ep := selector.Select(packet, []*GravityEndpoint{bad, good})
	if ep == nil {
		t.Fatal("expected healthy endpoint to be selected")
	}
	if ep != good {
		t.Fatalf("expected healthy endpoint 'good', got %s", ep.URL)
	}
}

func TestEndpointSelector_AllUnhealthy(t *testing.T) {
	selector := NewEndpointSelector(time.Second)
	packet := makeIPv6TCPPacket([16]byte{6}, [16]byte{7}, 5000, 6000)
	ep := selector.Select(packet, []*GravityEndpoint{
		makeEndpoint("g1", false),
		makeEndpoint("g2", false),
	})
	if ep != nil {
		t.Fatalf("expected nil when all endpoints unhealthy, got %s", ep.URL)
	}
}

func TestEndpointSelector_RoundRobin(t *testing.T) {
	selector := NewEndpointSelector(time.Second)
	e1 := makeEndpoint("g1", true)
	e2 := makeEndpoint("g2", true)
	endpoints := []*GravityEndpoint{e1, e2}

	seen := map[*GravityEndpoint]bool{}
	for i := 0; i < 8; i++ {
		packet := makeIPv6TCPPacket(
			[16]byte{byte(i + 1)},
			[16]byte{byte(i + 101)},
			uint16(1000+i),
			80,
		)
		ep := selector.Select(packet, endpoints)
		if ep == nil {
			t.Fatal("expected endpoint selection")
		}
		seen[ep] = true
	}

	if !seen[e1] || !seen[e2] {
		t.Fatalf("expected traffic distributed across both endpoints, seen=%d", len(seen))
	}
}

func TestEndpointSelector_ExpireBindings(t *testing.T) {
	selector := NewEndpointSelector(20 * time.Millisecond)
	endpoints := []*GravityEndpoint{makeEndpoint("g1", true), makeEndpoint("g2", true)}

	packet1 := makeIPv6TCPPacket([16]byte{1}, [16]byte{2}, 1111, 80)
	packet2 := makeIPv6TCPPacket([16]byte{3}, [16]byte{4}, 2222, 80)

	selector.Select(packet1, endpoints)
	selector.Select(packet2, endpoints)
	if selector.Len() != 2 {
		t.Fatalf("expected 2 bindings, got %d", selector.Len())
	}

	time.Sleep(25 * time.Millisecond)
	selector.ExpireBindings()

	if selector.Len() != 0 {
		t.Fatalf("expected all bindings to expire, got %d", selector.Len())
	}
}

func TestEndpointSelector_Concurrent(t *testing.T) {
	selector := NewEndpointSelector(500 * time.Millisecond)
	endpoints := []*GravityEndpoint{
		makeEndpoint("g1", true),
		makeEndpoint("g2", true),
		makeEndpoint("g3", true),
	}

	const goroutines = 32
	const iterations = 200

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				packet := makeIPv6TCPPacket(
					[16]byte{byte(gid + 1)},
					[16]byte{byte(i + 1)},
					uint16(1000+gid),
					uint16(2000+i%100),
				)
				ep := selector.Select(packet, endpoints)
				if ep == nil {
					t.Errorf("unexpected nil endpoint in concurrent select")
					return
				}
			}
		}(g)
	}
	wg.Wait()
}

func TestEndpointSelector_Len(t *testing.T) {
	selector := NewEndpointSelector(time.Second)
	endpoints := []*GravityEndpoint{makeEndpoint("g1", true)}

	if selector.Len() != 0 {
		t.Fatalf("expected zero bindings initially, got %d", selector.Len())
	}

	selector.Select(makeIPv6TCPPacket([16]byte{1}, [16]byte{2}, 1001, 80), endpoints)
	selector.Select(makeIPv6TCPPacket([16]byte{3}, [16]byte{4}, 1002, 80), endpoints)

	if selector.Len() != 2 {
		t.Fatalf("expected two bindings, got %d", selector.Len())
	}
}
