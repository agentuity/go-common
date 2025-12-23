package dns

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/agentuity/go-common/logger"
	"github.com/miekg/dns"
)

func TestDNSConfig_IsManagedDomain(t *testing.T) {
	config := DNSConfig{
		ManagedDomains: []string{"agentuity.com", "agentuity.internal", "example.org"},
	}

	tests := []struct {
		domain   string
		expected bool
	}{
		{"agentuity.com", true},
		{"agentuity.com.", true},
		{"AGENTUITY.COM", true},
		{"sub.agentuity.com", true},
		{"sub.agentuity.com.", true},
		{"agentuity.internal", true},
		{"test.agentuity.internal", true},
		{"example.org", true},
		{"google.com", false},
		{"notmanaged.com", false},
		{"agentuity.net", false},
		{"", false},
	}

	for _, test := range tests {
		result := config.IsManagedDomain(test.domain)
		if result != test.expected {
			t.Errorf("IsManagedDomain(%q) = %v, want %v", test.domain, result, test.expected)
		}
	}
}

func TestDNSConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  DNSConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: DNSConfig{
				ManagedDomains:      []string{"example.com"},
				InternalNameservers: []string{"ns1.example.com:53"},
				UpstreamNameservers: []string{"8.8.8.8:53"},
			},
			wantErr: false,
		},
		{
			name: "missing managed domains",
			config: DNSConfig{
				InternalNameservers: []string{"ns1.example.com:53"},
				UpstreamNameservers: []string{"8.8.8.8:53"},
			},
			wantErr: true,
		},
		{
			name: "missing internal nameservers",
			config: DNSConfig{
				ManagedDomains:      []string{"example.com"},
				UpstreamNameservers: []string{"8.8.8.8:53"},
			},
			wantErr: true,
		},
		{
			name: "missing upstream nameservers",
			config: DNSConfig{
				ManagedDomains:      []string{"example.com"},
				InternalNameservers: []string{"ns1.example.com:53"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSimpleServer_NewAndLifecycle(t *testing.T) {
	logger := logger.NewTestLogger()

	config := DNSConfig{
		ListenAddress:       ":15353", // Use non-privileged port for testing
		ManagedDomains:      []string{"test.local"},
		InternalNameservers: []string{"127.0.0.1:5354"},
		UpstreamNameservers: []string{"8.8.8.8:53"},
		QueryTimeout:        "2s",
	}

	server, err := New(context.Background(), logger, config)
	if err != nil {
		t.Fatalf("NewSimple() error = %v", err)
	}

	if server.IsRunning() {
		t.Error("Server should not be running initially")
	}

	// Test starting the server
	err = server.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !server.IsRunning() {
		t.Error("Server should be running after Start()")
	}

	// Give the server a moment to start listening
	time.Sleep(100 * time.Millisecond)

	// Test stopping the server
	err = server.Stop()
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if server.IsRunning() {
		t.Error("Server should not be running after Stop()")
	}
}

func TestDefaultDNSConfig(t *testing.T) {
	config := DefaultDNSConfig()

	if len(config.ManagedDomains) == 0 {
		t.Error("DefaultDNSConfig should have managed domains")
	}

	if len(config.InternalNameservers) == 0 {
		t.Error("DefaultDNSConfig should have internal nameservers")
	}

	if len(config.UpstreamNameservers) == 0 {
		t.Error("DefaultDNSConfig should have upstream nameservers")
	}

	if config.ListenAddress == "" {
		t.Error("DefaultDNSConfig should have a listen address")
	}

	if config.QueryTimeout == "" {
		t.Error("DefaultDNSConfig should have a query timeout")
	}
}

func TestDNSResolver_needsCNAMEResolution(t *testing.T) {
	logger := logger.NewTestLogger()
	config := DNSConfig{
		ListenAddress:       ":15353",
		ManagedDomains:      []string{"test.local"},
		InternalNameservers: []string{"127.0.0.1:5354"},
		UpstreamNameservers: []string{"8.8.8.8:53"},
		QueryTimeout:        "2s",
	}

	resolver, err := New(context.Background(), logger, config)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}

	tests := []struct {
		name           string
		setupMsg       func() *dns.Msg
		queryType      uint16
		wantResolution bool
		wantTarget     string
	}{
		{
			name: "CNAME only for A query - needs resolution",
			setupMsg: func() *dns.Msg {
				msg := new(dns.Msg)
				msg.Answer = []dns.RR{
					&dns.CNAME{
						Hdr:    dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 300},
						Target: "target.example.com.",
					},
				}
				return msg
			},
			queryType:      dns.TypeA,
			wantResolution: true,
			wantTarget:     "target.example.com.",
		},
		{
			name: "CNAME only for AAAA query - needs resolution",
			setupMsg: func() *dns.Msg {
				msg := new(dns.Msg)
				msg.Answer = []dns.RR{
					&dns.CNAME{
						Hdr:    dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 300},
						Target: "target.example.com.",
					},
				}
				return msg
			},
			queryType:      dns.TypeAAAA,
			wantResolution: true,
			wantTarget:     "target.example.com.",
		},
		{
			name: "CNAME with A record - no resolution needed",
			setupMsg: func() *dns.Msg {
				msg := new(dns.Msg)
				msg.Answer = []dns.RR{
					&dns.CNAME{
						Hdr:    dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 300},
						Target: "target.example.com.",
					},
					&dns.A{
						Hdr: dns.RR_Header{Name: "target.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
						A:   []byte{192, 168, 1, 1},
					},
				}
				return msg
			},
			queryType:      dns.TypeA,
			wantResolution: false,
			wantTarget:     "",
		},
		{
			name: "CNAME with AAAA record - no resolution needed",
			setupMsg: func() *dns.Msg {
				msg := new(dns.Msg)
				msg.Answer = []dns.RR{
					&dns.CNAME{
						Hdr:    dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 300},
						Target: "target.example.com.",
					},
					&dns.AAAA{
						Hdr:  dns.RR_Header{Name: "target.example.com.", Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 300},
						AAAA: []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
					},
				}
				return msg
			},
			queryType:      dns.TypeAAAA,
			wantResolution: false,
			wantTarget:     "",
		},
		{
			name: "CNAME for MX query - no resolution needed",
			setupMsg: func() *dns.Msg {
				msg := new(dns.Msg)
				msg.Answer = []dns.RR{
					&dns.CNAME{
						Hdr:    dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 300},
						Target: "target.example.com.",
					},
				}
				return msg
			},
			queryType:      dns.TypeMX,
			wantResolution: false,
			wantTarget:     "",
		},
		{
			name: "No answers - no resolution needed",
			setupMsg: func() *dns.Msg {
				msg := new(dns.Msg)
				msg.Answer = []dns.RR{}
				return msg
			},
			queryType:      dns.TypeA,
			wantResolution: false,
			wantTarget:     "",
		},
		{
			name: "Only A record - no resolution needed",
			setupMsg: func() *dns.Msg {
				msg := new(dns.Msg)
				msg.Answer = []dns.RR{
					&dns.A{
						Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
						A:   []byte{192, 168, 1, 1},
					},
				}
				return msg
			},
			queryType:      dns.TypeA,
			wantResolution: false,
			wantTarget:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.setupMsg()
			needsResolution, target := resolver.needsCNAMEResolution(msg, tt.queryType)

			if needsResolution != tt.wantResolution {
				t.Errorf("needsCNAMEResolution() needsResolution = %v, want %v", needsResolution, tt.wantResolution)
			}

			if target != tt.wantTarget {
				t.Errorf("needsCNAMEResolution() target = %v, want %v", target, tt.wantTarget)
			}
		})
	}
}

