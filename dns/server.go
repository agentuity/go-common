package dns

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/agentuity/go-common/cache"
	"github.com/agentuity/go-common/logger"
	"github.com/miekg/dns"
)

const (
	dnsPacketSize         = 1232 // EDNS0-safe UDP payload size to avoid IPv6 fragmentation; accommodates most real-world queries.
	maxRecursionDepth     = 10   // maximum CNAME chain depth
	maxConcurrentRequests = 1000 // maximum concurrent DNS request handlers
)

// dnsCacheEntry represents a cached DNS response with metadata
type dnsCacheEntry struct {
	msg      *dns.Msg  // parsed DNS message
	cachedAt time.Time // when the response was cached
	minTTL   uint32    // minimum TTL from Answer records
}

// DNSResolver implements a basic DNS resolver with conditional forwarding
type DNSResolver struct {
	logger       logger.Logger
	config       DNSConfig
	conn4        *net.UDPConn // IPv4 listener
	conn6        *net.UDPConn // IPv6 listener
	protocol     string
	dialer       func(ctx context.Context, network, address string) (net.Conn, error)
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	mu           sync.RWMutex
	running      bool
	queryTimeout time.Duration
	once         sync.Once
	bufferPool   sync.Pool
	cache        cache.Cache
	requestSem   chan struct{}
}

// New creates a new DNS resolver instance
func New(ctx context.Context, logger logger.Logger, config DNSConfig) (*DNSResolver, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid DNS config: %w", err)
	}

	timeout, err := time.ParseDuration(config.QueryTimeout)
	if err != nil {
		return nil, fmt.Errorf("invalid query timeout: %w", err)
	}

	// Create resolver-scoped context for proper lifecycle management
	resolverCtx, resolverCancel := context.WithCancel(ctx)

	s := &DNSResolver{
		ctx:          resolverCtx,
		cancel:       resolverCancel,
		logger:       logger.WithPrefix("[dns]"),
		config:       config,
		queryTimeout: timeout,
		bufferPool: sync.Pool{
			New: func() any {
				return make([]byte, dnsPacketSize)
			},
		},
		cache:      cache.NewInMemory(resolverCtx, 5*time.Minute), // Use resolver context for cache lifecycle
		requestSem: make(chan struct{}, maxConcurrentRequests),
	}

	s.protocol = config.DefaultProtocol
	if s.protocol == "" {
		s.protocol = "udp"
	}
	switch s.protocol {
	case "tcp", "udp":
	default:
		return nil, fmt.Errorf("invalid protocol: %s. must be either tcp or udp", s.protocol)
	}

	if config.DialContext != nil {
		s.dialer = config.DialContext
	} else {
		var d net.Dialer
		s.dialer = func(ctx context.Context, network, address string) (net.Conn, error) {
			return d.DialContext(ctx, network, address)
		}
	}

	return s, nil
}

// Start starts the DNS server
func (s *DNSResolver) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("DNS server is already running")
	}

	// Extract port from listen address (e.g., ":53" -> "53")
	_, port, err := net.SplitHostPort(s.config.ListenAddress)
	if err != nil {
		// If no port specified, assume default DNS port
		port = "53"
	}

	// Create IPv4 UDP listener
	addr4, err := net.ResolveUDPAddr("udp4", "0.0.0.0:"+port)
	if err != nil {
		return fmt.Errorf("failed to resolve IPv4 listen address: %w", err)
	}
	s.conn4, err = net.ListenUDP("udp4", addr4)
	if err != nil {
		return fmt.Errorf("failed to listen on IPv4 UDP: %w", err)
	}

	// Create IPv6 UDP listener
	addr6, err := net.ResolveUDPAddr("udp6", "[::]:"+port)
	if err != nil {
		s.conn4.Close() // Clean up IPv4 listener
		return fmt.Errorf("failed to resolve IPv6 listen address: %w", err)
	}
	s.conn6, err = net.ListenUDP("udp6", addr6)
	if err != nil {
		s.conn4.Close() // Clean up IPv4 listener
		return fmt.Errorf("failed to listen on IPv6 UDP: %w", err)
	}

	s.running = true

	// Start handlers for both IPv4 and IPv6
	s.wg.Add(2)
	go s.handleRequests(s.conn4, "IPv4")
	go s.handleRequests(s.conn6, "IPv6")

	s.logger.Info("DNS server started on %s (IPv4: %s, IPv6: %s)", s.config.ListenAddress, addr4, addr6)
	s.logger.Info("Managed domains: %v", s.config.ManagedDomains)
	s.logger.Info("Internal nameservers: %v", s.config.InternalNameservers)
	s.logger.Info("Upstream nameservers: %v", s.config.UpstreamNameservers)

	return nil
}

