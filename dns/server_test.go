package dns

import (
	"context"
	"net"
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

				// DNS over TCP requires a 2-byte length prefix
				tcpMsg := make([]byte, 2+len(packed))
				tcpMsg[0] = byte(len(packed) >> 8)
				tcpMsg[1] = byte(len(packed))
				copy(tcpMsg[2:], packed)

				// Return a mock connection with the DNS response
				return &mockConn{
					readBuf: tcpMsg,
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