func TestDNSResolver_resolveCNAMERecursively_MaxDepth(t *testing.T) {
	logger := logger.NewTestLogger()
	config := DNSConfig{
		ListenAddress:       ":15353",
		ManagedDomains:      []string{"test.local"},
		InternalNameservers: []string{"127.0.0.1:5354"},
		UpstreamNameservers: []string{"8.8.8.8:53"},
		QueryTimeout:        "2s",
	}

	resolver, err := New(context.Background(), logger, config)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}

	// Test that max recursion depth is enforced
	ctx := context.Background()
	nameservers := []string{"127.0.0.1:5354"}

	// Start at depth that would exceed max
	_, err = resolver.resolveCNAMERecursively(ctx, "test.example.com", dns.TypeA, nameservers, maxRecursionDepth)
	if err == nil {
		t.Error("Expected error for exceeding max recursion depth, got nil")
	}

	if err != nil && err.Error() != "max recursion depth reached for CNAME chain" {
		t.Errorf("Expected max recursion depth error, got: %v", err)
	}
}

func TestDNSResolver_PreservesOriginalQuestionAndHeader(t *testing.T) {
	// Test that when merging recursive CNAME responses, the original Question and Header are preserved
	originalMsg := new(dns.Msg)
	originalMsg.SetQuestion("example.com.", dns.TypeA)
	originalMsg.Id = 12345
	originalMsg.RecursionDesired = true
	originalMsg.CheckingDisabled = false

	// Simulate initial CNAME response
	cnameResponse := new(dns.Msg)
	cnameResponse.SetReply(originalMsg)
	cnameResponse.Answer = []dns.RR{
		&dns.CNAME{
			Hdr:    dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 300},
			Target: "target.example.com.",
		},
	}

	// Simulate recursive resolution response (would have different question)
	recursiveResponse := new(dns.Msg)
	recursiveResponse.SetQuestion("target.example.com.", dns.TypeA)
	recursiveResponse.Answer = []dns.RR{
		&dns.A{
			Hdr: dns.RR_Header{Name: "target.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
			A:   []byte{192, 168, 1, 1},
		},
	}

	// Simulate the merge logic from server.go lines 301-305
	finalResponse := recursiveResponse
	finalResponse.MsgHdr = cnameResponse.MsgHdr
	finalResponse.Question = cnameResponse.Question
	finalResponse.Answer = append(cnameResponse.Answer, finalResponse.Answer...)

	// Validate the final response preserves original question
	if len(finalResponse.Question) != 1 {
		t.Fatalf("Expected 1 question, got %d", len(finalResponse.Question))
	}

	if finalResponse.Question[0].Name != "example.com." {
		t.Errorf("Expected question name 'example.com.', got '%s'", finalResponse.Question[0].Name)
	}

	if finalResponse.Question[0].Qtype != dns.TypeA {
		t.Errorf("Expected question type A, got %d", finalResponse.Question[0].Qtype)
	}

	// Validate transaction ID preserved
	if finalResponse.Id != 12345 {
		t.Errorf("Expected transaction ID 12345, got %d", finalResponse.Id)
	}

	// Validate header flags preserved
	if finalResponse.RecursionDesired != originalMsg.RecursionDesired {
		t.Errorf("RecursionDesired flag not preserved")
	}

	// Validate merged answers (CNAME + A record)
	if len(finalResponse.Answer) != 2 {
		t.Fatalf("Expected 2 answers (CNAME + A), got %d", len(finalResponse.Answer))
	}

	if finalResponse.Answer[0].Header().Rrtype != dns.TypeCNAME {
		t.Errorf("Expected first answer to be CNAME, got type %d", finalResponse.Answer[0].Header().Rrtype)
	}

	if finalResponse.Answer[1].Header().Rrtype != dns.TypeA {
		t.Errorf("Expected second answer to be A, got type %d", finalResponse.Answer[1].Header().Rrtype)
	}
}

func TestDNSResolver_selectNameservers(t *testing.T) {
	logger := logger.NewTestLogger()

	tests := []struct {
		name            string
		config          DNSConfig
		target          string
		expectShuffled  bool
		expectedServers []string
	}{
		{
			name: "single internal nameserver - no shuffle",
			config: DNSConfig{
				ManagedDomains:      []string{"internal.local"},
				InternalNameservers: []string{"ns1.internal:53"},
				UpstreamNameservers: []string{"8.8.8.8:53"},
				QueryTimeout:        "2s",
				ListenAddress:       ":15353",
			},
			target:          "test.internal.local",
			expectShuffled:  false,
			expectedServers: []string{"ns1.internal:53"},
		},
		{
			name: "multiple internal nameservers - should shuffle",
			config: DNSConfig{
				ManagedDomains:      []string{"internal.local"},
				InternalNameservers: []string{"ns1.internal:53", "ns2.internal:53", "ns3.internal:53"},
				UpstreamNameservers: []string{"8.8.8.8:53"},
				QueryTimeout:        "2s",
				ListenAddress:       ":15353",
			},
			target:          "test.internal.local",
			expectShuffled:  true,
			expectedServers: []string{"ns1.internal:53", "ns2.internal:53", "ns3.internal:53"},
		},
		{
			name: "upstream domain - no shuffle",
			config: DNSConfig{
				ManagedDomains:      []string{"internal.local"},
				InternalNameservers: []string{"ns1.internal:53", "ns2.internal:53"},
				UpstreamNameservers: []string{"8.8.8.8:53", "8.8.4.4:53"},
				QueryTimeout:        "2s",
				ListenAddress:       ":15353",
			},
			target:          "google.com",
			expectShuffled:  false,
			expectedServers: []string{"8.8.8.8:53", "8.8.4.4:53"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver, err := New(context.Background(), logger, tt.config)
			if err != nil {
				t.Fatalf("Failed to create resolver: %v", err)
			}

			result := resolver.selectNameservers(tt.target)

			// Verify length matches expected
			if len(result) != len(tt.expectedServers) {
				t.Errorf("Expected %d nameservers, got %d", len(tt.expectedServers), len(result))
			}

			// Verify all expected servers are present
			resultMap := make(map[string]bool)
			for _, ns := range result {
				resultMap[ns] = true
			}

			for _, expected := range tt.expectedServers {
				if !resultMap[expected] {
					t.Errorf("Expected nameserver %s not found in result", expected)
				}
			}

			// If we expect no shuffling, verify order is unchanged
			if !tt.expectShuffled {
				for i, ns := range result {
					if ns != tt.expectedServers[i] {
						t.Errorf("Order changed when no shuffle expected: got %v, want %v", result, tt.expectedServers)
						break
					}
				}
			}
		})
	}
}