// Stop stops the DNS server
func (s *DNSResolver) Stop() error {
	s.once.Do(func() {
		s.mu.Lock()
		defer s.mu.Unlock()

		if !s.running {
			return
		}

		s.logger.Info("Stopping DNS server")
		s.running = false

		if s.cancel != nil {
			s.cancel()
		}

		if s.conn4 != nil {
			s.conn4.Close()
		}

		if s.conn6 != nil {
			s.conn6.Close()
		}

		if s.cache != nil {
			s.cache.Close()
		}

		s.wg.Wait()

		s.logger.Info("DNS server stopped")
	})
	return nil
}

// handleRequests handles incoming DNS requests
func (s *DNSResolver) handleRequests(conn *net.UDPConn, proto string) {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		// Get buffer from pool
		buffer := s.bufferPool.Get().([]byte)

		// Set read deadline to prevent blocking indefinitely
		conn.SetReadDeadline(time.Now().Add(time.Second))

		n, clientAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			// Return buffer to pool on error
			s.bufferPool.Put(buffer)
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue // Timeout is expected, continue listening
			}
			// Check if connection is closed (happens during shutdown)
			if strings.Contains(err.Error(), "use of closed network connection") {
				return
			}
			s.logger.Error("Failed to read %s UDP packet: %v from %s", proto, err, clientAddr)
			continue
		}

		// Acquire semaphore before spawning handler goroutine
		select {
		case s.requestSem <- struct{}{}:
			// Handle the DNS request in a goroutine, transferring buffer ownership
			go s.handleDNSRequest(conn, buffer, n, clientAddr)
		case <-s.ctx.Done():
			s.bufferPool.Put(buffer)
			return
		default:
			// Semaphore full, drop request
			s.bufferPool.Put(buffer)
			s.logger.Debug("DNS request dropped: concurrency limit reached")
		}
	}
}

// handleDNSRequest processes a single DNS request
func (s *DNSResolver) handleDNSRequest(conn *net.UDPConn, buffer []byte, dataLen int, clientAddr *net.UDPAddr) {
	defer s.bufferPool.Put(buffer)
	defer func() { <-s.requestSem }()

	data := buffer[:dataLen]
	if len(data) < 12 {
		s.logger.Debug("DNS packet too short")
		return
	}

	// Extract domain name from DNS query (simplified parsing)
	domain, queryType, err := s.parseSimpleDNSQuery(data)
	if err != nil {
		s.logger.Debug("Failed to parse DNS query: %v", err)
		return
	}

	s.logger.Debug("DNS query for %s (%s) from %s", domain, dns.TypeToString[queryType], clientAddr)

	// Create normalized cache key (case-insensitive domain)
	cacheKey := fmt.Sprintf("%s:%d", strings.ToLower(domain), queryType)

	// Check cache first (with nil guard and shutdown check)
	if s.cache != nil {
		// Check if shutdown is in progress before accessing cache
		select {
		case <-s.ctx.Done():
			// Shutdown in progress, skip cache and fall through to resolution
		default:
			if found, cachedResponse, err := s.cache.Get(cacheKey); err == nil && found {
				if cacheEntry, ok := cachedResponse.(*dnsCacheEntry); ok {
					s.logger.Debug("Cache hit for %s (%s)", domain, dns.TypeToString[queryType])

					// Update cached response with current transaction ID and remaining TTLs
					if updatedResponse := s.updateCachedResponse(cacheEntry, data); updatedResponse != nil {
						_, err := conn.WriteToUDP(updatedResponse, clientAddr)
						if err != nil {
							s.logger.Error("Failed to send cached DNS response: %v", err)
						}
						return
					}

					// If parsing/updating failed, fall back to normal resolution
					s.logger.Debug("Failed to update cached response for %s, falling back to normal resolution", domain)
				}
			}
		}
	}

	// Determine which nameservers to use
	nameservers := s.selectNameservers(domain)

	// Forward the query to appropriate nameservers
	s.forwardQuery(conn, data, domain, queryType, clientAddr, nameservers, cacheKey)
}

// parseSimpleDNSQuery extracts the domain name from a DNS query
func (s *DNSResolver) parseSimpleDNSQuery(data []byte) (string, uint16, error) {
	var m dns.Msg
	if err := m.Unpack(data); err != nil {
		return "", 0, fmt.Errorf("failed to unpack DNS query: %w", err)
	}
	if len(m.Question) == 0 {
		return "", 0, fmt.Errorf("empty question section")
	}
	q := m.Question[0]
	// Return the query name without a trailing dot for consistency in cache keys.
	return strings.TrimSuffix(q.Name, "."), q.Qtype, nil
}

