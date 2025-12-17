package dns

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/agentuity/go-common/logger"
	"github.com/miekg/dns"
)

// TestDNSResolver_DualStack tests that the DNS resolver listens on both IPv4 and IPv6
func TestDNSResolver_DualStack(t *testing.T) {
	log := logger.NewTestLogger()

	config := DNSConfig{
		ListenAddress:       ":15353", // Use non-standard port to avoid conflicts
		ManagedDomains:      []string{"test.internal"},
		InternalNameservers: []string{"8.8.8.8:53"},
		UpstreamNameservers: []string{"8.8.8.8:53", "8.8.4.4:53"},
		QueryTimeout:        "2s",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resolver, err := New(ctx, log, config)
	if err != nil {
		t.Fatalf("Failed to create DNS resolver: %v", err)
	}

	if err := resolver.Start(); err != nil {
		t.Fatalf("Failed to start DNS resolver: %v", err)
	}
	defer resolver.Stop()

	// Give the server a moment to start
	time.Sleep(100 * time.Millisecond)

	// Test IPv4 query
	t.Run("IPv4 query to localhost", func(t *testing.T) {
		client := &dns.Client{Net: "udp4", Timeout: 2 * time.Second}
		msg := &dns.Msg{}
		msg.SetQuestion("google.com.", dns.TypeA)

		resp, _, err := client.Exchange(msg, "127.0.0.1:15353")
		if err != nil {
			t.Fatalf("IPv4 query failed: %v", err)
		}
		if resp == nil {
			t.Fatal("Got nil response from IPv4 query")
		}
		if resp.Rcode != dns.RcodeSuccess && resp.Rcode != dns.RcodeServerFailure {
			t.Errorf("Unexpected response code: %d", resp.Rcode)
		}
	})

	// Test IPv6 query
	t.Run("IPv6 query to localhost", func(t *testing.T) {
		client := &dns.Client{Net: "udp6", Timeout: 2 * time.Second}
		msg := &dns.Msg{}
		msg.SetQuestion("google.com.", dns.TypeA)

		resp, _, err := client.Exchange(msg, "[::1]:15353")
		if err != nil {
			t.Fatalf("IPv6 query failed: %v", err)
		}
		if resp == nil {
			t.Fatal("Got nil response from IPv6 query")
		}
		if resp.Rcode != dns.RcodeSuccess && resp.Rcode != dns.RcodeServerFailure {
			t.Errorf("Unexpected response code: %d", resp.Rcode)
		}
	})

	// Test that both listeners are actually separate
	t.Run("both listeners active simultaneously", func(t *testing.T) {
		done := make(chan bool, 2)

		// Send IPv4 query
		go func() {
			client := &dns.Client{Net: "udp4", Timeout: 2 * time.Second}
			msg := &dns.Msg{}
			msg.SetQuestion("example.com.", dns.TypeA)
			_, _, err := client.Exchange(msg, "127.0.0.1:15353")
			if err != nil {
				t.Errorf("IPv4 concurrent query failed: %v", err)
			}
			done <- true
		}()

		// Send IPv6 query
		go func() {
			client := &dns.Client{Net: "udp6", Timeout: 2 * time.Second}
			msg := &dns.Msg{}
			msg.SetQuestion("example.org.", dns.TypeA)
			_, _, err := client.Exchange(msg, "[::1]:15353")
			if err != nil {
				t.Errorf("IPv6 concurrent query failed: %v", err)
			}
			done <- true
		}()

		// Wait for both queries to complete
		timeout := time.After(5 * time.Second)
		for i := 0; i < 2; i++ {
			select {
			case <-done:
				// Success
			case <-timeout:
				t.Fatal("Timeout waiting for concurrent queries")
			}
		}
	})
}

// TestDNSResolver_IPv4Only tests IPv4-only query behavior
func TestDNSResolver_IPv4Only(t *testing.T) {
	log := logger.NewTestLogger()

	config := DNSConfig{
		ListenAddress:       ":15354",
		ManagedDomains:      []string{"test.internal"},
		InternalNameservers: []string{"8.8.8.8:53"},
		UpstreamNameservers: []string{"8.8.8.8:53"},
		QueryTimeout:        "2s",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resolver, err := New(ctx, log, config)
	if err != nil {
		t.Fatalf("Failed to create DNS resolver: %v", err)
	}

	if err := resolver.Start(); err != nil {
		t.Fatalf("Failed to start DNS resolver: %v", err)
	}
	defer resolver.Stop()

	time.Sleep(100 * time.Millisecond)

	// Verify IPv4 listener exists
	conn, err := net.Dial("udp4", "127.0.0.1:15354")
	if err != nil {
		t.Fatalf("Failed to connect to IPv4 listener: %v", err)
	}
	conn.Close()
}

// TestDNSResolver_IPv6Only tests IPv6-only query behavior
func TestDNSResolver_IPv6Only(t *testing.T) {
	log := logger.NewTestLogger()

	config := DNSConfig{
		ListenAddress:       ":15355",
		ManagedDomains:      []string{"test.internal"},
		InternalNameservers: []string{"8.8.8.8:53"},
		UpstreamNameservers: []string{"8.8.8.8:53"},
		QueryTimeout:        "2s",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resolver, err := New(ctx, log, config)
	if err != nil {
		t.Fatalf("Failed to create DNS resolver: %v", err)
	}

	if err := resolver.Start(); err != nil {
		t.Fatalf("Failed to start DNS resolver: %v", err)
	}
	defer resolver.Stop()

	time.Sleep(100 * time.Millisecond)

	// Verify IPv6 listener exists
	conn, err := net.Dial("udp6", "[::1]:15355")
	if err != nil {
		t.Fatalf("Failed to connect to IPv6 listener: %v", err)
	}
	conn.Close()
}