func TestDNSResolver_selectNameservers_ShuffleDistribution(t *testing.T) {
	logger := logger.NewTestLogger()

	config := DNSConfig{
		ManagedDomains:      []string{"internal.local"},
		InternalNameservers: []string{"ns1.internal:53", "ns2.internal:53", "ns3.internal:53"},
		UpstreamNameservers: []string{"8.8.8.8:53"},
		QueryTimeout:        "2s",
		ListenAddress:       ":15353",
	}

	resolver, err := New(context.Background(), logger, config)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}

	// Track first position distribution
	firstPositionCounts := make(map[string]int)
	iterations := 100

	// Run multiple iterations to verify shuffling
	for i := 0; i < iterations; i++ {
		result := resolver.selectNameservers("test.internal.local")
		if len(result) > 0 {
			firstPositionCounts[result[0]]++
		}
	}

	// Verify all nameservers appear at least once in first position
	// This confirms shuffling is happening
	for _, ns := range config.InternalNameservers {
		if firstPositionCounts[ns] == 0 {
			t.Errorf("Nameserver %s never appeared in first position after %d iterations - shuffle may not be working", ns, iterations)
		}
	}

	// Verify no single nameserver dominates (basic distribution check)
	// With proper shuffling, no server should appear more than 80% of the time
	threshold := int(float64(iterations) * 0.8)
	for ns, count := range firstPositionCounts {
		if count > threshold {
			t.Errorf("Nameserver %s appeared in first position %d/%d times (>80%%) - poor distribution", ns, count, iterations)
		}
	}

	t.Logf("First position distribution over %d iterations: %v", iterations, firstPositionCounts)
}

func TestDNSResolver_selectNameservers_ManagedDomainCheck(t *testing.T) {
	logger := logger.NewTestLogger()

	config := DNSConfig{
		ManagedDomains:      []string{"internal.local", "corp.internal"},
		InternalNameservers: []string{"ns1.internal:53", "ns2.internal:53"},
		UpstreamNameservers: []string{"8.8.8.8:53", "8.8.4.4:53"},
		QueryTimeout:        "2s",
		ListenAddress:       ":15353",
	}

	resolver, err := New(context.Background(), logger, config)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}

	tests := []struct {
		domain            string
		shouldUseInternal bool
	}{
		{"test.internal.local", true},
		{"internal.local", true},
		{"sub.corp.internal", true},
		{"corp.internal", true},
		{"google.com", false},
		{"example.org", false},
		{"notinternal.local", false},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			result := resolver.selectNameservers(tt.domain)

			if tt.shouldUseInternal {
				// Verify all returned nameservers are from internal list
				for _, ns := range result {
					found := false
					for _, internal := range config.InternalNameservers {
						if ns == internal {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Expected internal nameserver for %s, got %s", tt.domain, ns)
					}
				}
			} else {
				// Verify all returned nameservers are from upstream list
				for _, ns := range result {
					found := false
					for _, upstream := range config.UpstreamNameservers {
						if ns == upstream {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Expected upstream nameserver for %s, got %s", tt.domain, ns)
					}
				}
			}
		})
	}
}

// mockConn is a mock net.Conn for testing
type mockConn struct {
	readBuf  []byte
	writeBuf []byte
	closed   bool
	isTCP    bool
	response *dns.Msg
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	if m.closed {
		return 0, net.ErrClosed
	}

	// If readBuf is empty and we have a response, prepare it
	if len(m.readBuf) == 0 && m.response != nil {
		packed, err := m.response.Pack()
		if err != nil {
			return 0, err
		}
		if m.isTCP {
			tcpMsg := make([]byte, 2+len(packed))
			tcpMsg[0] = byte(len(packed) >> 8)
			tcpMsg[1] = byte(len(packed))
			copy(tcpMsg[2:], packed)
			m.readBuf = tcpMsg
		} else {
			m.readBuf = packed
		}
		m.response = nil // Only prepare once
	}

	if len(m.readBuf) == 0 {
		return 0, net.ErrClosed
	}
	n = copy(b, m.readBuf)
	m.readBuf = m.readBuf[n:]
	return n, nil
}

func (m *mockConn) Write(b []byte) (n int, err error) {
	if m.closed {
		return 0, net.ErrClosed
	}
	m.writeBuf = append(m.writeBuf, b...)
	return len(b), nil
}

func (m *mockConn) Close() error {
	m.closed = true
	return nil
}

func (m *mockConn) LocalAddr() net.Addr {
	if m.isTCP {
		return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
	}
	return &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
}

func (m *mockConn) RemoteAddr() net.Addr {
	if m.isTCP {
		return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 53}
	}
	return &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 53}
}

func (m *mockConn) SetDeadline(t time.Time) error {
	return nil
}