func (s *DNSResolver) selectNameservers(target string) []string {
	// Determine nameservers for CNAME target
	var nameservers []string
	if s.config.IsManagedDomain(target) {
		if len(s.config.InternalNameservers) > 1 {
			// load balance between internal nameservers
			nameservers = make([]string, len(s.config.InternalNameservers))
			copy(nameservers, s.config.InternalNameservers)
			rand.Shuffle(len(nameservers), func(i, j int) {
				nameservers[i], nameservers[j] = nameservers[j], nameservers[i]
			})
		} else {
			nameservers = s.config.InternalNameservers
		}
	} else {
		nameservers = s.config.UpstreamNameservers
	}
	return nameservers
}

// forwardQuery forwards a DNS query to nameservers
func (s *DNSResolver) forwardQuery(conn *net.UDPConn, originalData []byte, domain string, queryType uint16, clientAddr *net.UDPAddr, nameservers []string, cacheKey string) {
	for _, ns := range nameservers {
		s.logger.Debug("sending DNS query for %s (%s) to %s", domain, dns.TypeToString[queryType], ns)
		response, err := s.queryNameserver(s.ctx, originalData, ns)
		if err != nil {
			s.logger.Debug("Failed to query %s: %v", ns, err)
			continue
		}
		if response != nil {
			s.debugDNSResponse(response, domain, queryType)

			// Check if we need to recursively resolve CNAME
			if needsRecursion, cnameTarget := s.needsCNAMEResolution(response, queryType); needsRecursion {
				s.logger.Debug("Response contains CNAME without final record, recursively resolving %s", cnameTarget)

				// Determine nameservers for CNAME target
				cnameNameservers := s.selectNameservers(cnameTarget)

				// Recursively resolve the CNAME
				finalResponse, err := s.resolveCNAMERecursively(s.ctx, cnameTarget, queryType, cnameNameservers, 1)
				if err != nil {
					s.logger.Error("Failed to resolve CNAME chain: %v", err)
					// Fall back to sending the partial CNAME response
				} else if finalResponse != nil {
					// Merge CNAME records with final response
					finalResponse.MsgHdr = response.MsgHdr     // Preserve original header fields and transaction ID
					finalResponse.Question = response.Question // Preserve original question
					finalResponse.Answer = append(response.Answer, finalResponse.Answer...)
					response = finalResponse
					s.debugDNSResponse(response, domain, queryType)
				}
			}

			// Pack the response back to raw bytes
			responseBytes, err := response.Pack()
			if err != nil {
				s.logger.Error("failed to pack DNS response: %s", err)
				return
			}

			// Cache the response using the minimum TTL from all answers
			s.cacheResponse(cacheKey, responseBytes, response, domain, queryType)

			// Send response back to client
			_, err = conn.WriteToUDP(responseBytes, clientAddr)
			if err != nil {
				s.logger.Error("Failed to send DNS response: %v", err)
			}
			return
		}
	}

	s.logger.Debug("DNS returned error for %s (%s) to %s", domain, dns.TypeToString[queryType], clientAddr)

	// If we get here, all nameservers failed - send error response
	s.sendErrorResponse(conn, originalData, clientAddr)
}

// queryNameserver queries a specific nameserver, first trying UDP with EDNS0, then falling back to TCP if truncated
func (s *DNSResolver) queryNameserver(ctx context.Context, request []byte, nameserver string) (*dns.Msg, error) {
	// Parse the DNS message from raw bytes
	msg := &dns.Msg{}
	err := msg.Unpack(request)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DNS message: %w", err)
	}

	// Set EDNS0 with 4096 byte UDP payload size to avoid truncation
	msg.SetEdns0(4096, true)

	// Try the configured protocol first
	conn, err := s.dialer(ctx, s.protocol, nameserver)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to nameserver via %s: %w", strings.ToUpper(s.protocol), err)
	}
	co := &dns.Conn{Conn: conn}

	// Set deadline for operation
	conn.SetDeadline(time.Now().Add(s.queryTimeout))

	if err := co.WriteMsg(msg); err != nil {
		co.Close()
		return nil, fmt.Errorf("failed to write DNS message via %s: %w", strings.ToUpper(s.protocol), err)
	}
	response, err := co.ReadMsg()
	if err != nil {
		co.Close()
		return nil, fmt.Errorf("failed to read DNS response via %s: %w", strings.ToUpper(s.protocol), err)
	}

	// Check if response was truncated (TC bit set) and we're using UDP
	if response.Truncated && s.protocol == "udp" {
		// Close UDP connection before switching to TCP
		co.Close()

		// Retry over TCP
		tcpConn, err := s.dialer(ctx, "tcp", nameserver)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to nameserver via TCP: %w", err)
		}
		tcpCo := &dns.Conn{Conn: tcpConn}
		defer tcpCo.Close()

		// Set deadline for TCP operation
		tcpConn.SetDeadline(time.Now().Add(s.queryTimeout))

		if err := tcpCo.WriteMsg(msg); err != nil {
			return nil, fmt.Errorf("failed to write DNS message via TCP: %w", err)
		}
		response, err = tcpCo.ReadMsg()
		if err != nil {
			return nil, fmt.Errorf("failed to read DNS response via TCP: %w", err)
		}
	} else {
		// Close the connection on successful non-truncated response
		co.Close()
	}

	return response, nil
}

// sendErrorResponse sends a DNS error response
func (s *DNSResolver) sendErrorResponse(conn *net.UDPConn, originalData []byte, clientAddr *net.UDPAddr) {
	if len(originalData) < 12 {
		return
	}

	// Use buffer pool if original data fits, otherwise allocate
	var response []byte

	if len(originalData) <= dnsPacketSize {
		buffer := s.bufferPool.Get().([]byte)
		defer s.bufferPool.Put(buffer)
		response = buffer[:len(originalData)]
	} else {
		response = make([]byte, len(originalData))
	}

	// Create error response by modifying the original query
	copy(response, originalData)

	// Set response bit and error code (SERVFAIL = 2)
	response[2] |= 0x80                       // Set QR bit (response)
	response[3] = (response[3] & 0xF0) | 0x02 // Set RCODE to SERVFAIL

	_, err := conn.WriteToUDP(response, clientAddr)
	if err != nil {
		s.logger.Error("Failed to send error response: %v to %s", err, clientAddr)
	}
}

// IsRunning returns whether the DNS server is currently running
func (s *DNSResolver) IsRunning() bool {
	s.mu.RLock()
	val := s.running
	s.mu.RUnlock()
	return val
}

// updateCachedResponse updates cached DNS response with current transaction ID and remaining TTLs
func (s *DNSResolver) updateCachedResponse(cacheEntry *dnsCacheEntry, originalRequest []byte) []byte {
	// Extract transaction ID from original request
	if len(originalRequest) < 2 {
		s.logger.Error("Original DNS request too short to extract transaction ID")
		return nil
	}
	originalID := binary.BigEndian.Uint16(originalRequest[0:2])

	// Clone the cached message to avoid modifying the original
	msg := cacheEntry.msg.Copy()

	// Calculate remaining TTL
	now := time.Now()
	elapsed := uint32(now.Sub(cacheEntry.cachedAt).Seconds())
	remaining := uint32(0)
	if elapsed < cacheEntry.minTTL {
		remaining = cacheEntry.minTTL - elapsed
	}

	// If TTL has expired, don't use cached response
	if remaining == 0 {
		s.logger.Debug("Cached response has expired, not using cache")
		return nil
	}

	// Update TTL for all RRs in all sections
	for _, rr := range msg.Answer {
		rr.Header().Ttl = remaining
	}
	for _, rr := range msg.Ns {
		rr.Header().Ttl = remaining
	}
	for _, rr := range msg.Extra {
		rr.Header().Ttl = remaining
	}

	// Update transaction ID to match original request
	msg.Id = originalID

	// Re-encode the message
	updatedBytes, err := msg.Pack()
	if err != nil {
		s.logger.Error("Failed to re-encode DNS response with updated ID and TTLs: %v", err)
		return nil
	}

	return updatedBytes
}