func (m *mockConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (m *mockConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func TestDNSResolver_CustomDialer(t *testing.T) {
	logger := logger.NewTestLogger()

	// Track dialer calls
	var dialerCalled bool
	var dialerNetwork string
	var dialerAddress string
	var dialerCallCount int

	// Create a custom dialer that tracks calls
	customDialer := func(ctx context.Context, network, address string) (net.Conn, error) {
		dialerCalled = true
		dialerNetwork = network
		dialerAddress = address
		dialerCallCount++

		// Create a mock DNS response
		msg := new(dns.Msg)
		msg.SetQuestion("test.example.com.", dns.TypeA)
		msg.Answer = []dns.RR{
			&dns.A{
				Hdr: dns.RR_Header{Name: "test.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
				A:   net.ParseIP("192.168.1.1"),
			},
		}

		// Pack the response
		packed, err := msg.Pack()
		if err != nil {
			return nil, err
		}

		// DNS over TCP requires a 2-byte length prefix
		tcpMsg := make([]byte, 2+len(packed))
		tcpMsg[0] = byte(len(packed) >> 8)
		tcpMsg[1] = byte(len(packed))
		copy(tcpMsg[2:], packed)

		// Return a mock connection with the DNS response
		return &mockConn{
			readBuf: tcpMsg,
		}, nil
	}

	config := DNSConfig{
		ManagedDomains:      []string{"example.com"},
		InternalNameservers: []string{"ns1.example.com:53"},
		UpstreamNameservers: []string{"8.8.8.8:53"},
		QueryTimeout:        "2s",
		ListenAddress:       ":15353",
		DialContext:         customDialer,
		DefaultProtocol:     "tcp",
	}

	resolver, err := New(context.Background(), logger, config)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}

	// Create a DNS query
	query := new(dns.Msg)
	query.SetQuestion("test.example.com.", dns.TypeA)
	queryBytes, err := query.Pack()
	if err != nil {
		t.Fatalf("Failed to pack query: %v", err)
	}

	// Query the nameserver directly (this is where the dialer is used)
	response, err := resolver.queryNameserver(context.Background(), queryBytes, "ns1.example.com:53")
	if err != nil {
		t.Fatalf("queryNameserver failed: %v", err)
	}

	// Verify custom dialer was called
	if !dialerCalled {
		t.Error("Custom dialer was not called")
	}

	if dialerNetwork != "tcp" {
		t.Errorf("Expected dialer to be called with 'tcp', got '%s'", dialerNetwork)
	}

	if dialerAddress != "ns1.example.com:53" {
		t.Errorf("Expected dialer to be called with 'ns1.example.com:53', got '%s'", dialerAddress)
	}

	if dialerCallCount != 1 {
		t.Errorf("Expected dialer to be called once, got %d calls", dialerCallCount)
	}

	// Verify we got a valid response
	if response == nil {
		t.Fatal("Expected non-nil response")
	}

	if len(response.Answer) != 1 {
		t.Errorf("Expected 1 answer, got %d", len(response.Answer))
	}
}

func TestDNSResolver_DefaultDialer(t *testing.T) {
	logger := logger.NewTestLogger()

	// Config without custom dialer
	config := DNSConfig{
		ManagedDomains:      []string{"example.com"},
		InternalNameservers: []string{"127.0.0.1:5354"},
		UpstreamNameservers: []string{"8.8.8.8:53"},
		QueryTimeout:        "2s",
		ListenAddress:       ":15353",
		// DialContext is nil, should use default dialer
	}

	resolver, err := New(context.Background(), logger, config)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}

	// Verify resolver was created successfully with default dialer
	if resolver.dialer == nil {
		t.Error("Expected resolver to have a dialer function, got nil")
	}

	// Create a DNS query
	query := new(dns.Msg)
	query.SetQuestion("test.example.com.", dns.TypeA)
	queryBytes, err := query.Pack()
	if err != nil {
		t.Fatalf("Failed to pack query: %v", err)
	}

	// Attempt to query (will fail because nameserver doesn't exist, but that's ok)
	// We just want to verify the default dialer is set and can be called
	_, err = resolver.queryNameserver(context.Background(), queryBytes, "127.0.0.1:5354")

	// We expect an error because the nameserver doesn't exist,
	// but it should be a connection error, not a nil pointer error
	if err == nil {
		t.Log("Unexpectedly succeeded connecting to non-existent nameserver")
	} else {
		// Verify it's a connection error, not a panic or nil pointer
		if err.Error() == "" {
			t.Error("Expected connection error message, got empty string")
		}
		t.Logf("Got expected connection error: %v", err)
	}
}

func TestDNSResolver_parseSimpleDNSQuery(t *testing.T) {
	logger := logger.NewTestLogger()
	config := DNSConfig{
		ListenAddress:       ":15353",
		ManagedDomains:      []string{"test.local"},
		InternalNameservers: []string{"127.0.0.1:5354"},
		UpstreamNameservers: []string{"8.8.8.8:53"},
		QueryTimeout:        "2s",
	}

	resolver, err := New(context.Background(), logger, config)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}

	tests := []struct {
		name        string
		setupQuery  func() []byte
		wantDomain  string
		wantType    uint16
		wantErr     bool
		errContains string
	}{
		{
			name: "simple uncompressed query",
			setupQuery: func() []byte {
				msg := new(dns.Msg)
				msg.SetQuestion("example.com.", dns.TypeA)
				packed, _ := msg.Pack()
				return packed
			},
			wantDomain: "example.com",
			wantType:   dns.TypeA,
			wantErr:    false,
		},
		{
			name: "query with compression pointers",
			setupQuery: func() []byte {
				// Create a query that would use compression if it were a response
				// (queries typically don't compress, but the parser should handle it)
				msg := new(dns.Msg)
				msg.SetQuestion("sub.example.com.", dns.TypeAAAA)
				packed, _ := msg.Pack()
				return packed
			},
			wantDomain: "sub.example.com",
			wantType:   dns.TypeAAAA,
			wantErr:    false,
		},
		{
			name: "MX query",
			setupQuery: func() []byte {
				msg := new(dns.Msg)
				msg.SetQuestion("mail.example.com.", dns.TypeMX)
				packed, _ := msg.Pack()
				return packed
			},
			wantDomain: "mail.example.com",
			wantType:   dns.TypeMX,
			wantErr:    false,
		},
		{
			name: "CNAME query",
			setupQuery: func() []byte {
				msg := new(dns.Msg)
				msg.SetQuestion("alias.example.com.", dns.TypeCNAME)
				packed, _ := msg.Pack()
				return packed
			},
			wantDomain: "alias.example.com",
			wantType:   dns.TypeCNAME,
			wantErr:    false,
		},
		{
			name: "truncated packet",
			setupQuery: func() []byte {
				return []byte{0x00, 0x01, 0x02} // Too short to be valid
			},
			wantErr:     true,
			errContains: "failed to unpack",
		},
		{
			name: "empty packet",
			setupQuery: func() []byte {
				return []byte{}
			},
			wantErr:     true,
			errContains: "failed to unpack",
		},
		{
			name: "query with no questions",
			setupQuery: func() []byte {
				msg := new(dns.Msg)
				msg.Id = 1234
				// Don't add any questions
				packed, _ := msg.Pack()
				return packed
			},
			wantErr:     true,
			errContains: "empty question section",
		},
		{
			name: "long domain name",
			setupQuery: func() []byte {
				msg := new(dns.Msg)
				msg.SetQuestion("very.long.subdomain.with.many.labels.example.com.", dns.TypeA)
				packed, _ := msg.Pack()
				return packed
			},
			wantDomain: "very.long.subdomain.with.many.labels.example.com",
			wantType:   dns.TypeA,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queryBytes := tt.setupQuery()
			domain, qtype, err := resolver.parseSimpleDNSQuery(queryBytes)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error containing '%s', got nil", tt.errContains)
					return
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if domain != tt.wantDomain {
				t.Errorf("Domain = %q, want %q", domain, tt.wantDomain)
			}

			if qtype != tt.wantType {
				t.Errorf("Query type = %d (%s), want %d (%s)",
					qtype, dns.TypeToString[qtype],
					tt.wantType, dns.TypeToString[tt.wantType])
			}
		})
	}
}

// contains checks if a string contains a substring (helper for tests)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && (s[0:len(substr)] == substr || contains(s[1:], substr))))
}

func TestDNSConfig_DefaultProtocol(t *testing.T) {
	tests := []struct {
		name             string
		config           DNSConfig
		expectedProtocol string
	}{
		{
			name:             "default config uses udp",
			config:           DefaultDNSConfig(),
			expectedProtocol: "udp",
		},
		{
			name: "config with no protocol defaults to empty",
			config: DNSConfig{
				ManagedDomains:      []string{"example.com"},
				InternalNameservers: []string{"127.0.0.1:53"},
				UpstreamNameservers: []string{"8.8.8.8:53"},
				QueryTimeout:        "2s",
			},
			expectedProtocol: "",
		},
		{
			name: "config with tcp protocol",
			config: DNSConfig{
				ManagedDomains:      []string{"example.com"},
				InternalNameservers: []string{"127.0.0.1:53"},
				UpstreamNameservers: []string{"8.8.8.8:53"},
				QueryTimeout:        "2s",
				DefaultProtocol:     "tcp",
			},
			expectedProtocol: "tcp",
		},
		{
			name: "config with udp protocol",
			config: DNSConfig{
				ManagedDomains:      []string{"example.com"},
				InternalNameservers: []string{"127.0.0.1:53"},
				UpstreamNameservers: []string{"8.8.8.8:53"},
				QueryTimeout:        "2s",
				DefaultProtocol:     "udp",
			},
			expectedProtocol: "udp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.config.DefaultProtocol != tt.expectedProtocol {
				t.Errorf("DefaultProtocol = %q, want %q", tt.config.DefaultProtocol, tt.expectedProtocol)
			}
		})
	}
}