// cacheResponse caches a DNS response using the minimum TTL from Answer records only
func (s *DNSResolver) cacheResponse(cacheKey string, responseBytes []byte, response *dns.Msg, domain string, queryType uint16) {
	// Only cache positive successful answers
	if response.Rcode != dns.RcodeSuccess || len(response.Answer) == 0 {
		s.logger.Debug("Not caching DNS response for %s (%s): Rcode=%d, Answers=%d",
			domain, dns.TypeToString[queryType], response.Rcode, len(response.Answer))
		return
	}

	// Find minimum TTL from Answer records only
	var minTTL uint32
	for i, answer := range response.Answer {
		ttl := answer.Header().Ttl
		if i == 0 || ttl < minTTL {
			minTTL = ttl
		}
	}

	// Only cache if we have a valid TTL and cache is available
	if minTTL > 0 && s.cache != nil {
		// Create cache entry with metadata
		cacheEntry := &dnsCacheEntry{
			msg:      response.Copy(), // Store a copy of the parsed message
			cachedAt: time.Now(),
			minTTL:   minTTL,
		}

		// Convert TTL to duration for cache expiration
		cacheDuration := time.Duration(minTTL) * time.Second

		// Store in cache
		if err := s.cache.Set(cacheKey, cacheEntry, cacheDuration); err != nil {
			s.logger.Error("Failed to cache DNS response for %s: %v", domain, err)
		} else {
			s.logger.Debug("Cached DNS response for %s (%s) with TTL %d seconds", domain, dns.TypeToString[queryType], minTTL)
		}
	}
}

func (s *DNSResolver) debugDNSResponse(msg *dns.Msg, domain string, queryType uint16) {
	s.logger.Debug("DNS query %s (%v) response: %s", domain, dns.TypeToString[queryType], msg)
}

// needsCNAMEResolution checks if a response contains only CNAME records without final A/AAAA records
func (s *DNSResolver) needsCNAMEResolution(msg *dns.Msg, originalQueryType uint16) (bool, string) {

	// Only resolve CNAMEs for A and AAAA queries
	if originalQueryType != dns.TypeA && originalQueryType != dns.TypeAAAA {
		return false, ""
	}

	if len(msg.Answer) == 0 {
		return false, ""
	}

	hasCNAME := false
	hasTargetRecord := false
	var cnameTarget string

	for _, answer := range msg.Answer {
		switch rr := answer.(type) {
		case *dns.CNAME:
			hasCNAME = true
			cnameTarget = rr.Target
		case *dns.A, *dns.AAAA:
			hasTargetRecord = true
		}
	}

	// Need resolution if we have CNAME but no final A/AAAA record
	needsResolution := hasCNAME && !hasTargetRecord
	if needsResolution {
		return true, cnameTarget
	}
	return false, ""
}

// resolveCNAMERecursively follows CNAME chains to get the final A/AAAA record
func (s *DNSResolver) resolveCNAMERecursively(ctx context.Context, domain string, queryType uint16, nameservers []string, depth int) (*dns.Msg, error) {
	if depth >= maxRecursionDepth {
		return nil, fmt.Errorf("max recursion depth reached for CNAME chain")
	}

	// Create a DNS query for the CNAME target
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(domain), queryType)
	msg.RecursionDesired = true

	packed, err := msg.Pack()
	if err != nil {
		return nil, fmt.Errorf("failed to pack DNS query: %w", err)
	}

	// Query nameservers
	for _, ns := range nameservers {
		s.logger.Debug("resolving CNAME %s (%s) via %s (depth: %d)", domain, dns.TypeToString[queryType], ns, depth)

		response, err := s.queryNameserver(ctx, packed, ns)
		if err != nil {
			s.logger.Debug("Failed to query %s: %v", ns, err)
			continue
		}

		if response == nil {
			s.logger.Debug("Received nil response from %s for %s", ns, domain)
			continue
		}

		s.logger.Debug("CNAME recursive query for %s got response: %s", domain, response)

		// Check response status
		if response.Rcode != dns.RcodeSuccess {
			s.logger.Debug("CNAME resolution for %s failed with rcode %s", domain, dns.RcodeToString[response.Rcode])
			continue
		}

		// Check if we need to recurse further
		if needsRecursion, cnameTarget := s.needsCNAMEResolution(response, queryType); needsRecursion {
			s.logger.Debug("CNAME %s points to %s, recursing", domain, cnameTarget)

			// Determine nameservers for this CNAME target using selectNameservers for proper load balancing
			nextNameservers := s.selectNameservers(cnameTarget)

			// Recurse to resolve the CNAME target
			finalResponse, err := s.resolveCNAMERecursively(ctx, cnameTarget, queryType, nextNameservers, depth+1)
			if err != nil {
				s.logger.Debug("Failed to resolve CNAME recursively: %v", err)
				continue
			}

			// Merge the CNAME record(s) from current response with final response
			if finalResponse != nil {
				// Prepend CNAME records to the final answer
				finalResponse.Answer = append(response.Answer, finalResponse.Answer...)
				return finalResponse, nil
			}
		}

		return response, nil
	}

	return nil, fmt.Errorf("all nameservers failed")
}