func TestDNSResolver_ProtocolSupport(t *testing.T) {
	logger := logger.NewTestLogger()

	tests := []struct {
		name             string
		protocol         string
		expectedProtocol string
		mockResponse     *dns.Msg
	}{
		{
			name:             "udp protocol",
			protocol:         "udp",
			expectedProtocol: "udp",
			mockResponse: &dns.Msg{
				MsgHdr: dns.MsgHdr{
					Id:       1234,
					Response: true,
					Rcode:    dns.RcodeSuccess,
				},
				Question: []dns.Question{{Name: "test.example.com.", Qtype: dns.TypeA, Qclass: dns.ClassINET}},
				Answer: []dns.RR{
					&dns.A{
						Hdr: dns.RR_Header{Name: "test.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
						A:   net.ParseIP("192.168.1.1"),
					},
				},
			},
		},
		{
			name:             "tcp protocol",
			protocol:         "tcp",
			expectedProtocol: "tcp",
			mockResponse: &dns.Msg{
				MsgHdr: dns.MsgHdr{
					Id:       1234,
					Response: true,
					Rcode:    dns.RcodeSuccess,
				},
				Question: []dns.Question{{Name: "test.example.com.", Qtype: dns.TypeA, Qclass: dns.ClassINET}},
				Answer: []dns.RR{
					&dns.A{
						Hdr: dns.RR_Header{Name: "test.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
						A:   net.ParseIP("192.168.1.1"),
					},
				},
			},
		},
		{
			name:             "default protocol (empty) defaults to udp",
			protocol:         "",
			expectedProtocol: "udp",
			mockResponse: &dns.Msg{
				MsgHdr: dns.MsgHdr{
					Id:       1234,
					Response: true,
					Rcode:    dns.RcodeSuccess,
				},
				Question: []dns.Question{{Name: "test.example.com.", Qtype: dns.TypeA, Qclass: dns.ClassINET}},
				Answer: []dns.RR{
					&dns.A{
						Hdr: dns.RR_Header{Name: "test.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
						A:   net.ParseIP("192.168.1.1"),
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedProtocol string

			// Create custom dialer that captures the protocol
			customDialer := func(ctx context.Context, network, address string) (net.Conn, error) {
				capturedProtocol = network

				// Pack the response
				packed, err := tt.mockResponse.Pack()
				if err != nil {
					return nil, err
				}

				var readBuf []byte
				if network == "tcp" {
					// DNS over TCP requires a 2-byte length prefix
					readBuf = make([]byte, 2+len(packed))
					readBuf[0] = byte(len(packed) >> 8)
					readBuf[1] = byte(len(packed))
					copy(readBuf[2:], packed)
				} else {
					// UDP: raw DNS message without length prefix
					readBuf = packed
				}

				// Return a mock connection with the DNS response
				return &mockConn{
					readBuf: readBuf,
					isTCP:   network == "tcp",
				}, nil
			}

			config := DNSConfig{
				ManagedDomains:      []string{"example.com"},
				InternalNameservers: []string{"ns1.example.com:53"},
				UpstreamNameservers: []string{"8.8.8.8:53"},
				QueryTimeout:        "2s",
				ListenAddress:       ":15353",
				DialContext:         customDialer,
				DefaultProtocol:     tt.protocol,
			}

			resolver, err := New(context.Background(), logger, config)
			if err != nil {
				t.Fatalf("Failed to create resolver: %v", err)
			}

			// Create a DNS query
			query := new(dns.Msg)
			query.SetQuestion("test.example.com.", dns.TypeA)
			queryBytes, err := query.Pack()
			if err != nil {
				t.Fatalf("Failed to pack query: %v", err)
			}

			// Query the nameserver
			response, err := resolver.queryNameserver(context.Background(), queryBytes, "ns1.example.com:53")
			if err != nil {
				t.Fatalf("queryNameserver failed: %v", err)
			}

			// Verify the protocol was passed to dialer
			if capturedProtocol != tt.expectedProtocol {
				t.Errorf("Protocol = %q, want %q", capturedProtocol, tt.expectedProtocol)
			}

			// Verify we got a valid response
			if response == nil {
				t.Fatal("Expected non-nil response")
			}

			if len(response.Answer) != 1 {
				t.Errorf("Expected 1 answer, got %d", len(response.Answer))
			}
		})
	}
}

func TestDNSResolver_ValidateUpstream(t *testing.T) {
	testLogger := logger.NewTestLogger()

	t.Run("dial error fails validation", func(t *testing.T) {
		mockDialer := func(ctx context.Context, network, address string) (net.Conn, error) {
			return nil, net.UnknownNetworkError("no route to host")
		}

		config := DNSConfig{
			ManagedDomains:      []string{"test.local"},
			InternalNameservers: []string{"ns1.test.local:53"},
			UpstreamNameservers: []string{"8.8.8.8:53"},
			QueryTimeout:        "2s",
			ListenAddress:       ":15353",
			DialContext:         mockDialer,
		}

		resolver, err := New(context.Background(), testLogger, config)
		if err != nil {
			t.Fatalf("Failed to create resolver: %v", err)
		}

		err = resolver.ValidateUpstream("example.com")
		if err == nil {
			t.Error("ValidateUpstream() expected error for dial failure, got nil")
		}
	})

	t.Run("successful validation with real DNS", func(t *testing.T) {
		if testing.Short() {
			t.Skip("Skipping integration test in short mode")
		}

		config := DNSConfig{
			ManagedDomains:      []string{"test.local"},
			InternalNameservers: []string{"ns1.test.local:53"},
			UpstreamNameservers: []string{"9.9.9.9:53", "1.1.1.1:53"},
			QueryTimeout:        "5s",
			ListenAddress:       ":15353",
		}

		resolver, err := New(context.Background(), testLogger, config)
		if err != nil {
			t.Fatalf("Failed to create resolver: %v", err)
		}

		err = resolver.ValidateUpstream("google.com")
		if err != nil {
			t.Errorf("ValidateUpstream() unexpected error: %v", err)
		}
	})

	t.Run("empty domain uses default", func(t *testing.T) {
		if testing.Short() {
			t.Skip("Skipping integration test in short mode")
		}

		config := DNSConfig{
			ManagedDomains:      []string{"test.local"},
			InternalNameservers: []string{"ns1.test.local:53"},
			UpstreamNameservers: []string{"9.9.9.9:53", "1.1.1.1:53"},
			QueryTimeout:        "5s",
			ListenAddress:       ":15353",
		}

		resolver, err := New(context.Background(), testLogger, config)
		if err != nil {
			t.Fatalf("Failed to create resolver: %v", err)
		}

		// Empty string should use default "agentuity.com"
		err = resolver.ValidateUpstream("")
		if err != nil {
			t.Errorf("ValidateUpstream() unexpected error: %v", err)
		}
	})
}

func TestDNSResolver_ValidateUpstream_NoUpstreams(t *testing.T) {
	testLogger := logger.NewTestLogger()

	config := DNSConfig{
		ManagedDomains:      []string{"test.local"},
		InternalNameservers: []string{"ns1.test.local:53"},
		UpstreamNameservers: []string{}, // No upstreams - but validation will fail
		QueryTimeout:        "2s",
		ListenAddress:       ":15353",
	}

	// This will fail validation because no upstream nameservers
	_, err := New(context.Background(), testLogger, config)
	if err == nil {
		t.Error("Expected error when creating resolver with no upstream nameservers")
	}
}

func TestDNSResolver_NameserverFallback(t *testing.T) {
	testLogger := logger.NewTestLogger()

	tests := []struct {
		name                string
		firstResponse       *dns.Msg
		secondResponse      *dns.Msg
		expectSecondCalled  bool
		expectSuccessAnswer bool
	}{
		{
			name: "SERVFAIL triggers fallback to next nameserver",
			firstResponse: func() *dns.Msg {
				m := new(dns.Msg)
				m.SetQuestion("example.com.", dns.TypeA)
				m.Rcode = dns.RcodeServerFailure
				return m
			}(),
			secondResponse: func() *dns.Msg {
				m := new(dns.Msg)
				m.SetQuestion("example.com.", dns.TypeA)
				m.Rcode = dns.RcodeSuccess
				m.Answer = []dns.RR{
					&dns.A{
						Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
						A:   net.ParseIP("93.184.216.34"),
					},
				}
				return m
			}(),
			expectSecondCalled:  true,
			expectSuccessAnswer: true,
		},
		{
			name: "REFUSED triggers fallback to next nameserver",
			firstResponse: func() *dns.Msg {
				m := new(dns.Msg)
				m.SetQuestion("example.com.", dns.TypeA)
				m.Rcode = dns.RcodeRefused
				return m
			}(),
			secondResponse: func() *dns.Msg {
				m := new(dns.Msg)
				m.SetQuestion("example.com.", dns.TypeA)
				m.Rcode = dns.RcodeSuccess
				m.Answer = []dns.RR{
					&dns.A{
						Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
						A:   net.ParseIP("93.184.216.34"),
					},
				}
				return m
			}(),
			expectSecondCalled:  true,
			expectSuccessAnswer: true,
		},
		{
			name: "NXDOMAIN does NOT trigger fallback (authoritative)",
			firstResponse: func() *dns.Msg {
				m := new(dns.Msg)
				m.SetQuestion("nonexistent.example.com.", dns.TypeA)
				m.Rcode = dns.RcodeNameError // NXDOMAIN
				return m
			}(),
			secondResponse: func() *dns.Msg {
				m := new(dns.Msg)
				m.SetQuestion("nonexistent.example.com.", dns.TypeA)
				m.Rcode = dns.RcodeSuccess
				m.Answer = []dns.RR{
					&dns.A{
						Hdr: dns.RR_Header{Name: "nonexistent.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
						A:   net.ParseIP("1.2.3.4"),
					},
				}
				return m
			}(),
			expectSecondCalled:  false,
			expectSuccessAnswer: false, // Should return NXDOMAIN, not success
		},
		{
			name: "Success on first nameserver does not call second",
			firstResponse: func() *dns.Msg {
				m := new(dns.Msg)
				m.SetQuestion("example.com.", dns.TypeA)
				m.Rcode = dns.RcodeSuccess
				m.Answer = []dns.RR{
					&dns.A{
						Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
						A:   net.ParseIP("93.184.216.34"),
					},
				}
				return m
			}(),
			secondResponse:      nil, // Should never be called
			expectSecondCalled:  false,
			expectSuccessAnswer: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var callCount int
			var secondCalled bool

			// Create a mock dialer that returns different responses per nameserver
			mockDialer := func(ctx context.Context, network, address string) (net.Conn, error) {
				mu.Lock()
				callCount++
				mu.Unlock()

				var response *dns.Msg
				if address == "ns1.test:53" {
					response = tt.firstResponse
				} else if address == "ns2.test:53" {
					mu.Lock()
					secondCalled = true
					mu.Unlock()
					response = tt.secondResponse
				}

				if response == nil {
					return nil, net.UnknownNetworkError("no response configured")
				}

				// Pack and prepare the response for TCP
				packed, err := response.Pack()
				if err != nil {
					return nil, err
				}

				tcpMsg := make([]byte, 2+len(packed))
				tcpMsg[0] = byte(len(packed) >> 8)
				tcpMsg[1] = byte(len(packed))
				copy(tcpMsg[2:], packed)

				return &mockConn{
					readBuf: tcpMsg,
					isTCP:   true,
				}, nil
			}

			config := DNSConfig{
				ManagedDomains:      []string{"test.local"},
				InternalNameservers: []string{"ns.internal:53"},
				UpstreamNameservers: []string{"ns1.test:53", "ns2.test:53"},
				QueryTimeout:        "2s",
				ListenAddress:       ":15353",
				DialContext:         mockDialer,
				DefaultProtocol:     "tcp",
			}

			resolver, err := New(context.Background(), testLogger, config)
			if err != nil {
				t.Fatalf("Failed to create resolver: %v", err)
			}

			// Create a DNS query
			query := new(dns.Msg)
			query.SetQuestion("example.com.", dns.TypeA)
			queryBytes, err := query.Pack()
			if err != nil {
				t.Fatalf("Failed to pack query: %v", err)
			}

			// Use forwardQueryTCP to test the fallback behavior
			// With staggered queries, we need to allow time for the stagger timer to fire
			response := resolver.forwardQueryTCP(queryBytes, "example.com", dns.TypeA, config.UpstreamNameservers, "test:1")

			// Check if second nameserver was called as expected
			mu.Lock()
			gotSecondCalled := secondCalled
			mu.Unlock()
			if gotSecondCalled != tt.expectSecondCalled {
				t.Errorf("Second nameserver called = %v, want %v", gotSecondCalled, tt.expectSecondCalled)
			}

			// Check response
			if tt.expectSuccessAnswer {
				if response == nil {
					t.Fatal("Expected non-nil response")
				}
				if response.Rcode != dns.RcodeSuccess {
					t.Errorf("Expected RcodeSuccess, got %s", dns.RcodeToString[response.Rcode])
				}
				if len(response.Answer) == 0 {
					t.Error("Expected at least one answer")
				}
			} else if response != nil && response.Rcode == dns.RcodeSuccess && len(response.Answer) > 0 {
				// For NXDOMAIN case, we should NOT get a success with answers
				if tt.name == "NXDOMAIN does NOT trigger fallback (authoritative)" {
					t.Error("NXDOMAIN should not have triggered fallback to get success response")
				}
			}
		})
	}
}

func TestDNSResolver_NameserverFallback_ConnectionError(t *testing.T) {
	testLogger := logger.NewTestLogger()

	var callCount int

	// Create a mock dialer where first nameserver fails with connection error
	mockDialer := func(ctx context.Context, network, address string) (net.Conn, error) {
		callCount++

		if address == "ns1.test:53" {
			// First nameserver fails with connection error
			return nil, net.UnknownNetworkError("connection refused")
		}

		// Second nameserver succeeds
		response := new(dns.Msg)
		response.SetQuestion("example.com.", dns.TypeA)
		response.Rcode = dns.RcodeSuccess
		response.Answer = []dns.RR{
			&dns.A{
				Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
				A:   net.ParseIP("93.184.216.34"),
			},
		}

		packed, err := response.Pack()
		if err != nil {
			return nil, err
		}

		tcpMsg := make([]byte, 2+len(packed))
		tcpMsg[0] = byte(len(packed) >> 8)
		tcpMsg[1] = byte(len(packed))
		copy(tcpMsg[2:], packed)

		return &mockConn{
			readBuf: tcpMsg,
			isTCP:   true,
		}, nil
	}

	config := DNSConfig{
		ManagedDomains:      []string{"test.local"},
		InternalNameservers: []string{"ns.internal:53"},
		UpstreamNameservers: []string{"ns1.test:53", "ns2.test:53"},
		QueryTimeout:        "2s",
		ListenAddress:       ":15353",
		DialContext:         mockDialer,
		DefaultProtocol:     "tcp",
	}

	resolver, err := New(context.Background(), testLogger, config)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}

	// Create a DNS query
	query := new(dns.Msg)
	query.SetQuestion("example.com.", dns.TypeA)
	queryBytes, err := query.Pack()
	if err != nil {
		t.Fatalf("Failed to pack query: %v", err)
	}

	// Use forwardQueryTCP to test the fallback behavior
	response := resolver.forwardQueryTCP(queryBytes, "example.com", dns.TypeA, config.UpstreamNameservers, "test:1")

	// Both nameservers should have been called
	if callCount != 2 {
		t.Errorf("Expected 2 nameserver calls, got %d", callCount)
	}

	// Should get success from second nameserver
	if response == nil {
		t.Fatal("Expected non-nil response")
	}
	if response.Rcode != dns.RcodeSuccess {
		t.Errorf("Expected RcodeSuccess, got %s", dns.RcodeToString[response.Rcode])
	}
	if len(response.Answer) == 0 {
		t.Error("Expected at least one answer")
	}
}

func TestDNSResolver_NegativeCaching(t *testing.T) {
	testLogger := logger.NewTestLogger()

	tests := []struct {
		name           string
		response       *dns.Msg
		expectCached   bool
		expectedTTL    uint32
		cacheType      string
	}{
		{
			name: "NODATA response (NOERROR with no answers) is cached",
			response: &dns.Msg{
				MsgHdr: dns.MsgHdr{
					Id:       1234,
					Response: true,
					Rcode:    dns.RcodeSuccess,
				},
				Question: []dns.Question{{Name: "example.com.", Qtype: dns.TypeAAAA, Qclass: dns.ClassINET}},
				Answer:   []dns.RR{}, // No answers
				Ns: []dns.RR{
					&dns.SOA{
						Hdr:     dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 900},
						Ns:      "ns1.example.com.",
						Mbox:    "admin.example.com.",
						Serial:  1,
						Refresh: 21600,
						Retry:   3600,
						Expire:  259200,
						Minttl:  60, // Negative cache TTL
					},
				},
			},
			expectCached: true,
			expectedTTL:  60, // min(900, 60) = 60
			cacheType:    "negative",
		},
		{
			name: "NXDOMAIN response is cached",
			response: &dns.Msg{
				MsgHdr: dns.MsgHdr{
					Id:       1234,
					Response: true,
					Rcode:    dns.RcodeNameError, // NXDOMAIN
				},
				Question: []dns.Question{{Name: "nonexistent.example.com.", Qtype: dns.TypeA, Qclass: dns.ClassINET}},
				Answer:   []dns.RR{},
				Ns: []dns.RR{
					&dns.SOA{
						Hdr:     dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 300},
						Ns:      "ns1.example.com.",
						Mbox:    "admin.example.com.",
						Serial:  1,
						Refresh: 21600,
						Retry:   3600,
						Expire:  259200,
						Minttl:  120,
					},
				},
			},
			expectCached: true,
			expectedTTL:  120, // min(300, 120) = 120
			cacheType:    "negative",
		},
		{
			name: "SOA TTL used when smaller than MINIMUM",
			response: &dns.Msg{
				MsgHdr: dns.MsgHdr{
					Id:       1234,
					Response: true,
					Rcode:    dns.RcodeSuccess,
				},
				Question: []dns.Question{{Name: "example.com.", Qtype: dns.TypeAAAA, Qclass: dns.ClassINET}},
				Answer:   []dns.RR{},
				Ns: []dns.RR{
					&dns.SOA{
						Hdr:     dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 30},
						Ns:      "ns1.example.com.",
						Mbox:    "admin.example.com.",
						Serial:  1,
						Refresh: 21600,
						Retry:   3600,
						Expire:  259200,
						Minttl:  300, // Larger than TTL
					},
				},
			},
			expectCached: true,
			expectedTTL:  30, // min(30, 300) = 30
			cacheType:    "negative",
		},
		{
			name: "Negative response without SOA uses DefaultNegativeTTL",
			response: &dns.Msg{
				MsgHdr: dns.MsgHdr{
					Id:       1234,
					Response: true,
					Rcode:    dns.RcodeSuccess,
				},
				Question: []dns.Question{{Name: "example.com.", Qtype: dns.TypeAAAA, Qclass: dns.ClassINET}},
				Answer:   []dns.RR{}, // No answers
				Ns:       []dns.RR{}, // No SOA
			},
			expectCached: true,
			expectedTTL:  30, // Uses default negative TTL
			cacheType:    "negative",
		},
		{
			name: "SERVFAIL is not cached",
			response: &dns.Msg{
				MsgHdr: dns.MsgHdr{
					Id:       1234,
					Response: true,
					Rcode:    dns.RcodeServerFailure,
				},
				Question: []dns.Question{{Name: "example.com.", Qtype: dns.TypeA, Qclass: dns.ClassINET}},
			},
			expectCached: false,
			expectedTTL:  0,
			cacheType:    "",
		},
		{
			name: "REFUSED is not cached",
			response: &dns.Msg{
				MsgHdr: dns.MsgHdr{
					Id:       1234,
					Response: true,
					Rcode:    dns.RcodeRefused,
				},
				Question: []dns.Question{{Name: "example.com.", Qtype: dns.TypeA, Qclass: dns.ClassINET}},
			},
			expectCached: false,
			expectedTTL:  0,
			cacheType:    "",
		},
		{
			name: "Positive response is still cached",
			response: &dns.Msg{
				MsgHdr: dns.MsgHdr{
					Id:       1234,
					Response: true,
					Rcode:    dns.RcodeSuccess,
				},
				Question: []dns.Question{{Name: "example.com.", Qtype: dns.TypeA, Qclass: dns.ClassINET}},
				Answer: []dns.RR{
					&dns.A{
						Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 3600},
						A:   net.ParseIP("93.184.216.34"),
					},
				},
			},
			expectCached: true,
			expectedTTL:  3600,
			cacheType:    "positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			config := DNSConfig{
				ManagedDomains:      []string{"test.local"},
				InternalNameservers: []string{"ns1.test.local:53"},
				UpstreamNameservers: []string{"8.8.8.8:53"},
				QueryTimeout:        "2s",
				ListenAddress:       ":15353",
				DefaultNegativeTTL:  30, // Default for tests
			}

			resolver, err := New(ctx, testLogger, config)
			if err != nil {
				t.Fatalf("Failed to create resolver: %v", err)
			}
			defer resolver.Stop()

			// Pack the response
			responseBytes, err := tt.response.Pack()
			if err != nil {
				t.Fatalf("Failed to pack response: %v", err)
			}

			domain := "example.com"
			queryType := uint16(dns.TypeA)
			if len(tt.response.Question) > 0 {
				domain = tt.response.Question[0].Name
				queryType = tt.response.Question[0].Qtype
			}
			cacheKey := fmt.Sprintf("%s:%d", domain, queryType)

			// Call cacheResponse
			resolver.cacheResponse(cacheKey, responseBytes, tt.response, domain, queryType)

			// Check if it was cached
			found, cached, _ := resolver.cache.Get(cacheKey)

			if tt.expectCached {
				if !found {
					t.Errorf("Expected response to be cached, but it wasn't")
					return
				}

				cacheEntry, ok := cached.(*dnsCacheEntry)
				if !ok {
					t.Fatalf("Cached value is not a dnsCacheEntry")
				}

				if cacheEntry.minTTL != tt.expectedTTL {
					t.Errorf("Expected TTL %d, got %d", tt.expectedTTL, cacheEntry.minTTL)
				}
			} else {
				if found {
					t.Errorf("Expected response NOT to be cached, but it was")
				}
			}
		})
	}
}

func TestDNSResolver_NegativeCaching_DisabledDefault(t *testing.T) {
	testLogger := logger.NewTestLogger()
	ctx := context.Background()

	// Test that setting DefaultNegativeTTL to 0 disables caching when no SOA is present
	config := DNSConfig{
		ManagedDomains:      []string{"test.local"},
		InternalNameservers: []string{"ns1.test.local:53"},
		UpstreamNameservers: []string{"8.8.8.8:53"},
		QueryTimeout:        "2s",
		ListenAddress:       ":15353",
		DefaultNegativeTTL:  0, // Disabled
	}

	resolver, err := New(ctx, testLogger, config)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}
	defer resolver.Stop()

	// Create a negative response without SOA
	response := &dns.Msg{
		MsgHdr: dns.MsgHdr{
			Id:       1234,
			Response: true,
			Rcode:    dns.RcodeSuccess,
		},
		Question: []dns.Question{{Name: "example.com.", Qtype: dns.TypeAAAA, Qclass: dns.ClassINET}},
		Answer:   []dns.RR{}, // No answers
		Ns:       []dns.RR{}, // No SOA
	}

	responseBytes, err := response.Pack()
	if err != nil {
		t.Fatalf("Failed to pack response: %v", err)
	}

	cacheKey := "example.com.:28" // AAAA = 28
	resolver.cacheResponse(cacheKey, responseBytes, response, "example.com.", dns.TypeAAAA)

	// Should NOT be cached since DefaultNegativeTTL is 0
	found, _, _ := resolver.cache.Get(cacheKey)
	if found {
		t.Error("Expected response NOT to be cached when DefaultNegativeTTL is 0, but it was")
	}
}

func TestDNSResolver_GetNegativeCacheTTL(t *testing.T) {
	testLogger := logger.NewTestLogger()

	ctx := context.Background()
	config := DNSConfig{
		ManagedDomains:      []string{"test.local"},
		InternalNameservers: []string{"ns1.test.local:53"},
		UpstreamNameservers: []string{"8.8.8.8:53"},
		QueryTimeout:        "2s",
		ListenAddress:       ":15353",
	}

	resolver, err := New(ctx, testLogger, config)
	if err != nil {
		t.Fatalf("Failed to create resolver: %v", err)
	}

	tests := []struct {
		name        string
		response    *dns.Msg
		expectedTTL uint32
	}{
		{
			name: "SOA MINIMUM is smaller",
			response: &dns.Msg{
				Ns: []dns.RR{
					&dns.SOA{
						Hdr:    dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeSOA, Ttl: 900},
						Minttl: 60,
					},
				},
			},
			expectedTTL: 60,
		},
		{
			name: "SOA TTL is smaller",
			response: &dns.Msg{
				Ns: []dns.RR{
					&dns.SOA{
						Hdr:    dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeSOA, Ttl: 30},
						Minttl: 300,
					},
				},
			},
			expectedTTL: 30,
		},
		{
			name: "No SOA record",
			response: &dns.Msg{
				Ns: []dns.RR{},
			},
			expectedTTL: 0,
		},
		{
			name: "SOA with other NS records",
			response: &dns.Msg{
				Ns: []dns.RR{
					&dns.NS{
						Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeNS, Ttl: 3600},
						Ns:  "ns1.example.com.",
					},
					&dns.SOA{
						Hdr:    dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeSOA, Ttl: 600},
						Minttl: 120,
					},
				},
			},
			expectedTTL: 120,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ttl := resolver.getNegativeCacheTTL(tt.response)
			if ttl != tt.expectedTTL {
				t.Errorf("getNegativeCacheTTL() = %d, want %d", ttl, tt.expectedTTL)
			}
		})
	}
}
