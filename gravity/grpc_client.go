package gravity

import (
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	mathrand "math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agentuity/go-common/dns"
	"github.com/agentuity/go-common/gravity/network"
	pb "github.com/agentuity/go-common/gravity/proto"
	"github.com/agentuity/go-common/gravity/provider"
	"github.com/agentuity/go-common/logger"
	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	grpcgzip "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// contextKey is a private type for context value keys to avoid collisions.
type contextKey int

const (
	machineIDKey contextKey = iota
	streamIDKey
)

// Initialize gzip compression settings during package initialization
func init() {
	// Set gzip compression level for optimal text compression
	// Using BestSpeed for control messages to balance compression ratio and CPU usage
	if err := grpcgzip.SetLevel(gzip.BestSpeed); err != nil {
		// Log error but don't panic during initialization
		fmt.Printf("Warning: failed to set gzip compression level: %v\n", err)
	}
}

// Error variables for consistency with old implementation
var ErrConnectionClosed = errors.New("gravity connection closed")

const (
	// debugTunnelPackets gates verbose per-packet tunnel logging.
	// Keep false in production to avoid high-volume log spam.
	debugTunnelPackets = false

	// DefaultMaxGravityPeers is the maximum number of Gravity servers
	// a single Hadron will connect to simultaneously.
	DefaultMaxGravityPeers = 3

	// DefaultPeerDiscoveryInterval controls how often DNS is re-resolved
	// to discover additional Gravity peers.
	DefaultPeerDiscoveryInterval = 30 * time.Minute

	// DefaultPeerCycleInterval controls how often one connection is rotated
	// when more Gravity peers are available than active connections.
	DefaultPeerCycleInterval = 2 * time.Hour

	// DefaultStreamsPerGravity is the number of tunnel streams established
	// per Gravity server connection.
	DefaultStreamsPerGravity = 2

	// DefaultBindingTTL is how long a flow stays pinned to the same Gravity.
	// Must be at least as long as ion's NAT table idle timeout (10 min) so
	// response packets route back to the ion that created the NAT entry.
	// With 5s TTL, any response after 5s went to a random ion → "unknown
	// ipv6 destination" → RST, breaking HTTP/2 connection reuse and
	// long-lived connections.
	DefaultBindingTTL = 10 * time.Minute
)

// TunnelKeepaliveMarker is a 4-byte magic prefix for tunnel keepalive packets.
// When ion receives a packet starting with this marker, it echoes it back
// immediately without writing to the TUN. This provides a positive data-plane
// liveness signal on each tunnel stream, detecting zombie streams and keeping
// idle connections active.
var TunnelKeepaliveMarker = []byte{0x41, 0x47, 0x4E, 0x54} // "AGNT"

// IsTunnelKeepalive checks if a packet is a tunnel keepalive probe.
func IsTunnelKeepalive(data []byte) bool {
	return len(data) >= 4 &&
		data[0] == TunnelKeepaliveMarker[0] &&
		data[1] == TunnelKeepaliveMarker[1] &&
		data[2] == TunnelKeepaliveMarker[2] &&
		data[3] == TunnelKeepaliveMarker[3]
}

// isContextCanceled checks if an error is due to context cancellation
// This includes both direct context.Canceled errors and gRPC Canceled status codes
func isContextCanceled(ctx context.Context, err error) bool {
	if ctx.Err() == context.Canceled {
		return true
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	if st, ok := status.FromError(err); ok && st.Code() == codes.Canceled {
		return true
	}
	return false
}

// ConnectionPoolConfig holds configuration for gRPC connection pool optimization
type ConnectionPoolConfig struct {
	// Connection pool size (4-8 connections as per PLAN.md)
	PoolSize int

	// Streams per connection for packet multiplexing
	StreamsPerConnection int

	// Stream allocation strategy
	AllocationStrategy StreamAllocationStrategy

	// Health check and failover settings
	HealthCheckInterval time.Duration
	FailoverTimeout     time.Duration

	// MaxGravityPeers caps how many Gravity servers Hadron connects to.
	// Default: DefaultMaxGravityPeers (3).
	MaxGravityPeers int

	// StreamsPerGravity is tunnel streams per Gravity host.
	// Default: DefaultStreamsPerGravity (2).
	StreamsPerGravity int
}

// StreamAllocationStrategy defines how streams are selected for load distribution
type StreamAllocationStrategy int

const (
	RoundRobin StreamAllocationStrategy = iota
	HashBased
	LeastConnections
	WeightedRoundRobin
)

// routeSandboxResult wraps a sandbox route response or error
type routeSandboxResult struct {
	Response *pb.RouteSandboxResponse
	Error    string
}

// routeDeploymentResult wraps a deployment route response or error
type routeDeploymentResult struct {
	Response *pb.RouteDeploymentResponse
	Error    string
}

// checkpointURLResult wraps a checkpoint URL response or error
type checkpointURLResult struct {
	Response *pb.CheckpointURLResponse
	Error    string
}

// GravityEndpoint represents a connection to a single Gravity server.
type GravityEndpoint struct {
	// URL is the address of this Gravity server.
	URL string

	// TLSServerName is the hostname to use for TLS SNI when the URL contains
	// an IP address (e.g., after DNS resolution). If empty, the hostname is
	// extracted from URL as usual.
	TLSServerName string

	// healthy indicates whether this endpoint is currently reachable.
	healthy atomic.Bool

	// lastHeartbeat is the Unix timestamp of the last successful health check.
	lastHeartbeat atomic.Int64
}

// IsHealthy returns true if the endpoint is currently reachable.
func (e *GravityEndpoint) IsHealthy() bool {
	return e.healthy.Load()
}

// GravityClient implements the provider.Server interface using gRPC transport
type GravityClient struct {
	// Configuration
	context            context.Context
	logger             logger.Logger
	provider           provider.Provider
	url                string
	gravityURLs        []string
	ecdsaPrivateKey    *ecdsa.PrivateKey
	instanceID         string
	authorizationToken string
	ip4Address         string
	ip6Address         string
	clientVersion      string
	clientName         string
	capabilities       *pb.ClientCapabilities
	hostInfo           *pb.HostInfo
	workingDir         string

	// Session response fields
	machineID string

	// Connection pool configuration
	poolConfig ConnectionPoolConfig

	// gRPC connections and streams
	connections    []*grpc.ClientConn // Connection pool (4-8 connections)
	sessionClients []pb.GravitySessionServiceClient
	streamManager  *StreamManager

	// Multi-endpoint support: one endpoint per Gravity server
	endpoints         []*GravityEndpoint
	endpointsMu       sync.RWMutex
	selector          *EndpointSelector
	useMultiConnect   bool
	multiEndpointMode atomic.Bool // true when client is effectively in multi-endpoint mode

	// Peer discovery and cycling
	discoveryResolveFunc  func() []string
	peerDiscoveryInterval time.Duration
	peerCycleInterval     time.Duration
	lastCycleTime         atomic.Int64  // unix timestamp of last cycle
	peerDiscoveryWake     chan struct{} // non-blocking signal to wake the discovery loop immediately

	// dnsLookupMulti is the function used to resolve hostnames during
	// endpoint re-resolution. Defaults to dns.DefaultDNS.LookupMulti.
	// Overridable in tests.
	dnsLookupMulti func(ctx context.Context, hostname string) (bool, []net.IP, error)

	// probeRotation tracks which stream within each endpoint to probe next.
	// Incremented each cycle so all streams are covered within the stale timeout.
	probeRotation atomic.Int64

	// healthProbe is the function used to check if a gravity endpoint is
	// alive before connecting. Defaults to probeHealthEndpoint (HTTP probe).
	// Overridable in tests to avoid real network calls.
	healthProbe func(host, port string) bool

	// Per-connection endpoint URL for multi-endpoint routing.
	connectionURLs []string

	// Endpoint URL -> tunnel stream indices.
	endpointStreamIndices map[string][]int
	bindingCleanupOnce    sync.Once

	// Fault tolerance
	retryConfig     RetryConfig
	circuitBreakers []*CircuitBreaker // One per connection

	// Connection management
	mu                         sync.RWMutex
	closing                    bool
	ctx                        context.Context
	cancel                     context.CancelFunc
	connectionCtx              context.Context
	connectionCancel           context.CancelFunc
	tlsCert                    *tls.Certificate
	skipAutoReconnect          bool
	closed                     chan struct{}
	maxReconnectAttempts       int
	reconnectAttemptTimeout    time.Duration
	reconnectionFailedCallback func(attempts int, lastErr error)
	onEndpointReady            func(endpointIndex int)

	// State management
	connected            bool
	reconnecting         bool          // Tracks if reconnection is in progress
	endpointReconnecting []atomic.Bool // Per-endpoint reconnect guard (multi-endpoint only)
	endpointFailCount    []atomic.Int32
	connectionIDs        []string      // Stores connection IDs from server responses
	connectionIDChan     chan string   // Channel to signal when connection ID is received
	helloAckedStreams    sync.Map      // streamIndex (int) → true: tracks which streams received SessionHelloResponse
	sessionReady         chan struct{} // Closed when session is fully authenticated and configured
	otlpURL              string
	otlpToken            string
	apiURL               string
	hostMapping          []*pb.HostMapping
	hostEnvironment      []string
	// Preserved session config fields — multi-tunnel reuse sessions send
	// minimal responses without these. We store the values from the primary
	// session and reuse them when subsequent responses are empty.
	subnetRoutes      []string
	machineSubnet     string
	sessionHostname   string
	sessionOrgID      string
	machineToken      string
	machineCertBundle string
	sshPublicKey      []byte
	signingPublicKey  []byte
	once              sync.Once

	// Messaging
	evacuationCallback    func()
	monitorCommandHandler func(cmd *pb.MonitorCommand) // Monitor command handler (set via SetMonitorCommandHandler)
	handlerWg             sync.WaitGroup               // tracks in-flight checkpoint/restore goroutines
	response              chan *pb.ProtocolResponse    // Reserved for legacy response fan-out; intentionally kept for API compatibility.
	pending               map[string]chan *pb.ProtocolResponse
	pendingMu             sync.RWMutex

	// Route deployment responses
	pendingRouteDeployment   map[string]chan routeDeploymentResult
	pendingRouteDeploymentMu sync.RWMutex

	// Route sandbox responses
	pendingRouteSandbox   map[string]chan routeSandboxResult
	pendingRouteSandboxMu sync.RWMutex

	// Checkpoint URL responses
	pendingCheckpointURL   map[string]chan checkpointURLResult
	pendingCheckpointURLMu sync.RWMutex

	// Packet channels for network device multiplexing
	droppedPackets  atomic.Int64 // counts consecutive dropped inbound packets
	inboundPackets  chan *PooledBuffer
	outboundPackets chan []byte
	textMessages    chan *PooledBuffer // Reserved for future text-message handling; currently drained to return pooled buffers.

	// ── Tunnel health counters (lock-free, read by external monitor) ──

	// Inbound: gravity → hadron → container
	inboundReceived  atomic.Uint64 // packets received from gravity streams
	inboundDelivered atomic.Uint64 // packets delivered via ProcessInPacket
	inboundDropped   atomic.Uint64 // lifetime drops (channel full)
	inboundBytes     atomic.Uint64 // bytes received from gravity

	// Outbound: container → hadron → gravity
	outboundReceived atomic.Uint64 // packets from TUN (ProcessOutPacket called)
	outboundSent     atomic.Uint64 // packets sent via sendTunnelPacket
	outboundErrors   atomic.Uint64 // send failures
	outboundBytes    atomic.Uint64 // bytes sent to gravity

	// Control plane
	pingsSent      atomic.Uint64
	pongsReceived  atomic.Uint64
	pingTimeouts   atomic.Uint64
	lastPingSentUs atomic.Int64 // unix microseconds
	lastPongRecvUs atomic.Int64 // unix microseconds

	// Channel monitoring
	inboundHighWater atomic.Int32 // max channel depth since last reset

	// agentuity internal certificate
	caCert            string
	defaultServerName string

	// Buffer pool for memory efficiency
	bufferPool sync.Pool

	// heartbeat configuration
	pingInterval time.Duration

	// otel
	tracer trace.Tracer

	// trace packet logging
	tracePackets      bool
	tracePacketLogger logger.Logger
	traceLog          bool // cached g.logger.IsTraceEnabled() — avoids per-call interface dispatch on hot paths

	// network interface for routing
	networkInterface network.NetworkInterface
}

// StreamInfo tracks individual stream health and load
type StreamInfo struct {
	stream    pb.GravitySessionService_StreamSessionPacketsClient
	connIndex int        // Connection index this stream belongs to
	streamID  string     // Unique stream identifier
	isHealthy bool       // Stream health status
	loadCount int64      // Current load (packets in flight)
	lastUsed  time.Time  // Last time this stream was used
	sendMu    sync.Mutex // Serializes Send calls on this stream
}

const endpointDiscoveryRefreshFailureThreshold int32 = 10

// StreamManager manages multiple gRPC streams for multiplexing with advanced load balancing
type StreamManager struct {
	// Control streams (one per connection) - using session service
	controlStreams []pb.GravitySessionService_EstablishSessionClient
	controlSendMu  []sync.Mutex // Per-stream send serialization (gRPC Send is not concurrent-safe)
	controlMu      sync.RWMutex

	// Enhanced tunnel stream management
	tunnelStreams      []*StreamInfo // Enhanced stream tracking
	tunnelMu           sync.RWMutex
	nextTunnelIndex    int // Round-robin selection
	allocationStrategy StreamAllocationStrategy

	// Stream health and load balancing
	streamMetrics map[string]*StreamMetrics // Stream performance metrics
	metricsMu     sync.RWMutex

	// Connection health tracking
	connectionHealth    []bool // Health status per connection
	connectionIdleCount []int  // Consecutive IDLE health check count per connection
	healthMu            sync.RWMutex

	// Stream contexts for cancellation
	contexts []context.Context
	cancels  []context.CancelFunc
}

// StreamMetrics tracks performance metrics for individual streams
type StreamMetrics struct {
	PacketsSent     int64
	PacketsReceived int64
	BytesSent       int64
	BytesReceived   int64
	LastLatency     time.Duration
	ErrorCount      int64
	LastError       time.Time
	LastSendUs      int64 // unix microseconds of last successful send
	LastRecvUs      int64 // unix microseconds of last successful receive
	LastProbeSentUs int64 // unix microseconds of last keepalive probe sent (probe-only, not data traffic)
}

// New creates a new gRPC-based Gravity server client
func New(config GravityConfig) (*GravityClient, error) {
	if config.ECDSAPrivateKey == nil {
		return nil, fmt.Errorf("ECDSAPrivateKey is required")
	}
	if config.InstanceID == "" {
		return nil, fmt.Errorf("InstanceID is required")
	}

	selfSignedCert, err := createSelfSignedTLSConfig(config.ECDSAPrivateKey, config.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("error creating TLS configuration: %w", err)
	}

	// Get host information
	hostInfo, err := getHostInfo(config)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(config.Context)

	poolConfig := config.ConnectionPoolConfig
	if poolConfig == nil {
		// Default connection pool configuration
		poolConfig = &ConnectionPoolConfig{
			PoolSize:             4,                  // Start with 4 connections (PLAN.md: 4-8)
			StreamsPerConnection: 2,                  // 2 tunnel streams per connection
			AllocationStrategy:   WeightedRoundRobin, // Use weighted round robin for load balancing
			HealthCheckInterval:  30 * time.Second,   // Health check every 30 seconds
			FailoverTimeout:      5 * time.Second,    // 5 second failover timeout
		}
	}

	// Default retry configuration for fault tolerance
	retryConfig := DefaultRetryConfig()

	g := &GravityClient{
		context:                config.Context,
		logger:                 config.Logger,
		provider:               config.Provider,
		url:                    config.URL,
		gravityURLs:            append([]string(nil), config.GravityURLs...),
		useMultiConnect:        config.UseMultiConnect,
		discoveryResolveFunc:   config.DiscoveryResolveFunc,
		peerDiscoveryInterval:  config.PeerDiscoveryInterval,
		peerCycleInterval:      config.PeerCycleInterval,
		peerDiscoveryWake:      make(chan struct{}, 1),
		ecdsaPrivateKey:        config.ECDSAPrivateKey,
		instanceID:             config.InstanceID,
		ip4Address:             config.IP4Address,
		ip6Address:             config.IP6Address,
		clientVersion:          config.ClientVersion,
		clientName:             config.ClientName,
		capabilities:           config.Capabilities,
		networkInterface:       config.NetworkInterface,
		hostInfo:               hostInfo,
		poolConfig:             *poolConfig,
		retryConfig:            retryConfig,
		ctx:                    ctx,
		cancel:                 cancel,
		tlsCert:                selfSignedCert,
		caCert:                 config.CACert,
		defaultServerName:      config.DefaultServerName,
		connectionIDs:          make([]string, 0, poolConfig.PoolSize),
		connectionIDChan:       make(chan string, max(1, poolConfig.PoolSize*len(config.GravityURLs))),
		sessionReady:           make(chan struct{}),
		response:               make(chan *pb.ProtocolResponse, 100),
		pending:                make(map[string]chan *pb.ProtocolResponse),
		pendingRouteDeployment: make(map[string]chan routeDeploymentResult),
		pendingRouteSandbox:    make(map[string]chan routeSandboxResult),
		pendingCheckpointURL:   make(map[string]chan checkpointURLResult),
		endpointStreamIndices:  make(map[string][]int),
		inboundPackets:         make(chan *PooledBuffer, 1000),
		outboundPackets:        make(chan []byte, 1000),
		textMessages:           make(chan *PooledBuffer, 100),
		pingInterval:           config.PingInterval,
		streamManager: &StreamManager{
			streamMetrics:      make(map[string]*StreamMetrics),
			allocationStrategy: poolConfig.AllocationStrategy,
		},
		workingDir:                 config.WorkingDir,
		tracePackets:               config.TraceLogPackets,
		skipAutoReconnect:          config.SkipAutoReconnect,
		closed:                     make(chan struct{}, 1),
		maxReconnectAttempts:       config.MaxReconnectAttempts,
		reconnectAttemptTimeout:    config.ReconnectAttemptTimeout,
		reconnectionFailedCallback: config.ReconnectionFailedCallback,
		onEndpointReady:            config.OnEndpointReady,
		tracer:                     otel.Tracer("@agentuity/gravity/client"),
	}
	g.connectionCtx, g.connectionCancel = context.WithCancel(ctx)

	if config.TraceLogPackets {
		g.tracePacketLogger = logger.NewConsoleLogger()
	}
	g.traceLog = g.logger.IsTraceEnabled()

	// Initialize buffer pool
	g.bufferPool.New = func() any {
		return make([]byte, maxBufferSize)
	}

	return g, nil
}

func (g *GravityClient) ensureConnectionContextLocked() {
	if g.connectionCancel != nil {
		g.connectionCancel()
	}
	g.connectionCtx, g.connectionCancel = context.WithCancel(g.ctx)
}

func (g *GravityClient) currentConnectionContext() context.Context {
	g.mu.RLock()
	ctx := g.connectionCtx
	base := g.ctx
	g.mu.RUnlock()
	if ctx == nil {
		if base == nil {
			return context.Background()
		}
		return base
	}
	return ctx
}

// Start establishes gRPC connections and starts the client.
// When GravityURLs is configured with multiple URLs, the multi-endpoint path
// is used (multiple Gravity servers, sticky tunnel selection, peer cycling).
// Otherwise, the original single-URL path is used — identical to pre-multi-tunnel behavior.
func (g *GravityClient) Start() error {
	g.logger.Debug("starting gRPC Gravity client...")

	// Clear stale hello acks from any previous session attempt so
	// establishTunnelStreams only sees acks from this session.
	g.helloAckedStreams.Range(func(key, _ any) bool {
		g.helloAckedStreams.Delete(key)
		return true
	})

	g.mu.Lock()

	if g.connected {
		g.mu.Unlock()
		return fmt.Errorf("already connected")
	}

	g.ensureConnectionContextLocked()

	// Multi-endpoint mode: opt-in via GravityURLs with >1 URL.
	if len(g.gravityURLs) > 1 || g.useMultiConnect {
		g.mu.Unlock()
		return g.startMultiEndpoint()
	}

	// ── Original single-URL path (unchanged) ──────────────────────────────

	g.logger.Debug("parsing gRPC URL: %s", g.url)
	// Parse gRPC URL
	grpcURL, err := g.parseGRPCURL(g.url)
	if err != nil {
		g.logger.Error("failed to parse gRPC URL: %v", err)
		return fmt.Errorf("invalid URL: %w", err)
	}
	g.logger.Debug("parsed gRPC URL successfully: %s", grpcURL)

	// Extract hostname for TLS ServerName
	hostname, err := g.extractHostnameFromURL(g.url)
	if err != nil {
		g.logger.Error("failed to extract hostname from URL: %v", err)
		return fmt.Errorf("failed to extract hostname: %w", err)
	}

	g.logger.Debug("loading CA certificate for TLS...")
	pool, err := x509.SystemCertPool()
	if err != nil {
		g.logger.Warn("failed to load system cert pool, using empty pool: %v", err)
		pool = x509.NewCertPool()
	}
	if ok := pool.AppendCertsFromPEM([]byte(g.caCert)); !ok {
		g.logger.Error("failed to load embedded CA certificate")
		return fmt.Errorf("failed to load embedded CA certificate")
	}

	g.logger.Debug("creating TLS configuration...")
	if g.tlsCert == nil {
		g.logger.Error("failed to load TLS certificate")
		return fmt.Errorf("failed to load TLS certificate, self-signed cert was nil")
	}
	cert := *g.tlsCert

	// Create TLS config that includes both client certificates (mTLS) and server verification
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert}, // Client certificates for mTLS (self-signed)
		ServerName:   hostname,                // Dynamic server name for SNI and verification
		MinVersion:   tls.VersionTLS13,
		// GetClientCertificate ensures the client always sends its cert when requested
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			if g.tlsCert != nil {
				return g.tlsCert, nil
			}
			return nil, fmt.Errorf("no client certificate available")
		},
		RootCAs: pool,
	}

	creds := credentials.NewTLS(tlsConfig)
	g.logger.Debug("TLS credentials created successfully")

	// Create connection pool using configuration
	connectionCount := g.poolConfig.PoolSize
	g.logger.Debug("creating connection pool with %d connections", connectionCount)
	g.connections = make([]*grpc.ClientConn, connectionCount)
	g.sessionClients = make([]pb.GravitySessionServiceClient, connectionCount)
	g.circuitBreakers = make([]*CircuitBreaker, connectionCount)

	// Initialize connection health tracking
	g.streamManager.connectionHealth = make([]bool, connectionCount)
	g.streamManager.connectionIdleCount = make([]int, connectionCount)
	for i := range g.streamManager.connectionHealth {
		g.streamManager.connectionHealth[i] = true // Start assuming healthy
	}

	g.logger.Debug("establishing %d gRPC connections to %s", connectionCount, grpcURL)
	// Establish connections using modern gRPC client API
	for i := range connectionCount {
		g.logger.Debug("establishing connection %d/%d...", i+1, connectionCount)

		g.logger.Debug("creating gRPC client %d to %s with TLS", i+1, grpcURL)
		conn, err := grpc.NewClient(grpcURL,
			grpc.WithTransportCredentials(creds),
			grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
			grpc.WithInitialWindowSize(1<<20),
			grpc.WithInitialConnWindowSize(4<<20),
			grpc.WithKeepaliveParams(keepalive.ClientParameters{
				Time:                30 * time.Second,
				Timeout:             10 * time.Second,
				PermitWithoutStream: true,
			}),
			grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", addr)
			}),
		)
		if err != nil {
			g.logger.Error("failed to create gRPC client %d: %v", i+1, err)
			// Cleanup partial connections
			g.cleanup()
			return fmt.Errorf("failed to create gRPC client to %s: %w", grpcURL, err)
		}
		g.logger.Debug("gRPC client %d created successfully", i+1)

		g.connections[i] = conn
		g.sessionClients[i] = pb.NewGravitySessionServiceClient(conn)
		g.circuitBreakers[i] = NewCircuitBreaker(DefaultCircuitBreakerConfig())
	}
	g.logger.Debug("all %d gRPC clients created successfully", connectionCount)

	// Release mutex before blocking operations (control streams, connect, tunnel streams)
	g.mu.Unlock()

	g.logger.Debug("establishing control streams...")
	// Establish control streams (one per connection)
	err = g.establishControlStreams()
	if err != nil {
		// Check if this is due to context cancellation (graceful shutdown)
		if isContextCanceled(g.ctx, err) {
			g.logger.Debug("control streams closed due to context cancellation")
			g.cleanup()
			return context.Canceled
		}
		g.logger.Error("failed to establish control streams: %v", err)
		g.cleanup()
		return fmt.Errorf("failed to establish control streams: %w", err)
	}
	g.logger.Debug("control streams established successfully")

	g.logger.Debug("sending session hello...")
	// Send session hello to authenticate and get session info
	err = g.sendSessionHello()
	if err != nil {
		// Check if this is due to context cancellation (graceful shutdown)
		if isContextCanceled(g.ctx, err) {
			g.logger.Debug("session hello cancelled due to context cancellation")
			g.cleanup()
			return context.Canceled
		}
		g.logger.Error("failed to send session hello: %v", err)
		g.cleanup()
		return fmt.Errorf("failed to send session hello: %w", err)
	}
	g.logger.Debug("session hello sent successfully")

	g.logger.Debug("establishing tunnel streams...")
	// Establish tunnel streams (multiple per connection) - after connect message
	err = g.establishTunnelStreams()
	if err != nil {
		// Check if this is due to context cancellation (graceful shutdown)
		if isContextCanceled(g.ctx, err) {
			g.logger.Debug("tunnel streams closed due to context cancellation")
			g.cleanup()
			return context.Canceled
		}
		g.logger.Error("failed to establish tunnel streams: %v", err)
		g.cleanup()
		return fmt.Errorf("failed to establish tunnel streams: %w", err)
	}
	g.logger.Debug("tunnel streams established successfully")

	// Re-acquire mutex for final state updates
	g.mu.Lock()
	g.connected = true
	g.mu.Unlock()

	go g.tunnelLivenessMonitor()

	g.logger.Debug("connected to Gravity server: %s", g.url)

	g.logger.Debug("starting background goroutines...")
	// Start background goroutines
	go g.handleInboundPackets()
	go g.handleOutboundPackets()
	go g.handleTextMessages()

	go g.monitorConnectionHealth()
	go g.handlePingHeartbeat()

	g.logger.Debug("all background goroutines started successfully")

	g.logger.Debug("gRPC Gravity client startup completed successfully")
	return nil
}

var ErrNoGravityFound = errors.New("no available gravity URL resolved")

// startMultiEndpoint is the multi-Gravity connection path, activated when
// GravityURLs has >1 URL. Creates connections to multiple Gravity servers
// with sticky tunnel selection and peer cycling.
func (g *GravityClient) startMultiEndpoint() error {
	// Resolve URLs under g.mu (reads gravityURLs), then release before
	// acquiring g.endpointsMu to avoid nested lock ordering issues.
	g.mu.Lock()
	urls := g.resolveGravityURLs()
	if len(urls) == 0 {
		g.mu.Unlock()
		return ErrNoGravityFound
	}
	g.mu.Unlock()

	g.logger.Info("multi-endpoint mode: connecting to %d Gravity servers: %v", len(urls), urls)

	g.endpointsMu.Lock()
	g.endpoints = make([]*GravityEndpoint, 0)
	var errs []error
	for _, endpointURL := range urls {
		// Only expand single URL via DNS when useMultiConnect is explicitly enabled
		if len(urls) == 1 && g.useMultiConnect {
			// Parse URL to get host:port
			parsedHost, err := g.parseGRPCURL(endpointURL)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to parse URL %q: %w", endpointURL, err))
				continue
			}
			hostname, port, err := net.SplitHostPort(parsedHost)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to split host:port from %q: %w", parsedHost, err))
				continue
			}
			if port == "" {
				port = "443"
			}

			// If hostname is already an IP address, skip DNS lookup
			if ip := net.ParseIP(hostname); ip != nil {
				ep := &GravityEndpoint{URL: endpointURL, TLSServerName: g.preferredTLSServerName(endpointURL)}
				ep.healthy.Store(false)
				g.endpoints = append(g.endpoints, ep)
				continue
			}

			// Perform DNS lookup on the raw hostname
			ok, ips, err := dns.DefaultDNS.LookupMulti(g.context, hostname)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if ok && len(ips) > 0 {
				// Add ALL unique IPs as candidate endpoints. Don't cap here —
				// establishControlStreamsMulti connects in parallel and keeps
				// the first MaxGravityPeers that succeed. This ensures stale
				// DNS records (unreachable IPs) don't block real ions from
				// getting a slot.
				seen := make(map[string]bool, len(ips))
				for _, ip := range ips {
					ipStr := ip.String()
					if seen[ipStr] {
						continue // dedupe
					}
					seen[ipStr] = true
					hostPort := net.JoinHostPort(ipStr, port)
					ep := &GravityEndpoint{
						URL:           "grpc://" + hostPort,
						TLSServerName: hostname,
					}
					ep.healthy.Store(false)
					g.endpoints = append(g.endpoints, ep)
				}
			} else {
				// DNS returned no IPs, fall back to original URL
				ep := &GravityEndpoint{URL: endpointURL, TLSServerName: g.preferredTLSServerName(endpointURL)}
				ep.healthy.Store(false)
				g.endpoints = append(g.endpoints, ep)
			}
		} else {
			ep := &GravityEndpoint{URL: endpointURL, TLSServerName: g.preferredTLSServerName(endpointURL)}
			ep.healthy.Store(false)
			g.endpoints = append(g.endpoints, ep)
		}
	}
	g.endpointsMu.Unlock()

	if len(errs) > 0 && len(g.endpoints) == 0 {
		return errors.Join(errs...)
	}

	if len(g.endpoints) == 0 {
		return ErrNoGravityFound
	}

	// Set multiEndpointMode flag when we have multiple endpoints or UseMultiConnect
	// is explicitly enabled. This is used by other code paths that need to know if
	// we're in multi-endpoint mode (e.g., sendSessionHello, heartbeat, disconnection
	// handling, and resilient control stream establishment).
	g.multiEndpointMode.Store(len(g.endpoints) > 1 || g.useMultiConnect)

	g.selector = NewEndpointSelector(DefaultBindingTTL)

	g.mu.Lock()

	g.bindingCleanupOnce.Do(func() {
		go g.bindingCleanupLoop()
	})

	// Start peer discovery and connection cycling when a resolver is configured.
	if g.discoveryResolveFunc != nil {
		go g.peerDiscoveryLoop()
	}

	g.logger.Debug("loading CA certificate for TLS...")
	pool, err := x509.SystemCertPool()
	if err != nil {
		g.logger.Warn("failed to load system cert pool, using empty pool: %v", err)
		pool = x509.NewCertPool()
	}
	if ok := pool.AppendCertsFromPEM([]byte(g.caCert)); !ok {
		g.logger.Error("failed to load embedded CA certificate")
		g.mu.Unlock()
		return fmt.Errorf("failed to load embedded CA certificate")
	}

	g.logger.Debug("creating TLS configuration...")
	if g.tlsCert == nil {
		g.logger.Error("failed to load TLS certificate")
		g.mu.Unlock()
		return fmt.Errorf("failed to load TLS certificate, self-signed cert was nil")
	}

	g.endpointsMu.RLock()
	endpoints := make([]*GravityEndpoint, len(g.endpoints))
	copy(endpoints, g.endpoints)
	g.endpointsMu.RUnlock()

	connectionCount := len(endpoints)
	g.logger.Debug("creating connection pool with %d connections", connectionCount)
	g.connections = make([]*grpc.ClientConn, connectionCount)
	g.sessionClients = make([]pb.GravitySessionServiceClient, connectionCount)
	g.circuitBreakers = make([]*CircuitBreaker, connectionCount)
	g.connectionURLs = make([]string, connectionCount)
	g.endpointReconnecting = make([]atomic.Bool, connectionCount)
	g.endpointFailCount = make([]atomic.Int32, connectionCount)
	g.endpointStreamIndices = make(map[string][]int)

	// Initialize connection health tracking
	g.streamManager.connectionHealth = make([]bool, connectionCount)
	g.streamManager.connectionIdleCount = make([]int, connectionCount)
	for i := range g.streamManager.connectionHealth {
		g.streamManager.connectionHealth[i] = true
	}

	g.logger.Debug("establishing %d gRPC connections", connectionCount)
	for i := range connectionCount {
		endpointURL := endpoints[i].URL
		grpcURL, err := g.parseGRPCURL(endpointURL)
		if err != nil {
			g.cleanup()
			g.mu.Unlock()
			return fmt.Errorf("invalid URL %q: %w", endpointURL, err)
		}

		// Use the endpoint's TLSServerName (set during DNS resolution) if available,
		// otherwise extract from the URL. This ensures IP-based endpoints resolved
		// via DNS use the original hostname for TLS SNI.
		hostname := endpoints[i].TLSServerName
		if hostname == "" {
			var err error
			hostname, err = g.extractHostnameFromURL(endpointURL)
			if err != nil {
				g.cleanup()
				g.mu.Unlock()
				return fmt.Errorf("failed to extract hostname from %q: %w", endpointURL, err)
			}
		} else {
			g.logger.Debug("using TLS ServerName %q for IP-based endpoint %s", hostname, endpointURL)
		}

		cert := *g.tlsCert
		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
			ServerName:   hostname,
			MinVersion:   tls.VersionTLS13,
			GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
				if g.tlsCert != nil {
					return g.tlsCert, nil
				}
				return nil, fmt.Errorf("no client certificate available")
			},
			RootCAs: pool,
		}

		creds := credentials.NewTLS(tlsConfig)

		g.logger.Debug("creating gRPC client %d to %s with TLS", i+1, grpcURL)
		conn, err := grpc.NewClient(grpcURL,
			grpc.WithTransportCredentials(creds),
			grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
			grpc.WithInitialWindowSize(1<<20),
			grpc.WithInitialConnWindowSize(4<<20),
			grpc.WithKeepaliveParams(keepalive.ClientParameters{
				Time:                30 * time.Second,
				Timeout:             10 * time.Second,
				PermitWithoutStream: true,
			}),
			grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", addr)
			}),
		)
		if err != nil {
			g.logger.Error("failed to create gRPC client %d: %v", i+1, err)
			g.cleanup()
			g.mu.Unlock()
			return fmt.Errorf("failed to create gRPC client to %s: %w", grpcURL, err)
		}

		g.connections[i] = conn
		g.sessionClients[i] = pb.NewGravitySessionServiceClient(conn)
		g.circuitBreakers[i] = NewCircuitBreaker(DefaultCircuitBreakerConfig())
		g.connectionURLs[i] = endpointURL
	}
	g.logger.Debug("all %d gRPC clients created successfully", connectionCount)

	g.mu.Unlock()

	g.logger.Debug("establishing control streams...")
	err = g.establishControlStreams()
	if err != nil {
		if isContextCanceled(g.ctx, err) {
			g.logger.Debug("control streams closed due to context cancellation")
			g.cleanup()
			return context.Canceled
		}
		g.logger.Error("failed to establish control streams: %v", err)
		g.cleanup()
		return fmt.Errorf("failed to establish control streams: %w", err)
	}
	g.logger.Debug("control streams established successfully")

	g.logger.Debug("sending session hello...")
	err = g.sendSessionHello()
	if err != nil {
		if isContextCanceled(g.ctx, err) {
			g.logger.Debug("session hello cancelled due to context cancellation")
			g.cleanup()
			return context.Canceled
		}
		g.logger.Error("failed to send session hello: %v", err)
		g.cleanup()
		return fmt.Errorf("failed to send session hello: %w", err)
	}
	g.logger.Debug("session hello sent successfully")

	g.logger.Debug("establishing tunnel streams...")
	err = g.establishTunnelStreams()
	if err != nil {
		if isContextCanceled(g.ctx, err) {
			g.logger.Debug("tunnel streams closed due to context cancellation")
			g.cleanup()
			return context.Canceled
		}
		g.logger.Error("failed to establish tunnel streams: %v", err)
		g.cleanup()
		return fmt.Errorf("failed to establish tunnel streams: %w", err)
	}
	g.logger.Debug("tunnel streams established successfully")
	g.rebuildEndpointStreamIndices()
	g.refreshEndpointHealth()

	g.mu.Lock()
	g.connected = true
	g.mu.Unlock()

	// Start tunnel liveness monitor after startup succeeds (connected=true).
	// Starting earlier would leak the goroutine if startup fails.
	g.logger.Debug("launching tunnelLivenessMonitor goroutine")
	go g.tunnelLivenessMonitor()

	var endpointURLs []string
	for _, ep := range endpoints {
		endpointURLs = append(endpointURLs, ep.URL)
	}

	g.streamManager.controlMu.RLock()
	healthyCount := 0
	for _, s := range g.streamManager.controlStreams {
		if s != nil {
			healthyCount++
		}
	}
	g.streamManager.controlMu.RUnlock()

	if healthyCount < connectionCount {
		g.logger.Info("connected to %d of %d Gravity servers (degraded mode, %d reconnecting): %v", healthyCount, connectionCount, connectionCount-healthyCount, endpointURLs)
	} else {
		g.logger.Info("connected to %d Gravity servers: %v", connectionCount, endpointURLs)
	}

	go g.handleInboundPackets()
	go g.handleOutboundPackets()
	go g.handleTextMessages()
	go g.monitorConnectionHealth()
	go g.handlePingHeartbeat()

	g.logger.Debug("gRPC Gravity client multi-endpoint startup completed successfully")
	return nil
}

func (g *GravityClient) resolveGravityURLs() []string {
	if len(g.gravityURLs) == 0 {
		if strings.TrimSpace(g.url) == "" {
			return nil
		}
		return []string{g.url}
	}

	maxPeers := g.poolConfig.MaxGravityPeers
	if maxPeers <= 0 {
		maxPeers = DefaultMaxGravityPeers
	}

	// Don't cap at maxPeers here — include ALL URLs so
	// establishControlStreamsMulti can race them in parallel and keep
	// the first maxPeers that connect. Capping early lets stale/dead
	// IPs steal slots from healthy ones.
	seen := make(map[string]struct{}, len(g.gravityURLs))
	urls := make([]string, 0, len(g.gravityURLs))
	for _, raw := range g.gravityURLs {
		u := strings.TrimSpace(raw)
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		urls = append(urls, u)
	}

	if len(urls) == 0 {
		if strings.TrimSpace(g.url) == "" {
			return nil
		}
		return []string{g.url}
	}

	return urls
}

// tunnelLivenessMonitor periodically sends keepalive probes on tunnel streams
// and checks whether they're actually carrying data. It detects zombie streams
// where the gRPC connection is alive (keepalive passes, control stream active)
// but data has stopped flowing — caused by HTTP/2 flow control deadlock,
// half-open streams, or stuck Send/Recv.
//
// The keepalive probe ("AGNT" marker) is sent on one stream per endpoint every
// 10s. Ion echoes it back immediately, updating LastRecvUs in stream metrics.
// This keeps idle connections active and provides a positive liveness signal.
// sendTunnelKeepalives also sets LastProbeSentUs on every probe so streams are
// promoted from "idle" (zero timestamps) to "probed" — enabling detection
// of endpoints that die before the first echo arrives. LastProbeSentUs is
// separate from LastSendUs (updated by normal data traffic) to prevent
// WritePacket sends from masking zombie tunnels.
//
// If ALL streams on an endpoint have been probed but show no round-trip
// activity for 30s (~3 missed probes), the endpoint is marked unhealthy
// and reconnection is triggered. Streams that have never been probed
// (LastProbeSentUs=0, LastRecvUs=0) are truly idle and not flagged.
func (g *GravityClient) tunnelLivenessMonitor() {
	defer func() {
		if r := recover(); r != nil {
			g.logger.Error("tunnel liveness monitor PANICKED: %v", r)
		}
	}()

	const (
		checkInterval = 10 * time.Second
		staleTimeout  = 30 * time.Second
	)

	g.logger.Debug("tunnel liveness monitor started (check=%s, stale=%s)", checkInterval, staleTimeout)

	// Send the first keepalive immediately — don't wait 10s. This lets
	// ion verify the tunnel and mark the machine as healthy ASAP after
	// hadron connects.
	g.sendTunnelKeepalives()

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-g.ctx.Done():
			g.logger.Debug("tunnel liveness monitor exiting (context cancelled)")
			return
		case <-ticker.C:
			g.sendTunnelKeepalives()
			g.checkTunnelStreamLiveness(staleTimeout)
		}
	}
}

// sendTunnelKeepalives sends a keepalive probe on one healthy stream per
// endpoint. Ion echoes the probe back, updating both send and receive
// timestamps. This keeps idle streams active and detects zombie streams
// that can't round-trip data.
// streamSnapshot holds fields copied from StreamInfo under tunnelMu for
// safe access outside the lock.
type streamSnapshot struct {
	streamID  string
	connIndex int
	isHealthy bool
	stream    pb.GravitySessionService_StreamSessionPacketsClient
	sendMu    *sync.Mutex // pointer to the stream's send mutex for serialization
}

func (g *GravityClient) sendTunnelKeepalives() {
	g.logger.Trace("tunnel keepalive: starting probe cycle")
	// Snapshot stream info under tunnelMu to avoid data races on isHealthy.
	g.streamManager.tunnelMu.RLock()
	snapshots := make([]streamSnapshot, len(g.streamManager.tunnelStreams))
	for i, si := range g.streamManager.tunnelStreams {
		if si != nil {
			snapshots[i] = streamSnapshot{
				streamID:  si.streamID,
				connIndex: si.connIndex,
				isHealthy: si.isHealthy,
				stream:    si.stream,
				sendMu:    &si.sendMu,
			}
		}
	}
	g.streamManager.tunnelMu.RUnlock()

	if len(snapshots) == 0 {
		return
	}

	streamsPerConn := g.poolConfig.StreamsPerGravity
	if streamsPerConn <= 0 {
		streamsPerConn = DefaultStreamsPerGravity
	}

	// Build keepalive packet: 4-byte marker + 8-byte timestamp (for RTT measurement)
	probe := make([]byte, 12)
	copy(probe, TunnelKeepaliveMarker)
	ts := time.Now().UnixMicro()
	probe[4] = byte(ts >> 56)
	probe[5] = byte(ts >> 48)
	probe[6] = byte(ts >> 40)
	probe[7] = byte(ts >> 32)
	probe[8] = byte(ts >> 24)
	probe[9] = byte(ts >> 16)
	probe[10] = byte(ts >> 8)
	probe[11] = byte(ts)

	// Rotate which stream gets probed within each endpoint. Each cycle probes
	// a different stream so all streams are covered within the stale timeout
	// (60s / 15s interval = 4 cycles → covers all 4 streams per endpoint).
	rotation := int(g.probeRotation.Add(1))

	for connIndex := 0; connIndex*streamsPerConn < len(snapshots); connIndex++ {
		base := connIndex * streamsPerConn
		end := base + streamsPerConn
		if end > len(snapshots) {
			end = len(snapshots)
		}

		// Count healthy streams for this endpoint to pick the rotation target
		healthyInEndpoint := make([]int, 0, streamsPerConn)
		for i := base; i < end; i++ {
			s := snapshots[i]
			if s.isHealthy && s.stream != nil && s.sendMu != nil {
				healthyInEndpoint = append(healthyInEndpoint, i)
			}
		}
		if len(healthyInEndpoint) == 0 {
			continue
		}

		// Pick the stream for this rotation cycle
		targetIdx := healthyInEndpoint[rotation%len(healthyInEndpoint)]
		s := snapshots[targetIdx]

		// Serialize with the data path's sendMu. Use a goroutine with
		// timeout so a wedged Send doesn't block the monitor loop.
		go func(idx int, snap streamSnapshot) {
			errCh := make(chan error, 1)
			go func() {
				snap.sendMu.Lock()
				err := snap.stream.Send(&pb.TunnelPacket{Data: probe})
				snap.sendMu.Unlock()
				errCh <- err
			}()
			select {
			case err := <-errCh:
				// Update LastProbeSentUs on every successful probe so the
				// liveness monitor can distinguish "probed but no echo"
				// from "never probed." Unlike LastSendUs (updated by
				// normal WritePacket traffic), LastProbeSentUs is only
				// set by keepalive probes — preventing data-path sends
				// from masking a zombie tunnel (one-way: sending works,
				// receiving doesn't).
				if err == nil {
					g.streamManager.metricsMu.Lock()
					if m := g.streamManager.streamMetrics[snap.streamID]; m != nil {
						m.LastProbeSentUs = time.Now().UnixMicro()
					}
					g.streamManager.metricsMu.Unlock()
				}
			case <-time.After(5 * time.Second):
				g.logger.Warn("tunnel keepalive send blocked >5s on stream %d — possible zombie", idx)
			}
		}(targetIdx, s)
	}
}

// checkTunnelStreamLiveness examines each tunnel stream's last activity
// timestamps and triggers reconnection for endpoints with all-stale streams.
func (g *GravityClient) checkTunnelStreamLiveness(staleTimeout time.Duration) {
	now := time.Now().UnixMicro()
	staleUs := staleTimeout.Microseconds()

	// Snapshot stream info under tunnelMu to avoid data races on isHealthy/streamID.
	g.streamManager.tunnelMu.RLock()
	snapshots := make([]streamSnapshot, len(g.streamManager.tunnelStreams))
	for i, si := range g.streamManager.tunnelStreams {
		if si != nil {
			snapshots[i] = streamSnapshot{
				streamID:  si.streamID,
				connIndex: si.connIndex,
				isHealthy: si.isHealthy,
			}
		}
	}
	g.streamManager.tunnelMu.RUnlock()

	if len(snapshots) == 0 {
		return
	}

	// Determine streams per connection from the pool config
	streamsPerConn := g.poolConfig.StreamsPerGravity
	if streamsPerConn <= 0 {
		streamsPerConn = DefaultStreamsPerGravity
	}

	g.streamManager.metricsMu.RLock()
	defer g.streamManager.metricsMu.RUnlock()

	// Check each endpoint's streams as a group
	for connIndex := 0; connIndex*streamsPerConn < len(snapshots); connIndex++ {
		base := connIndex * streamsPerConn
		end := base + streamsPerConn
		if end > len(snapshots) {
			end = len(snapshots)
		}

		allStale := true
		hasStreams := false
		for i := base; i < end; i++ {
			s := snapshots[i]
			if !s.isHealthy {
				continue
			}
			hasStreams = true

			metrics, ok := g.streamManager.streamMetrics[s.streamID]
			if !ok {
				continue
			}

			lastRecvUs := metrics.LastRecvUs

			// Truly idle: never probed, never received. This means
			// sendTunnelKeepalives hasn't run yet (e.g., stream just
			// established). Don't flag as stale — wait for first probe.
			if metrics.LastProbeSentUs == 0 && lastRecvUs == 0 {
				allStale = false
				break
			}

			// Use LastRecvUs as the primary liveness signal when echoes
			// have been received — it confirms the remote end is alive.
			// If we've probed but never received an echo (LastRecvUs==0),
			// fall back to LastProbeSentUs which represents "probing since."
			// Using LastProbeSentUs (not LastSendUs) avoids masking zombie
			// tunnels — normal WritePacket traffic updates LastSendUs, which
			// can hide a one-way tunnel where sending works but receiving doesn't.
			var lastActivity int64
			if lastRecvUs > 0 {
				lastActivity = lastRecvUs
			} else {
				lastActivity = metrics.LastProbeSentUs
			}

			if (now - lastActivity) < staleUs {
				allStale = false // recently active
				break
			}
		}

		if hasStreams && allStale {
			g.endpointsMu.RLock()
			var epURL string
			if connIndex < len(g.endpoints) && g.endpoints[connIndex] != nil {
				epURL = g.endpoints[connIndex].URL
				g.endpoints[connIndex].healthy.Store(false)
			}
			g.endpointsMu.RUnlock()

			g.logger.Warn("tunnel liveness: endpoint %d (%s) has all stale streams (no activity for %s) — triggering reconnection",
				connIndex, epURL, staleTimeout)
			g.wakePeerDiscovery()
			// Use per-endpoint reconnection — only reconnect the specific
			// endpoint with stale streams. Previously this called
			// triggerAllEndpointReconnections which could cascade a
			// single-endpoint failure to all endpoints.
			g.handleEndpointDisconnection(connIndex, "tunnel_liveness_stale")
			return // One reconnection cycle at a time
		}
	}
}

func (g *GravityClient) bindingCleanupLoop() {
	ticker := time.NewTicker(DefaultBindingTTL)
	defer ticker.Stop()

	for {
		select {
		case <-g.ctx.Done():
			return
		case <-ticker.C:
			g.endpointsMu.RLock()
			selector := g.selector
			g.endpointsMu.RUnlock()
			if selector != nil {
				selector.ExpireBindings()
			}
		}
	}
}

func (g *GravityClient) peerDiscoveryLoop() {
	idleInterval := g.peerDiscoveryInterval
	if idleInterval <= 0 {
		idleInterval = DefaultPeerDiscoveryInterval
	}
	cycleInterval := g.peerCycleInterval
	if cycleInterval <= 0 {
		cycleInterval = DefaultPeerCycleInterval
	}

	// Seek interval is used when under capacity (healthy < MaxGravityPeers).
	// Aggressive enough to fill quickly, not so fast it hammers DNS.
	const seekInterval = 10 * time.Second

	maxPeers := g.poolConfig.MaxGravityPeers
	if maxPeers <= 0 {
		maxPeers = DefaultMaxGravityPeers
	}

	interval := seekInterval // start seeking
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	adjustInterval := func() {
		healthyCount := g.countHealthyEndpoints()
		var newInterval time.Duration
		if healthyCount < maxPeers {
			newInterval = seekInterval
		} else {
			newInterval = idleInterval
		}
		if newInterval != interval {
			if newInterval == seekInterval {
				g.logger.Info("peer discovery: under capacity (%d/%d healthy) — seeking every %s", healthyCount, maxPeers, seekInterval)
			} else {
				g.logger.Info("peer discovery: at capacity (%d/%d healthy) — idling every %s", healthyCount, maxPeers, idleInterval)
			}
			interval = newInterval
			ticker.Reset(interval)
		}
	}

	for {
		select {
		case <-g.ctx.Done():
			return
		case <-g.peerDiscoveryWake:
			// Woken by an endpoint going unhealthy — re-check immediately.
			g.checkPeerDiscovery(cycleInterval)
			adjustInterval()
		case <-ticker.C:
			g.checkPeerDiscovery(cycleInterval)
			adjustInterval()
		}
	}
}

// wakePeerDiscovery signals the peer discovery loop to re-check immediately.
// Non-blocking — safe to call from any goroutine.
func (g *GravityClient) wakePeerDiscovery() {
	select {
	case g.peerDiscoveryWake <- struct{}{}:
	default:
	}
}

// countHealthyEndpoints returns the number of endpoints with healthy=true.
func (g *GravityClient) countHealthyEndpoints() int {
	g.endpointsMu.RLock()
	defer g.endpointsMu.RUnlock()
	count := 0
	for _, ep := range g.endpoints {
		if ep != nil && ep.healthy.Load() {
			count++
		}
	}
	return count
}

func (g *GravityClient) checkPeerDiscovery(cycleInterval time.Duration) {
	if g.discoveryResolveFunc == nil {
		return
	}

	allURLs := g.discoveryResolveFunc()
	if len(allURLs) == 0 {
		g.logger.Debug("peer discovery: resolve returned no URLs")
		return
	}

	maxPeers := g.poolConfig.MaxGravityPeers
	if maxPeers <= 0 {
		maxPeers = DefaultMaxGravityPeers
	}

	connectedURLs := make(map[string]bool)
	g.endpointsMu.RLock()
	for _, ep := range g.endpoints {
		if ep == nil || strings.TrimSpace(ep.URL) == "" {
			continue
		}
		connectedURLs[ep.URL] = true
	}
	currentCount := len(connectedURLs)
	g.endpointsMu.RUnlock()

	g.mu.Lock()
	g.gravityURLs = append([]string(nil), allURLs...)
	g.mu.Unlock()

	g.logger.Debug("peer discovery: resolved %d URLs, connected to %d/%d",
		len(allURLs), currentCount, maxPeers)

	// If DNS doesn't expose more hosts than we are already connected to,
	// there is nothing to rotate.
	if len(allURLs) <= currentCount {
		g.logger.Debug("peer discovery: full coverage (%d URLs, %d connections), no cycling needed",
			len(allURLs), currentCount)
		return
	}

	// Find URLs we're not currently connected to.
	var newURLs []string
	for _, u := range allURLs {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if !connectedURLs[u] {
			newURLs = append(newURLs, u)
		}
	}

	if len(newURLs) == 0 {
		g.logger.Debug("peer discovery: no new URLs found")
		return
	}

	// Identify currently connected endpoints that are no longer present in DNS.
	dnsSet := make(map[string]bool, len(allURLs))
	for _, u := range allURLs {
		u = strings.TrimSpace(u)
		if u != "" {
			dnsSet[u] = true
		}
	}

	var staleURLs []string
	g.endpointsMu.RLock()
	for _, ep := range g.endpoints {
		if ep == nil || strings.TrimSpace(ep.URL) == "" {
			continue
		}
		if !dnsSet[ep.URL] {
			staleURLs = append(staleURLs, ep.URL)
		}
	}
	g.endpointsMu.RUnlock()

	// Priority 1: replace stale endpoints immediately.
	if len(staleURLs) > 0 {
		evictURL := staleURLs[0]
		newURL := pickRandomURL(newURLs)
		if newURL == "" {
			g.logger.Debug("peer discovery: no candidate URL available to replace stale endpoint %s", evictURL)
			return
		}
		g.logger.Info("peer discovery: replacing stale connection %s (no longer in DNS) with %s", evictURL, newURL)
		g.cycleEndpoint(evictURL, newURL)
		return
	}

	// Priority 2: if below capacity, add new endpoints immediately — don't
	// wait for the cycle interval. This ensures newly discovered ions are
	// connected to as soon as DNS returns their IP.
	if currentCount < maxPeers && len(newURLs) > 0 {
		newURL := pickRandomURL(newURLs)
		if newURL != "" {
			g.logger.Info("peer discovery: adding new endpoint %s (currently %d/%d, %d new available)",
				newURL, currentCount, maxPeers, len(newURLs))
			g.addEndpoint(newURL)
			return
		}
	}

	// Priority 3: rotate one connection for freshness (cycle interval gated).
	now := time.Now()
	lastCycleUnix := g.lastCycleTime.Load()
	if lastCycleUnix > 0 {
		lastCycle := time.Unix(lastCycleUnix, 0)
		if now.Sub(lastCycle) < cycleInterval {
			g.logger.Debug("peer discovery: at capacity (%d/%d), cycle interval not reached (last cycle: %s ago)",
				currentCount, maxPeers, now.Sub(lastCycle).Round(time.Minute))
			return
		}
	}

	g.endpointsMu.RLock()
	if len(g.endpoints) == 0 {
		g.endpointsMu.RUnlock()
		g.logger.Debug("peer discovery: no active endpoints available to cycle")
		return
	}
	evictURL := g.endpoints[randIndex(len(g.endpoints))].URL
	g.endpointsMu.RUnlock()

	newURL := pickRandomURL(newURLs)
	if newURL == "" {
		g.logger.Debug("peer discovery: no candidate URL available for cycle")
		return
	}

	g.logger.Info("peer discovery: cycling connection %s -> %s (%d total Gravities available, connected to %d)",
		evictURL, newURL, len(allURLs), currentCount)
	g.cycleEndpoint(evictURL, newURL)
	g.lastCycleTime.Store(now.Unix())
}

// cycleEndpoint replaces one endpoint with another.
// It removes the old endpoint and adds the new one.
func (g *GravityClient) cycleEndpoint(oldURL, newURL string) {
	if strings.TrimSpace(oldURL) == "" || strings.TrimSpace(newURL) == "" {
		g.logger.Debug("peer discovery: cycle skipped due to empty URL(s): old=%q new=%q", oldURL, newURL)
		return
	}

	g.endpointsMu.Lock()
	oldIdx := -1
	for i, ep := range g.endpoints {
		if ep != nil && ep.URL == oldURL {
			oldIdx = i
			break
		}
	}
	if oldIdx < 0 {
		g.endpointsMu.Unlock()
		g.logger.Debug("peer discovery: endpoint %s already removed", oldURL)
		return
	}

	newEp := &GravityEndpoint{URL: newURL, TLSServerName: g.preferredTLSServerName(newURL)}
	newEp.healthy.Store(false)
	g.endpoints[oldIdx] = newEp
	g.endpointsMu.Unlock()

	g.logger.Info("peer discovery: endpoint cycled: removed %s, added %s", oldURL, newURL)
	g.logger.Debug("peer discovery: new endpoint %s will connect on next connection attempt", newURL)
}

// addEndpoint adds a new endpoint to an empty slot (nil or empty URL) in
// the endpoints slice. Used by peer discovery to connect to newly discovered
// gravity servers without waiting for the cycle interval.
func (g *GravityClient) addEndpoint(newURL string) {
	if strings.TrimSpace(newURL) == "" {
		return
	}

	g.endpointsMu.Lock()
	slotIdx := -1
	for i, ep := range g.endpoints {
		if ep == nil || strings.TrimSpace(ep.URL) == "" {
			slotIdx = i
			break
		}
	}
	if slotIdx < 0 {
		// No empty slot found. If we haven't reached maxPeers, grow
		// the endpoints slice (startup may have discovered fewer peers
		// than the configured maximum).
		maxPeers := g.poolConfig.MaxGravityPeers
		if maxPeers <= 0 {
			maxPeers = DefaultMaxGravityPeers
		}
		if len(g.endpoints) < maxPeers {
			slotIdx = len(g.endpoints)
			g.endpoints = append(g.endpoints, nil) // will be set below
		} else {
			g.endpointsMu.Unlock()
			g.logger.Debug("peer discovery: no empty slot for new endpoint %s (at max %d peers)", newURL, maxPeers)
			return
		}
	}

	newEp := &GravityEndpoint{URL: newURL, TLSServerName: g.preferredTLSServerName(newURL)}
	newEp.healthy.Store(false) // will become healthy after reconnection
	g.endpoints[slotIdx] = newEp
	g.endpointsMu.Unlock()

	// Grow all parallel arrays indexed by endpoint index so that
	// reconnectEndpoint and other code paths can safely access [slotIdx].
	// This mirrors the initialization in startMultiEndpoint.
	g.mu.Lock()
	for len(g.connectionURLs) <= slotIdx {
		g.connectionURLs = append(g.connectionURLs, "")
	}
	g.connectionURLs[slotIdx] = newURL
	for len(g.connections) <= slotIdx {
		g.connections = append(g.connections, nil)
	}
	for len(g.sessionClients) <= slotIdx {
		g.sessionClients = append(g.sessionClients, nil)
	}
	for len(g.circuitBreakers) <= slotIdx {
		g.circuitBreakers = append(g.circuitBreakers, nil)
	}
	for len(g.endpointReconnecting) <= slotIdx {
		g.endpointReconnecting = append(g.endpointReconnecting, atomic.Bool{})
	}
	for len(g.endpointFailCount) <= slotIdx {
		g.endpointFailCount = append(g.endpointFailCount, atomic.Int32{})
	}
	g.mu.Unlock()

	// Grow stream manager arrays under their respective locks.
	g.streamManager.controlMu.Lock()
	for len(g.streamManager.controlStreams) <= slotIdx {
		g.streamManager.controlStreams = append(g.streamManager.controlStreams, nil)
	}
	for len(g.streamManager.controlSendMu) <= slotIdx {
		g.streamManager.controlSendMu = append(g.streamManager.controlSendMu, sync.Mutex{})
	}
	for len(g.streamManager.contexts) <= slotIdx {
		g.streamManager.contexts = append(g.streamManager.contexts, nil)
	}
	for len(g.streamManager.cancels) <= slotIdx {
		g.streamManager.cancels = append(g.streamManager.cancels, nil)
	}
	g.streamManager.controlMu.Unlock()

	g.streamManager.healthMu.Lock()
	for len(g.streamManager.connectionHealth) <= slotIdx {
		g.streamManager.connectionHealth = append(g.streamManager.connectionHealth, false)
	}
	for len(g.streamManager.connectionIdleCount) <= slotIdx {
		g.streamManager.connectionIdleCount = append(g.streamManager.connectionIdleCount, 0)
	}
	g.streamManager.healthMu.Unlock()

	g.logger.Info("peer discovery: added new endpoint %s at slot %d, starting reconnection", newURL, slotIdx)
	go g.reconnectEndpoint(slotIdx, "peer_discovery_add")
}

func pickRandomURL(urls []string) string {
	if len(urls) == 0 {
		return ""
	}
	if len(urls) == 1 {
		return urls[0]
	}
	return urls[randIndex(len(urls))]
}

func randIndex(n int) int {
	if n <= 1 {
		return 0
	}
	return mathrand.IntN(n)
}

func (g *GravityClient) rebuildEndpointStreamIndices() {
	// Snapshot connectionURLs under g.mu to avoid racing with reconnect().
	g.mu.RLock()
	connURLs := make([]string, len(g.connectionURLs))
	copy(connURLs, g.connectionURLs)
	g.mu.RUnlock()

	m := make(map[string][]int)

	g.streamManager.tunnelMu.RLock()
	for idx, stream := range g.streamManager.tunnelStreams {
		if stream == nil {
			continue
		}
		if stream.connIndex < 0 || stream.connIndex >= len(connURLs) {
			continue
		}
		endpointURL := connURLs[stream.connIndex]
		if endpointURL == "" {
			continue
		}
		m[endpointURL] = append(m[endpointURL], idx)
	}
	g.streamManager.tunnelMu.RUnlock()

	g.mu.Lock()
	g.endpointStreamIndices = m
	g.mu.Unlock()
}

func (g *GravityClient) refreshEndpointHealth() {
	g.endpointsMu.RLock()
	endpoints := make([]*GravityEndpoint, len(g.endpoints))
	copy(endpoints, g.endpoints)
	g.endpointsMu.RUnlock()

	if len(endpoints) == 0 {
		return
	}

	connectionHealth := make([]bool, len(g.streamManager.connectionHealth))
	g.streamManager.healthMu.RLock()
	copy(connectionHealth, g.streamManager.connectionHealth)
	g.streamManager.healthMu.RUnlock()

	g.mu.RLock()
	connectionURLs := make([]string, len(g.connectionURLs))
	copy(connectionURLs, g.connectionURLs)
	g.mu.RUnlock()

	for _, endpoint := range endpoints {
		healthy := false
		for connIndex, endpointURL := range connectionURLs {
			if endpointURL != endpoint.URL {
				continue
			}
			if connIndex >= 0 && connIndex < len(connectionHealth) && connectionHealth[connIndex] {
				healthy = true
				break
			}
		}
		endpoint.healthy.Store(healthy)
		if healthy {
			endpoint.lastHeartbeat.Store(time.Now().Unix())
		}
	}
}

// establishControlStreams creates control streams for each connection
func (g *GravityClient) establishControlStreams() error {
	g.logger.Debug("creating control streams for %d connections", len(g.connections))
	numConns := len(g.connections)
	if numConns == 0 {
		return fmt.Errorf("no gRPC connections available")
	}
	g.streamManager.controlStreams = make([]pb.GravitySessionService_EstablishSessionClient, numConns)
	g.streamManager.controlSendMu = make([]sync.Mutex, numConns)
	g.streamManager.contexts = make([]context.Context, numConns)
	g.streamManager.cancels = make([]context.CancelFunc, numConns)

	if g.multiEndpointMode.Load() {
		return g.establishControlStreamsMulti()
	}

	if len(g.sessionClients) == 0 {
		return fmt.Errorf("no session clients available")
	}

	for i, client := range g.sessionClients {
		// Guard ALL slice accesses — during concurrent reconnection,
		// slices may be shorter than expected.
		if i >= numConns ||
			i >= len(g.streamManager.contexts) ||
			i >= len(g.streamManager.cancels) ||
			i >= len(g.streamManager.controlStreams) ||
			i >= len(g.streamManager.controlSendMu) {
			g.logger.Warn("establishControlStreams: index %d exceeds slice capacity (conns=%d, ctx=%d, streams=%d), breaking",
				i, numConns, len(g.streamManager.contexts), len(g.streamManager.controlStreams))
			break
		}
		g.logger.Debug("establishing control stream %d/%d", i+1, len(g.connections))
		ctx, cancel := context.WithCancel(g.ctx)
		g.streamManager.contexts[i] = ctx
		g.streamManager.cancels[i] = cancel

		stream, err := client.EstablishSession(ctx, grpc.UseCompressor(grpcgzip.Name))
		if err != nil {
			// Don't log error if context was cancelled (graceful shutdown)
			if !isContextCanceled(g.ctx, err) {
				g.logger.Error("failed to establish control stream %d: %v", i+1, err)
			}
			return fmt.Errorf("failed to establish control stream %d: %w", i, err)
		}
		g.logger.Debug("control stream %d established successfully", i+1)

		g.streamManager.controlStreams[i] = stream

		// Start listening on this control stream
		go g.handleControlStream(i, stream)
	}
	g.logger.Debug("all %d control streams established successfully", len(g.connections))

	return nil
}

func (g *GravityClient) establishControlStreamsMulti() error {
	type establishResult struct {
		index  int
		ctx    context.Context
		cancel context.CancelFunc
		stream pb.GravitySessionService_EstablishSessionClient
		err    error
	}

	n := len(g.sessionClients)
	maxPeers := g.poolConfig.MaxGravityPeers
	if maxPeers <= 0 {
		maxPeers = DefaultMaxGravityPeers
	}

	results := make(chan establishResult, n)

	for i, client := range g.sessionClients {
		go func(idx int, c pb.GravitySessionServiceClient) {
			ctx, cancel := context.WithCancel(g.ctx)
			stream, err := c.EstablishSession(ctx, grpc.UseCompressor(grpcgzip.Name))
			results <- establishResult{index: idx, ctx: ctx, cancel: cancel, stream: stream, err: err}
		}(i, client)
	}

	failed := make([]int, 0)
	successCount := 0
	var lastErr error

	for range n {
		result := <-results

		if result.err != nil {
			if !isContextCanceled(g.ctx, result.err) {
				g.logger.Debug("control stream %d failed: %v", result.index+1, result.err)
			}
			result.cancel()
			failed = append(failed, result.index)
			lastErr = result.err
			continue
		}

		// If we already have enough healthy connections, fully tear down
		// this excess winner so no stale state remains in rotation.
		if successCount >= maxPeers {
			g.logger.Debug("control stream %d succeeded but already have %d/%d — closing excess", result.index+1, successCount, maxPeers)
			result.cancel()
			idx := result.index
			g.mu.Lock()
			if idx >= 0 && idx < len(g.connections) && g.connections[idx] != nil {
				_ = g.connections[idx].Close()
				g.connections[idx] = nil
			}
			if idx >= 0 && idx < len(g.sessionClients) {
				g.sessionClients[idx] = nil
			}
			if idx >= 0 && idx < len(g.connectionURLs) {
				g.connectionURLs[idx] = ""
			}
			g.mu.Unlock()
			g.streamManager.healthMu.Lock()
			if idx >= 0 && idx < len(g.streamManager.connectionHealth) {
				g.streamManager.connectionHealth[idx] = false
			}
			g.streamManager.healthMu.Unlock()
			continue
		}

		g.logger.Debug("control stream %d established successfully (%d/%d)", result.index+1, successCount+1, maxPeers)

		g.streamManager.controlMu.Lock()
		g.streamManager.contexts[result.index] = result.ctx
		g.streamManager.cancels[result.index] = result.cancel
		g.streamManager.controlStreams[result.index] = result.stream
		g.streamManager.controlMu.Unlock()

		go g.handleControlStream(result.index, result.stream)
		successCount++
	}

	if successCount == 0 {
		return fmt.Errorf("failed to establish any control streams (%d candidates): last error: %w", n, lastErr)
	}

	// Clean up failed endpoints and start background reconnect.
	if len(failed) > 0 {
		g.logger.Warn("established %d/%d control streams (%d failed, max %d); failed endpoints will reconnect in background", successCount, n, len(failed), maxPeers)
		for _, idx := range failed {
			g.disconnectEndpointStreams(idx)
			if idx >= 0 && idx < len(g.endpointReconnecting) && g.endpointReconnecting[idx].CompareAndSwap(false, true) {
				go g.reconnectEndpoint(idx, "initial connection failed")
			}
		}
	}

	g.logger.Debug("established %d/%d control streams (max %d)", successCount, n, maxPeers)
	return nil
}

// establishTunnelStreams creates tunnel streams for packet forwarding
func (g *GravityClient) establishTunnelStreams() error {
	// Wait for machine ID from control stream(s).
	// In multi-endpoint mode, wait for responses from ALL Gravity servers
	// (one per endpoint, not per pooled connection) to ensure each has
	// processed the session hello before we open tunnel streams.
	// Use len(g.connections) rather than raw g.gravityURLs because
	// resolveGravityURLs() deduplicates and trims to MaxGravityPeers.
	numExpected := 1
	if len(g.connections) > 1 {
		g.streamManager.controlMu.RLock()
		healthyCount := 0
		for _, s := range g.streamManager.controlStreams {
			if s != nil {
				healthyCount++
			}
		}
		g.streamManager.controlMu.RUnlock()
		if healthyCount == 0 {
			return fmt.Errorf("no healthy control streams available")
		}
		numExpected = healthyCount
		g.logger.Debug("waiting for session hello responses from %d healthy Gravity servers (%d total)...", healthyCount, len(g.connections))
	} else {
		g.logger.Debug("waiting for machine ID from server...")
	}

	// Drain any stale responses from previous sessions (reconnect path).
	g.drainConnectionIDChan()

	// Single shared deadline — don't let late responses extend the total wait.
	// Track how many responses we received to only create tunnel streams on
	// connections where the session hello was acknowledged.
	var machineID string
	helloResponseCount := 0
	deadline := time.NewTimer(time.Minute)
	defer deadline.Stop()
	for i := 0; i < numExpected; i++ {
		select {
		case id := <-g.connectionIDChan:
			if id == "" {
				g.logger.Error("session failed - authentication rejected by server (response %d/%d)", i+1, numExpected)
				return fmt.Errorf("session failed - authentication rejected by server")
			}
			if machineID == "" {
				machineID = id
			}
			helloResponseCount++
			g.logger.Debug("session hello response %d/%d received (machine ID: %s)", i+1, numExpected, id)
		case <-deadline.C:
			if machineID != "" && i > 0 {
				g.logger.Warn("timeout waiting for session hello response %d/%d, proceeding with %d responses", i+1, numExpected, i)
				goto proceed
			}
			g.logger.Error("timeout waiting for machine ID from server - possible server issue or network problem")
			return fmt.Errorf("timeout waiting for machine ID from server")
		}
	}
proceed:
	g.logger.Debug("machine ID received: %s, proceeding with tunnel streams (%d/%d hellos acknowledged)", machineID, helloResponseCount, numExpected)

	// Build the set of connections that actually received a SessionHelloResponse.
	// Tracked by helloAckedStreams (set in processSessionMessage when the response
	// arrives), NOT by control stream liveness — a stream can be open without the
	// server having sent a response. Orphaned tunnel streams on unacked connections
	// have no SessionConnection, no forwardSessionPacketsToGRPC goroutine, and
	// silently drop all packets.
	helloAcked := make(map[int]bool, len(g.sessionClients))
	g.helloAckedStreams.Range(func(key, value any) bool {
		if idx, ok := key.(int); ok {
			helloAcked[idx] = true
		}
		return true
	})

	// If we got fewer responses than expected, mark non-responding endpoints unhealthy.
	// Also mark connectionHealth[i] = false so refreshEndpointHealth() doesn't
	// override our healthy=false with its connectionHealth-based rebuild.
	if helloResponseCount < numExpected {
		g.endpointsMu.RLock()
		for i, ep := range g.endpoints {
			if ep != nil && !helloAcked[i] {
				ep.healthy.Store(false)
				g.streamManager.healthMu.Lock()
				if i < len(g.streamManager.connectionHealth) {
					g.streamManager.connectionHealth[i] = false
				}
				g.streamManager.healthMu.Unlock()
				g.logger.Warn("marking endpoint %d (%s) unhealthy — session hello not acknowledged", i, ep.URL)
				g.wakePeerDiscovery()
			}
		}
		g.endpointsMu.RUnlock()
	}

	// Use configured streams per connection / gravity.
	streamsPerConnection := g.poolConfig.StreamsPerConnection
	if len(g.connections) > 1 {
		streamsPerConnection = g.poolConfig.StreamsPerGravity
		if streamsPerConnection <= 0 {
			streamsPerConnection = DefaultStreamsPerGravity
		}
	}
	// Allocate tunnel streams with fixed-size slots per connection so
	// reconnectSingleEndpoint can find streams at endpointIndex*streamsPerGravity.
	// Unacked/nil connections get nil slots — no streams created but the
	// offsets are preserved for other connections.
	totalStreams := len(g.sessionClients) * streamsPerConnection

	g.streamManager.tunnelStreams = make([]*StreamInfo, totalStreams)

	for connIndex, client := range g.sessionClients {
		if client == nil || !helloAcked[connIndex] {
			continue
		}
		base := connIndex * streamsPerConnection
		for i := range streamsPerConnection {
			slotIndex := base + i
			streamID := fmt.Sprintf("stream_%s", rand.Text())
			ctx := context.WithValue(g.ctx, machineIDKey, machineID)
			ctx = context.WithValue(ctx, streamIDKey, streamID)

			// Add metadata for stream identification
			md := metadata.Pairs(
				"machine-id", machineID,
				"stream-id", streamID,
			)
			ctx = metadata.NewOutgoingContext(ctx, md)

			stream, err := client.StreamSessionPackets(ctx)
			if err != nil {
				return fmt.Errorf("failed to establish tunnel stream %d: %w", slotIndex, err)
			}

			// Create enhanced stream info
			streamInfo := &StreamInfo{
				stream:    stream,
				connIndex: connIndex,
				streamID:  streamID,
				isHealthy: true,
				loadCount: 0,
				lastUsed:  time.Now(),
			}

			g.streamManager.tunnelStreams[slotIndex] = streamInfo

			// Initialize stream metrics
			g.streamManager.metricsMu.Lock()
			g.streamManager.streamMetrics[streamID] = &StreamMetrics{
				PacketsSent:     0,
				PacketsReceived: 0,
				LastLatency:     0,
				ErrorCount:      0,
			}
			g.streamManager.metricsMu.Unlock()

			// Start listening on this tunnel stream
			go g.handleTunnelStream(slotIndex, stream, streamID)
		}
	}

	return nil
}

// sendSessionHello sends the initial session hello message
func (g *GravityClient) sendSessionHello() error {
	g.logger.Debug("sendSessionHello called")
	msg := g.buildSessionHelloMessage()

	// In multi-endpoint mode, send session hello on ALL control streams so
	// every Gravity server registers this Hadron. Each Gravity independently
	// authenticates and creates a Machine for this session.
	g.streamManager.controlMu.RLock()
	numStreams := len(g.streamManager.controlStreams)
	g.streamManager.controlMu.RUnlock()

	if numStreams > 1 && g.multiEndpointMode.Load() {
		g.logger.Debug("sending session hello on all %d control streams (multi-endpoint)", numStreams)
		var successCount int
		var firstErr error
		for i := 0; i < numStreams; i++ {
			if err := g.sendSessionHelloOnStream(i, nil); err != nil {
				g.logger.Warn("session hello failed on control stream %d: %v", i, err)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			successCount++
			g.logger.Debug("session hello sent on control stream %d", i)
		}
		if successCount == 0 && firstErr != nil {
			return fmt.Errorf("failed to send session hello: %w", firstErr)
		}
		return nil
	}

	// Single-endpoint: send on primary control stream only (original path)
	g.logger.Debug("sending session hello on primary control stream")
	if len(g.streamManager.controlStreams) == 0 {
		return fmt.Errorf("controlStreams not initialized")
	}
	if len(g.circuitBreakers) == 0 {
		return fmt.Errorf("circuitBreakers not initialized")
	}
	stream := g.streamManager.controlStreams[0]
	circuitBreaker := g.circuitBreakers[0]

	if len(g.streamManager.controlSendMu) == 0 {
		return fmt.Errorf("controlSendMu not initialized")
	}
	sendMu := &g.streamManager.controlSendMu[0]
	err := RetryWithCircuitBreaker(context.Background(), g.retryConfig, circuitBreaker, func() error {
		sendMu.Lock()
		err := stream.Send(msg)
		sendMu.Unlock()
		return err
	})

	if err != nil {
		return fmt.Errorf("failed to send session hello: %w", err)
	}

	return nil
}

func (g *GravityClient) buildSessionHelloMessage() *pb.SessionMessage {
	// Convert existing deployments to protobuf format
	var existingDeployments []*pb.ExistingDeployment
	if provisioningProvider, ok := g.provider.(provider.ProvisioningProvider); ok {
		resources := provisioningProvider.Resources()

		g.logger.Debug("gathering current deployment state for server synchronization...")
		g.logger.Debug("found %d existing deployments to synchronize with server", len(resources))

		for _, res := range resources {
			g.logger.Debug("synchronizing deployment: ID=%s, IPv6=%s, Started=%s", res.GetId(), res.GetIpv6Address(), res.GetStarted().AsTime().Format("2006-01-02 15:04:05"))
			existingDeployments = append(existingDeployments, res)
		}

		if len(existingDeployments) > 0 {
			g.logger.Debug("sending %d existing deployments to gravity server for state synchronization", len(existingDeployments))
		} else {
			g.logger.Debug("no existing deployments to synchronize - this is a fresh session")
		}
	}

	// Create session hello
	sessionHello := &pb.SessionHello{
		ProtocolVersion: protocolVersion,
		ClientVersion:   g.clientVersion,
		ClientName:      g.clientName,
		Deployments:     existingDeployments,
		HostInfo:        g.hostInfo,
		Capabilities:    g.capabilities,
		InstanceId:      g.instanceID,
	}

	msg := &pb.SessionMessage{
		Id:       "session_hello",
		StreamId: "session_hello",
		MessageType: &pb.SessionMessage_SessionHello{
			SessionHello: sessionHello,
		},
	}
	return msg
}

func (g *GravityClient) sendSessionHelloOnStream(streamIndex int, stream pb.GravitySessionService_EstablishSessionClient) error {
	if stream == nil {
		g.streamManager.controlMu.RLock()
		if streamIndex < 0 || streamIndex >= len(g.streamManager.controlStreams) {
			g.streamManager.controlMu.RUnlock()
			return fmt.Errorf("control stream index %d out of range", streamIndex)
		}
		stream = g.streamManager.controlStreams[streamIndex]
		g.streamManager.controlMu.RUnlock()
	}

	if stream == nil {
		return fmt.Errorf("control stream %d is nil", streamIndex)
	}

	g.mu.RLock()
	if streamIndex < 0 || streamIndex >= len(g.circuitBreakers) {
		g.mu.RUnlock()
		return fmt.Errorf("circuit breaker index %d out of range (len=%d)", streamIndex, len(g.circuitBreakers))
	}
	cb := g.circuitBreakers[streamIndex]
	g.mu.RUnlock()

	if streamIndex < 0 || streamIndex >= len(g.streamManager.controlSendMu) {
		return fmt.Errorf("controlSendMu index %d out of range (len=%d)", streamIndex, len(g.streamManager.controlSendMu))
	}

	msg := g.buildSessionHelloMessage()
	sendMu := &g.streamManager.controlSendMu[streamIndex]
	return RetryWithCircuitBreaker(context.Background(), g.retryConfig, cb, func() error {
		sendMu.Lock()
		err := stream.Send(msg)
		sendMu.Unlock()
		return err
	})
}

// handleControlStream processes messages from a control stream
func (g *GravityClient) handleControlStream(streamIndex int, stream pb.GravitySessionService_EstablishSessionClient) {
	g.logger.Debug("handleControlStream %d called", streamIndex)
	defer func() {
		if r := recover(); r != nil {
			g.logger.Error("control stream %d panic: %v", streamIndex, r)
		}
		// Nil out the control stream slot so TunnelStats() reports
		// accurate HealthyControlStreams during reconnection.
		g.streamManager.controlMu.Lock()
		if streamIndex < len(g.streamManager.controlStreams) {
			g.streamManager.controlStreams[streamIndex] = nil
		}
		g.streamManager.controlMu.Unlock()
		g.logger.Debug("control stream %d handler exiting", streamIndex)
	}()

	g.logger.Debug("control stream %d handler started", streamIndex)
	for {
		g.logger.Trace("control stream %d waiting for message", streamIndex)
		msg, err := stream.Recv()
		if err != nil {
			// Determine if this is a recoverable disconnection
			isCanceled := false
			if st, ok := status.FromError(err); ok && st.Code() == codes.Canceled {
				isCanceled = true
				g.logger.Debug("control stream %d closed due to context cancellation", streamIndex)
			} else {
				g.logger.Error("control stream %d receive error: %v", streamIndex, err)
			}

			// Trigger reconnection for any stream death unless we're
			// intentionally closing. Context cancellation (codes.Canceled)
			// can happen during server restarts — not just graceful shutdown.
			g.mu.RLock()
			closing := g.closing
			g.mu.RUnlock()
			if !closing {
				isConnectionLost := isCanceled || errors.Is(err, io.EOF)
				if !isConnectionLost {
					if st, ok := status.FromError(err); ok && st.Code() == codes.Unavailable {
						isConnectionLost = true
					}
				}
				if isConnectionLost {
					g.logger.Warn("control stream %d connection lost, triggering reconnection", streamIndex)
					if g.multiEndpointMode.Load() {
						go g.handleEndpointDisconnection(streamIndex, fmt.Sprintf("control_stream_%d_error", streamIndex))
					} else {
						go g.handleServerDisconnection(fmt.Sprintf("control_stream_%d_error", streamIndex))
					}
				}
			}
			return
		}

		g.logger.Trace("control stream %d received message: ID=%s, Type=%T", streamIndex, msg.Id, msg.MessageType)
		g.processSessionMessage(streamIndex, msg)
	}
}

// handleTunnelStream processes packets from a tunnel stream
func (g *GravityClient) handleTunnelStream(streamIndex int, stream pb.GravitySessionService_StreamSessionPacketsClient, streamID string) {
	if g.traceLog {
		g.logger.Trace("handleTunnelStream %d called", streamIndex)
	}
	defer func() {
		if r := recover(); r != nil {
			g.logger.Error("tunnel stream %d panic: %v", streamIndex, r)
		}
		// Mark tunnel stream as unhealthy so TunnelStats() reports
		// accurate HealthyTunnelStreams during reconnection.
		g.streamManager.tunnelMu.Lock()
		if streamIndex < len(g.streamManager.tunnelStreams) {
			if si := g.streamManager.tunnelStreams[streamIndex]; si != nil {
				si.isHealthy = false
			}
		}
		g.streamManager.tunnelMu.Unlock()
	}()

	if g.traceLog {
		g.logger.Trace("handleTunnelStream: starting receive loop for stream %d", streamIndex)
	}
	for {
		if g.tracePackets {
			g.tracePacketLogger.Debug("handleTunnelStream: calling stream.Recv() for stream %d", streamIndex)
		}
		packet, err := stream.Recv()
		if err != nil {
			// Determine if this is a recoverable disconnection
			isCanceled := false
			if st, ok := status.FromError(err); ok && st.Code() == codes.Canceled {
				isCanceled = true
				g.logger.Debug("tunnel stream %d closed due to context cancellation", streamIndex)
			} else {
				g.logger.Error("tunnel stream %d receive error: %v", streamIndex, err)
			}

			// Trigger reconnection for any stream death unless we're
			// intentionally closing. Context cancellation can happen
			// during server restarts — not just graceful shutdown.
			g.mu.RLock()
			closing := g.closing
			g.mu.RUnlock()
			if !closing {
				isConnectionLost := isCanceled || errors.Is(err, io.EOF)
				if !isConnectionLost {
					if st, ok := status.FromError(err); ok && st.Code() == codes.Unavailable {
						isConnectionLost = true
					}
				}
				if isConnectionLost {
					g.handleTunnelStreamDisconnection(streamIndex)
				}
			}
			return
		}

		if debugTunnelPackets {
			if len(packet.Data) >= 40 {
				srcIP := net.IP(packet.Data[8:24])
				dstIP := net.IP(packet.Data[24:40])
				nextHdr := packet.Data[6]
				g.logger.Info("[tunnel-recv] stream %d: %d bytes SRC=%s DST=%s nextHdr=%d", streamIndex, len(packet.Data), srcIP, dstIP, nextHdr)
			} else {
				g.logger.Info("[tunnel-recv] stream %d: %d bytes (too small for IPv6)", streamIndex, len(packet.Data))
			}
		}

		// If enqueuedAt is set, log tunnel transit latency.
		// This is intentionally chatty — we'll dial it back later
		if enqueuedAtUs := packet.GetEnqueuedAtUs(); enqueuedAtUs > 0 && len(packet.Data) >= 54 {
			enqueuedAt := time.UnixMicro(enqueuedAtUs)
			tunnelLatency := time.Since(enqueuedAt)
			dstIP := make(net.IP, 16)
			copy(dstIP, packet.Data[24:40])
			srcPort := binary.BigEndian.Uint16(packet.Data[40:42])
			if g.traceLog {
				g.logger.Trace("gravity.packet.delivered dst=%s src_port=%d enqueued_at=%s tunnel_latency=%s", dstIP, srcPort, enqueuedAt.Format(time.RFC3339Nano), tunnelLatency)
			}
		}

		// Record reverse-flow binding so the response routes back through
		// the same endpoint that sent this inbound packet.
		if g.selector != nil && g.multiEndpointMode.Load() {
			g.streamManager.tunnelMu.RLock()
			var ep *GravityEndpoint
			if streamIndex >= 0 && streamIndex < len(g.streamManager.tunnelStreams) {
				if si := g.streamManager.tunnelStreams[streamIndex]; si != nil {
					connIdx := si.connIndex
					g.endpointsMu.RLock()
					if connIdx >= 0 && connIdx < len(g.endpoints) {
						ep = g.endpoints[connIdx]
					}
					g.endpointsMu.RUnlock()
				}
			}
			g.streamManager.tunnelMu.RUnlock()
			if ep != nil {
				g.selector.RecordInboundFlow(packet.Data, ep)
			}
		}

		// Track inbound packet from gravity stream
		g.inboundReceived.Add(1)
		g.inboundBytes.Add(uint64(len(packet.Data)))

		// Track per-stream receive metrics — streamID was captured at
		// goroutine creation, so we only need metricsMu (not tunnelMu).
		g.streamManager.metricsMu.Lock()
		if metrics := g.streamManager.streamMetrics[streamID]; metrics != nil {
			metrics.PacketsReceived++
			metrics.LastRecvUs = time.Now().UnixMicro()
			metrics.BytesReceived += int64(len(packet.Data))
		}
		g.streamManager.metricsMu.Unlock()

		// Handle tunnel keepalive echo — don't forward to TUN.
		// The probe was sent by sendTunnelKeepalives() and echoed by ion.
		// Receiving it proves the tunnel stream round-trips data.
		if IsTunnelKeepalive(packet.Data) {
			continue
		}

		// Forward packet to local processing
		pooledBuf := g.getBuffer(packet.Data)

		select {
		case g.inboundPackets <- pooledBuf:
			// Track channel high-water mark AFTER enqueue so the measurement
			// reflects the actual peak depth. Clamp to capacity since len()
			// can briefly exceed the value we'd see if sampled atomically.
			depth := int32(len(g.inboundPackets))
			if chanCap := int32(cap(g.inboundPackets)); depth > chanCap {
				depth = chanCap
			}
			if old := g.inboundHighWater.Load(); depth > old {
				g.inboundHighWater.CompareAndSwap(old, depth)
			}
			// Channel drained — if packets were dropped during backpressure, log a summary and reset.
			if dropped := g.droppedPackets.Swap(0); dropped > 0 {
				g.logger.Warn("tunnel stream %d: recovered after dropping %d packet(s)", streamIndex, dropped)
			}
		default:
			// Channel full, drop packet. Only log on the first drop of each burst.
			if g.droppedPackets.Add(1) == 1 {
				g.logger.Warn("tunnel stream %d: channel full, dropping packets", streamIndex)
			}
			g.inboundDropped.Add(1)
			g.returnBuffer(pooledBuf)
		}
	}
}

// handleTunnelStreamDisconnection resolves the owning endpoint for a tunnel
// stream and triggers either per-endpoint or full-server disconnection.
func (g *GravityClient) handleTunnelStreamDisconnection(streamIndex int) {
	reason := fmt.Sprintf("tunnel_stream_%d_error", streamIndex)
	g.logger.Trace("tunnel stream %d connection lost, triggering reconnection", streamIndex)

	if g.multiEndpointMode.Load() {
		connIdx := -1
		g.streamManager.tunnelMu.RLock()
		if streamIndex >= 0 && streamIndex < len(g.streamManager.tunnelStreams) {
			if si := g.streamManager.tunnelStreams[streamIndex]; si != nil {
				connIdx = si.connIndex
			}
		}
		g.streamManager.tunnelMu.RUnlock()
		if connIdx >= 0 {
			go g.handleEndpointDisconnection(connIdx, reason)
		} else {
			go g.handleServerDisconnection(reason)
		}
	} else {
		go g.handleServerDisconnection(reason)
	}
}

// processSessionMessage processes incoming session messages
func (g *GravityClient) processSessionMessage(streamIndex int, msg *pb.SessionMessage) {
	switch m := msg.MessageType.(type) {
	case *pb.SessionMessage_SessionHelloResponse:
		g.logger.Debug("received SessionHelloResponse: msgID=%s, streamID=%s, streamIndex=%d", msg.Id, msg.StreamId, streamIndex)
		g.helloAckedStreams.Store(streamIndex, true)
		g.handleSessionHelloResponse(streamIndex, msg.Id, m.SessionHelloResponse)
	case *pb.SessionMessage_RouteDeploymentResponse:
		g.handleRouteDeploymentResponse(msg.Id, m.RouteDeploymentResponse)
	case *pb.SessionMessage_RouteSandboxResponse:
		g.handleRouteSandboxResponse(msg.Id, m.RouteSandboxResponse)
	case *pb.SessionMessage_Unprovision:
		g.handleUnprovisionRequest(streamIndex, msg.Id, m.Unprovision)
	case *pb.SessionMessage_Ping:
		g.handlePingRequest(streamIndex, msg.Id, m.Ping)
	case *pb.SessionMessage_SessionClose:
		g.handleSessionCloseRequest(streamIndex, msg.Id, m.SessionClose)
	case *pb.SessionMessage_Pause:
		g.handlePauseRequest(streamIndex, msg.Id, m.Pause)
	case *pb.SessionMessage_Resume:
		g.handleResumeRequest(streamIndex, msg.Id, m.Resume)
	case *pb.SessionMessage_Response:
		g.handleGenericResponse(msg.Id, m.Response)
	case *pb.SessionMessage_Event:
		g.handleEvent(streamIndex, msg.Id, m.Event)
	case *pb.SessionMessage_Pong:
		g.handlePong(msg.Id, m.Pong)
	case *pb.SessionMessage_EvacuationPlan:
		g.handleEvacuationPlan(msg.Id, m.EvacuationPlan)
	case *pb.SessionMessage_RestoreSandboxTask:
		g.handleRestoreSandboxTask(msg.Id, m.RestoreSandboxTask)
	case *pb.SessionMessage_CheckpointUrlResponse:
		g.handleCheckpointURLResponse(msg.Id, m.CheckpointUrlResponse)
	case *pb.SessionMessage_MonitorCommand:
		g.mu.RLock()
		h := g.monitorCommandHandler
		g.mu.RUnlock()
		if h != nil {
			go func(cmd *pb.MonitorCommand) {
				defer func() {
					if r := recover(); r != nil {
						g.logger.Error("panic in monitor command handler: %v", r)
					}
				}()
				h(cmd)
			}(m.MonitorCommand)
		}
	case *pb.SessionMessage_MonitorReport:
		// Server should not send monitor reports to client — ignore
		g.logger.Debug("received unexpected monitor report from server, ignoring")
	default:
		g.logger.Debug("unhandled session message type: %T", m)
	}
}

// SetMonitorCommandHandler registers a callback for incoming MonitorCommand messages
// from the gravity server (e.g., interval adjustments, snapshot requests).
func (g *GravityClient) SetMonitorCommandHandler(handler func(cmd *pb.MonitorCommand)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.monitorCommandHandler = handler
}

// SetEvacuationCallback sets the callback called when evacuation plan processing completes.
func (g *GravityClient) SetEvacuationCallback(cb func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.evacuationCallback = cb
}

// Helper functions
func (g *GravityClient) handleSessionHelloResponse(streamIndex int, msgID string, response *pb.SessionHelloResponse) {
	g.logger.Debug("handleSessionHelloResponse called: streamIndex=%d, msgID=%s, machineID=%s, gravityServer=%s", streamIndex, msgID, response.MachineId, response.GravityServer)

	g.mu.Lock()

	// Store session response fields
	g.machineID = response.MachineId
	g.authorizationToken = response.MachineToken
	g.connectionIDs = append(g.connectionIDs, response.MachineId)

	// Only update config values when non-empty. Multi-tunnel reuse sessions
	// return minimal responses without these fields — don't overwrite the
	// good config from the primary session.
	g.logger.Info("[session-config] response: apiUrl=%q otlpUrl=%q env=%d hostMappings=%d machineId=%s server=%s",
		response.ApiUrl, response.OtlpUrl, len(response.Environment), len(response.HostMapping), response.MachineId, response.GravityServer)
	if response.OtlpUrl != "" {
		g.otlpURL = response.OtlpUrl
	}
	if response.OtlpKey != "" {
		g.otlpToken = response.OtlpKey
	}
	if response.ApiUrl != "" {
		g.apiURL = response.ApiUrl
	}
	if len(response.Environment) > 0 {
		g.hostEnvironment = response.Environment
	}
	if len(response.HostMapping) > 0 {
		g.hostMapping = response.HostMapping
	}
	if len(response.SubnetRoutes) > 0 {
		g.subnetRoutes = response.SubnetRoutes
	}
	if response.MachineSubnet != "" {
		g.machineSubnet = response.MachineSubnet
	}
	if response.Hostname != "" {
		g.sessionHostname = response.Hostname
	}
	if response.OrgId != "" {
		g.sessionOrgID = response.OrgId
	}
	if response.MachineToken != "" {
		g.machineToken = response.MachineToken
	}
	if response.MachineCertBundle != "" {
		g.machineCertBundle = response.MachineCertBundle
	}
	if len(response.SshPublicKey) > 0 {
		g.sshPublicKey = response.SshPublicKey
	}
	if len(response.SigningPublicKey) > 0 {
		g.signingPublicKey = response.SigningPublicKey
	}
	g.logger.Info("[session-config] effective: apiUrl=%q otlpUrl=%q hostMappings=%d subnetRoutes=%d machineSubnet=%q", g.apiURL, g.otlpURL, len(g.hostMapping), len(g.subnetRoutes), g.machineSubnet)

	g.mu.Unlock()

	signalConnectionID := func(id string) {
		select {
		case g.connectionIDChan <- id:
			if id == "" {
				g.logger.Debug("sent empty machine ID to signal session hello failure")
			} else {
				g.logger.Debug("machine ID sent to channel successfully")
			}
		default:
			g.logger.Warn("connectionIDChan full, dropped machine ID signal")
		}
	}

	closeSessionReady := func() {
		g.mu.Lock()
		select {
		case <-g.sessionReady:
			// Already closed
		default:
			close(g.sessionReady)
		}
		g.mu.Unlock()
	}

	// Validate MachineSubnet if present. Empty is allowed for backward
	// compatibility with ions that don't yet send it.
	if g.machineSubnet != "" {
		if _, _, err := net.ParseCIDR(g.machineSubnet); err != nil {
			g.logger.Error("session hello returned invalid MachineSubnet %q: %v", g.machineSubnet, err)
			signalConnectionID("")
			closeSessionReady()
			return
		}
	}

	// Configure provider with session info. Use preserved values (g.*)
	// for fields that multi-tunnel reuse sessions may leave empty.
	if err := g.provider.Configure(provider.Configuration{
		Server:            g,
		Context:           g.context,
		Logger:            g.logger,
		APIURL:            g.apiURL,
		SSHPublicKey:      g.sshPublicKey,
		TelemetryURL:      g.otlpURL,
		TelemetryAPIKey:   g.otlpToken,
		GravityURL:        g.url,
		AgentuityCACert:   g.caCert,
		HostMapping:       g.hostMapping,
		Environment:       g.hostEnvironment,
		SubnetRoutes:      g.subnetRoutes,
		Hostname:          g.sessionHostname,
		OrgID:             g.sessionOrgID,
		MachineToken:      g.machineToken,
		MachineID:         g.machineID,
		MachineCertBundle: g.machineCertBundle,
		MachineSubnet:     g.machineSubnet,
		SigningPublicKey:  g.signingPublicKey,
	}); err != nil {
		g.logger.Error("error configuring provider after session hello: %v", err)
		signalConnectionID("")
		closeSessionReady()
		return
	}
	g.logger.Debug("provider configured successfully")

	g.logger.Debug("configuring subnet routing for routes %v", g.subnetRoutes)
	if g.networkInterface != nil {
		if err := g.networkInterface.RouteTraffic(g.subnetRoutes); err != nil {
			g.logger.Error("failed to route traffic for gravity subnet: %v", err)
			signalConnectionID("")
			closeSessionReady()
			return
		}
	} else {
		g.logger.Debug("no network interface configured, skipping route setup")
	}

	// Send machine ID to channel to unblock tunnel stream establishment only
	// after provider/network setup is complete.
	signalConnectionID(response.MachineId)

	g.logger.Debug("session established successfully with machine ID: %s", response.MachineId)
	if provisioningProvider, ok := g.provider.(provider.ProvisioningProvider); ok {
		deploymentCount := len(provisioningProvider.Resources())
		if deploymentCount > 0 {
			g.logger.Debug("deployment state synchronization completed - server is now aware of %d existing deployments", deploymentCount)
		} else {
			g.logger.Debug("no existing deployments to synchronize - fresh session established")
		}
	}

	closeSessionReady()

	g.fireOnEndpointReady(streamIndex)
}

// fireOnEndpointReady safely invokes the OnEndpointReady callback in a
// goroutine with a panic guard. Passes the real endpoint index so hadron
// knows which gravity server just became ready.
func (g *GravityClient) fireOnEndpointReady(endpointIndex int) {
	if g.onEndpointReady == nil {
		return
	}
	g.logger.Info("firing OnEndpointReady callback for endpoint %d", endpointIndex)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				g.logger.Error("OnEndpointReady callback panicked for endpoint %d: %v", endpointIndex, r)
			}
		}()
		g.onEndpointReady(endpointIndex)
	}()
}

func (g *GravityClient) handleGenericResponse(msgID string, response *pb.ProtocolResponse) {
	g.logger.Trace("received generic response: msgID=%s, success=%v, error=%s, event=%s",
		msgID, response.Success, response.Error, response.Event)

	// Check if this is an error response for a session hello message
	if msgID == "session_hello" && !response.Success {
		g.logger.Error("session hello failed: %s", response.Error)
		select {
		case g.connectionIDChan <- "":
		default:
			g.logger.Warn("session hello failure: connectionIDChan full, failure signal dropped")
		}
		return
	}

	// Find pending request and send response
	g.pendingMu.RLock()
	ch, exists := g.pending[msgID]
	g.pendingMu.RUnlock()

	if exists {
		select {
		case ch <- response:
		default:
		}
		return
	}

	if !response.Success {
		g.pendingRouteSandboxMu.RLock()
		rsCh, rsExists := g.pendingRouteSandbox[msgID]
		g.pendingRouteSandboxMu.RUnlock()
		if rsExists {
			g.logger.Warn("received error response for route sandbox request msgID=%s: %s", msgID, response.Error)
			select {
			case rsCh <- routeSandboxResult{Error: response.Error}:
			default:
			}
			return
		}

		g.pendingRouteDeploymentMu.RLock()
		rdCh, rdExists := g.pendingRouteDeployment[msgID]
		g.pendingRouteDeploymentMu.RUnlock()
		if rdExists {
			g.logger.Warn("received error response for route deployment request msgID=%s: %s", msgID, response.Error)
			select {
			case rdCh <- routeDeploymentResult{Error: response.Error}:
			default:
			}
			return
		}

		g.pendingCheckpointURLMu.RLock()
		cuCh, cuExists := g.pendingCheckpointURL[msgID]
		g.pendingCheckpointURLMu.RUnlock()
		if cuExists {
			g.logger.Warn("received error response for checkpoint URL request msgID=%s: %s", msgID, response.Error)
			select {
			case cuCh <- checkpointURLResult{Error: response.Error}:
			default:
			}
			return
		}
	}

	g.logger.Trace("no pending request found for msgID: %s", msgID)
}

func (g *GravityClient) handleRouteDeploymentResponse(msgID string, response *pb.RouteDeploymentResponse) {
	g.logger.Debug("handleRouteDeploymentResponse: Received route deployment response for msgID=%s, ip=%s", msgID, response.Ip)

	// Find pending request and send response
	g.pendingRouteDeploymentMu.RLock()
	ch, exists := g.pendingRouteDeployment[msgID]
	g.pendingRouteDeploymentMu.RUnlock()

	if exists {
		select {
		case ch <- routeDeploymentResult{Response: response}:
		default:
		}
	} else {
		g.logger.Debug("handleRouteDeploymentResponse: No pending route deployment request found for msgID: %s", msgID)
	}
}

func (g *GravityClient) handleRouteSandboxResponse(msgID string, response *pb.RouteSandboxResponse) {
	g.logger.Debug("handleRouteSandboxResponse: Received route sandbox response for msgID=%s, ip=%s", msgID, response.Ip)

	// Find pending request and send response
	g.pendingRouteSandboxMu.RLock()
	ch, exists := g.pendingRouteSandbox[msgID]
	g.pendingRouteSandboxMu.RUnlock()

	if exists {
		select {
		case ch <- routeSandboxResult{Response: response}:
		default:
		}
	} else {
		g.logger.Debug("handleRouteSandboxResponse: No pending route sandbox request found for msgID: %s", msgID)
	}
}

func (g *GravityClient) handleCheckpointURLResponse(msgID string, response *pb.CheckpointURLResponse) {
	g.logger.Debug("handleCheckpointURLResponse: Received checkpoint URL response for msgID=%s, sandbox=%s, success=%v",
		msgID, response.SandboxId, response.Success)

	// Find pending request and send response
	g.pendingCheckpointURLMu.RLock()
	ch, exists := g.pendingCheckpointURL[msgID]
	g.pendingCheckpointURLMu.RUnlock()

	if exists {
		select {
		case ch <- checkpointURLResult{Response: response}:
		default:
		}
	} else {
		g.logger.Debug("handleCheckpointURLResponse: No pending checkpoint URL request found for msgID: %s", msgID)
	}
}

func (g *GravityClient) handleEvent(streamIndex int, msgID string, event *pb.ProtocolEvent) {
	g.logger.Debug("received event: id=%s, event=%s", msgID, event.Event)

	switch event.Event {
	case "close":
		g.logger.Trace("received close event from server")
		// For HA: disconnect and attempt reconnection instead of full shutdown
		if g.multiEndpointMode.Load() {
			go g.handleEndpointDisconnection(streamIndex, "close_event")
		} else {
			g.handleServerDisconnection("close_event")
		}
	case "provision":
		g.logger.Debug("received provision event from server")
		// Handle new deployment provisioning
		g.handleProvisionEvent(streamIndex, event)
	case "unprovision":
		g.logger.Debug("received unprovision event from server")
		// Handle deployment cleanup
		g.handleUnprovisionEvent(streamIndex, event)
	default:
		g.logger.Debug("unhandled event type: %s", event.Event)
	}
}

func (g *GravityClient) handleProvisionEvent(streamIndex int, event *pb.ProtocolEvent) {
	g.logger.Debug("handling provision event: %s", string(event.Payload))

	response := &pb.ProtocolResponse{
		Id:      event.Id,
		Event:   "provision",
		Success: true,
	}

	responseMsg := &pb.SessionMessage{
		Id: event.Id,
		MessageType: &pb.SessionMessage_Response{
			Response: response,
		},
	}

	if err := g.sendOnControlStream(streamIndex, responseMsg); err != nil {
		g.logger.Error("failed to send provision response: %v", err)
	} else {
		g.logger.Debug("sent provision response on stream %d", streamIndex)
	}
}

func (g *GravityClient) handleUnprovisionEvent(streamIndex int, event *pb.ProtocolEvent) {
	g.logger.Debug("handling unprovision event: %s", string(event.Payload))

	response := &pb.ProtocolResponse{
		Id:      event.Id,
		Event:   "unprovision",
		Success: true,
	}

	responseMsg := &pb.SessionMessage{
		Id: event.Id,
		MessageType: &pb.SessionMessage_Response{
			Response: response,
		},
	}

	if err := g.sendOnControlStream(streamIndex, responseMsg); err != nil {
		g.logger.Error("failed to send unprovision response: %v", err)
	} else {
		g.logger.Debug("sent unprovision response on stream %d", streamIndex)
	}
}

func (g *GravityClient) handlePong(msgID string, pong *pb.PongResponse) {
	if g.traceLog {
		g.logger.Trace("received pong response: id=%s", msgID)
	}
	_ = pong

	g.pongsReceived.Add(1)
	g.lastPongRecvUs.Store(time.Now().UnixMicro())

	// Find pending ping request and respond
	g.pendingMu.RLock()
	ch, exists := g.pending[msgID]
	g.pendingMu.RUnlock()

	if exists {
		// Convert to ProtocolResponse for compatibility
		protocolResp := &pb.ProtocolResponse{
			Id:      msgID,
			Event:   "pong",
			Success: true,
			Payload: nil,
		}

		select {
		case ch <- protocolResp:
		default:
		}

		// Clean up pending request
		g.pendingMu.Lock()
		delete(g.pending, msgID)
		g.pendingMu.Unlock()
	}
}

func (g *GravityClient) handleUnprovisionRequest(streamIndex int, msgID string, request *pb.UnprovisionRequest) {
	g.logger.Debug("received unprovision request: deployment_id=%s", request.DeploymentId)
	provisioningProvider, ok := g.provider.(provider.ProvisioningProvider)
	if !ok {
		return
	}

	// Call provider to deprovision the deployment
	ctx := context.WithoutCancel(g.context)
	err := provisioningProvider.Deprovision(ctx, request.DeploymentId, provider.DeprovisionReasonUnprovision)

	// Create generic response since there's no UnprovisionResponse message type
	var response *pb.ProtocolResponse
	if err != nil {
		g.logger.Error("unprovision failed for deployment %s: %v", request.DeploymentId, err)
		response = &pb.ProtocolResponse{
			Id:      msgID,
			Event:   "unprovision",
			Success: false,
			Error:   err.Error(),
		}
	} else {
		g.logger.Debug("unprovision successful for deployment %s", request.DeploymentId)
		response = &pb.ProtocolResponse{
			Id:      msgID,
			Event:   "unprovision",
			Success: true,
		}
	}

	// Send response back to gravity server
	responseMsg := &pb.SessionMessage{
		Id: msgID,
		MessageType: &pb.SessionMessage_Response{
			Response: response,
		},
	}

	err = g.sendOnControlStream(streamIndex, responseMsg)
	if err != nil {
		g.logger.Error("failed to send unprovision response: %v", err)
	} else {
		g.logger.Debug("sent unprovision response for deployment %s: success=%v", request.DeploymentId, response.Success)
	}
}

func (g *GravityClient) handleEvacuationPlan(msgID string, plan *pb.EvacuationPlan) {
	checkpointProvider, ok := g.provider.(provider.CheckpointProvider)
	supportsCheckpoint := ok && checkpointProvider.SupportsCheckpointRestore()

	if !ok {
		g.logger.Warn("received evacuation plan but provider does not support checkpoint/restore")
	} else if !supportsCheckpoint {
		g.logger.Warn("received evacuation plan but runtime does not support checkpoint/restore")
	} else {
		g.logger.Info("received evacuation plan: %d sandboxes to evacuate", len(plan.Sandboxes))
	}
	_ = msgID

	g.handlerWg.Add(1)
	go func() {
		defer g.handlerWg.Done()

		var results []*pb.SandboxCheckpointed

		if supportsCheckpoint {
			ctx := context.WithoutCancel(g.context)
			results = checkpointProvider.HandleEvacuationPlan(ctx, plan.Sandboxes)
		} else {
			// Build failure results for every sandbox so Gravity knows none were checkpointed
			reason := "provider does not support checkpoint/restore"
			if ok {
				reason = "runtime does not support checkpoint/restore"
			}
			results = make([]*pb.SandboxCheckpointed, len(plan.Sandboxes))
			for i, s := range plan.Sandboxes {
				results[i] = &pb.SandboxCheckpointed{
					SandboxId: s.SandboxId,
					Success:   false,
					Error:     reason,
				}
			}
		}

		// Ensure results covers every sandbox in the plan. The provider may
		// return a shorter slice or leave nil entries; fill gaps with failures.
		if len(results) < len(plan.Sandboxes) {
			padded := make([]*pb.SandboxCheckpointed, len(plan.Sandboxes))
			copy(padded, results)
			results = padded
		}
		for i, result := range results {
			if result == nil {
				sandboxID := ""
				if i < len(plan.Sandboxes) {
					sandboxID = plan.Sandboxes[i].SandboxId
				}
				results[i] = &pb.SandboxCheckpointed{
					SandboxId: sandboxID,
					Success:   false,
					Error:     "checkpoint result missing",
				}
			}
		}

		for _, result := range results {
			msgID := generateMessageID()
			msg := &pb.SessionMessage{
				Id: msgID,
				MessageType: &pb.SessionMessage_SandboxCheckpointed{
					SandboxCheckpointed: result,
				},
			}

			responseChan := make(chan *pb.ProtocolResponse, 1)
			g.pendingMu.Lock()
			g.pending[msgID] = responseChan
			g.pendingMu.Unlock()

			if err := g.sendSessionMessageAsync(msg); err != nil {
				g.logger.Error("failed to send SandboxCheckpointed for %s: %v", result.SandboxId, err)
				g.pendingMu.Lock()
				delete(g.pending, msgID)
				g.pendingMu.Unlock()
				continue
			}

			ackTimer := time.NewTimer(10 * time.Second)
			select {
			case resp := <-responseChan:
				ackTimer.Stop()
				if resp.Success {
					g.logger.Info("SandboxCheckpointed acknowledged for %s", result.SandboxId)
				} else {
					g.logger.Warn("SandboxCheckpointed failed for %s: %s", result.SandboxId, resp.Error)
				}
			case <-ackTimer.C:
				g.logger.Warn("timeout waiting for SandboxCheckpointed ack for %s", result.SandboxId)
			}

			g.pendingMu.Lock()
			delete(g.pending, msgID)
			g.pendingMu.Unlock()
		}

		g.mu.RLock()
		cb := g.evacuationCallback
		g.mu.RUnlock()
		if cb != nil {
			cb()
		}
	}()
}

func (g *GravityClient) handleRestoreSandboxTask(msgID string, task *pb.RestoreSandboxTask) {
	checkpointProvider, ok := g.provider.(provider.CheckpointProvider)
	if !ok {
		result := &pb.SandboxRestored{
			SandboxId: task.SandboxId,
			Success:   false,
			Error:     "provider does not support checkpoint/restore",
		}
		msg := &pb.SessionMessage{
			Id: generateMessageID(),
			MessageType: &pb.SessionMessage_SandboxRestored{
				SandboxRestored: result,
			},
		}
		if err := g.sendSessionMessageAsync(msg); err != nil {
			g.logger.Error("failed to send SandboxRestored for %s: %v", task.SandboxId, err)
		}
		return
	}

	if !checkpointProvider.SupportsCheckpointRestore() {
		result := &pb.SandboxRestored{
			SandboxId: task.SandboxId,
			Success:   false,
			Error:     "runtime does not support checkpoint/restore",
		}
		msg := &pb.SessionMessage{
			Id: generateMessageID(),
			MessageType: &pb.SessionMessage_SandboxRestored{
				SandboxRestored: result,
			},
		}
		if err := g.sendSessionMessageAsync(msg); err != nil {
			g.logger.Error("failed to send SandboxRestored for %s: %v", task.SandboxId, err)
		}
		return
	}

	g.logger.Info("received restore sandbox task: sandbox=%s checkpoint=%s", task.SandboxId, task.CheckpointId)
	_ = msgID

	g.handlerWg.Add(1)
	go func() {
		defer g.handlerWg.Done()

		ctx := context.WithoutCancel(g.context)
		result := checkpointProvider.HandleRestoreSandboxTask(ctx, task)
		if result == nil {
			result = &pb.SandboxRestored{
				SandboxId: task.SandboxId,
				Success:   false,
				Error:     "restore returned nil result",
			}
		}
		msg := &pb.SessionMessage{
			Id: generateMessageID(),
			MessageType: &pb.SessionMessage_SandboxRestored{
				SandboxRestored: result,
			},
		}
		if err := g.sendSessionMessageAsync(msg); err != nil {
			g.logger.Error("failed to send SandboxRestored for %s: %v", result.SandboxId, err)
		}
	}()
}

func (g *GravityClient) handlePingRequest(streamIndex int, msgID string, request *pb.PingRequest) {
	if g.traceLog {
		g.logger.Trace("received ping request: id=%s", msgID)
	}

	pongMsg := &pb.SessionMessage{
		Id: msgID,
		MessageType: &pb.SessionMessage_Pong{
			Pong: &pb.PongResponse{
				Timestamp: request.Timestamp,
			},
		},
	}

	err := g.sendOnControlStream(streamIndex, pongMsg)
	if err != nil {
		g.logger.Error("failed to send pong response on stream %d: %v", streamIndex, err)
	} else {
		if g.traceLog {
			g.logger.Trace("sent pong response for ping %s on stream %d", msgID, streamIndex)
		}
	}
}

func (g *GravityClient) handleSessionCloseRequest(streamIndex int, msgID string, request *pb.SessionCloseRequest) {
	g.logger.Debug("received session close request: reason=%s", request.Reason)

	response := &pb.ProtocolResponse{
		Id:      msgID,
		Event:   "session_close",
		Success: true,
	}

	responseMsg := &pb.SessionMessage{
		Id: msgID,
		MessageType: &pb.SessionMessage_Response{
			Response: response,
		},
	}

	if err := g.sendOnControlStream(streamIndex, responseMsg); err != nil {
		g.logger.Debug("failed to send session close response on stream %d: %v", streamIndex, err)
	}

	g.logger.Info("server requested session close, attempting reconnection for HA")
	if g.multiEndpointMode.Load() {
		go g.handleEndpointDisconnection(streamIndex, "session_close_request")
	} else {
		go g.handleServerDisconnection("session_close_request")
	}
}

func (g *GravityClient) handlePauseRequest(streamIndex int, msgID string, request *pb.PauseRequest) {
	g.logger.Debug("received pause request: reason=%s", request.Reason)

	g.logger.Debug("pausing hadron client operations: %s", request.Reason)

	response := &pb.ProtocolResponse{
		Id:      msgID,
		Event:   "pause",
		Success: true,
	}

	responseMsg := &pb.SessionMessage{
		Id: msgID,
		MessageType: &pb.SessionMessage_Response{
			Response: response,
		},
	}

	err := g.sendOnControlStream(streamIndex, responseMsg)
	if err != nil {
		g.logger.Error("failed to send pause response on stream %d: %v", streamIndex, err)
	} else {
		g.logger.Debug("sent pause response on stream %d, operations paused", streamIndex)
	}
}

func (g *GravityClient) handleResumeRequest(streamIndex int, msgID string, request *pb.ResumeRequest) {
	g.logger.Debug("received resume request: reason=%s", request.Reason)

	g.logger.Debug("resuming hadron client operations: %s", request.Reason)

	response := &pb.ProtocolResponse{
		Id:      msgID,
		Event:   "resume",
		Success: true,
	}

	responseMsg := &pb.SessionMessage{
		Id: msgID,
		MessageType: &pb.SessionMessage_Response{
			Response: response,
		},
	}

	err := g.sendOnControlStream(streamIndex, responseMsg)
	if err != nil {
		g.logger.Error("failed to send resume response on stream %d: %v", streamIndex, err)
	} else {
		g.logger.Debug("sent resume response on stream %d, operations resumed", streamIndex)
	}
}

func (g *GravityClient) sendOnControlStream(streamIndex int, msg *pb.SessionMessage) error {
	g.streamManager.controlMu.RLock()
	if streamIndex < 0 || streamIndex >= len(g.streamManager.controlStreams) {
		g.streamManager.controlMu.RUnlock()
		return fmt.Errorf("control stream index %d out of range", streamIndex)
	}
	stream := g.streamManager.controlStreams[streamIndex]
	g.streamManager.controlMu.RUnlock()
	if stream == nil {
		return fmt.Errorf("control stream %d is nil", streamIndex)
	}
	// gRPC Send is not safe for concurrent use; serialize per stream.
	if streamIndex < 0 || streamIndex >= len(g.streamManager.controlSendMu) {
		return fmt.Errorf("controlSendMu index %d out of range (len=%d)", streamIndex, len(g.streamManager.controlSendMu))
	}
	g.streamManager.controlSendMu[streamIndex].Lock()
	err := stream.Send(msg)
	g.streamManager.controlSendMu[streamIndex].Unlock()
	return err
}

// Interface methods

func (g *GravityClient) SendPacket(data []byte) error {
	select {
	case g.outboundPackets <- data:
		return nil
	default:
		return fmt.Errorf("outbound packet channel full")
	}
}

func (g *GravityClient) GetInboundPackets() <-chan *PooledBuffer {
	return g.inboundPackets
}

func (g *GravityClient) GetTextMessages() <-chan *PooledBuffer {
	return g.textMessages
}

func (g *GravityClient) IsConnected() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.connected
}

// WaitForSession blocks until the session is fully authenticated and configured,
// or the timeout/context expires.
func (g *GravityClient) WaitForSession(timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		// Read sessionReady under lock to avoid racing with reconnection
		// which replaces the channel.
		g.mu.RLock()
		ready := g.sessionReady
		g.mu.RUnlock()

		select {
		case <-ready:
			// Re-check that this is still the active channel. Reconnect may have
			// swapped sessionReady between snapshot and channel close.
			g.mu.RLock()
			current := g.sessionReady
			g.mu.RUnlock()
			if current == ready {
				return nil
			}
			continue
		case <-timer.C:
			return fmt.Errorf("timeout waiting for session to be ready")
		case <-g.ctx.Done():
			return fmt.Errorf("context cancelled while waiting for session ready")
		}
	}
}

func (g *GravityClient) stop() error {
	g.logger.Debug("stop called")
	g.mu.Lock()
	if g.closing {
		g.mu.Unlock()
		return nil
	}
	g.closing = true
	g.mu.Unlock()

	// Wait for in-flight checkpoint/restore handlers to finish so their
	// response messages reach the server before we tear down streams.
	g.handlerWg.Wait()

	g.mu.Lock()
	if g.connectionCancel != nil {
		g.connectionCancel()
	}
	g.cancel()

	g.cleanup()
	g.connected = false
	g.mu.Unlock()

	return nil
}

// Close will shutdown the client
func (g *GravityClient) Close() error {
	g.logger.Debug("close called")
	var err error
	g.once.Do(func() {
		err = g.stop()
		close(g.closed)
	})
	return err
}

// Disconnected will wait for the client to be disconnected or the ctx to be cancelled
func (g *GravityClient) Disconnected(ctx context.Context) {
	g.logger.Debug("waiting for client to be disconnected")
	select {
	case <-ctx.Done():
		g.logger.Debug("client disconnected from context cancelation")
		return
	case _, ok := <-g.closed:
		g.logger.Debug("client disconnected from disconnection (%v)", ok)
		return
	}
}

// handleServerDisconnection handles server-initiated disconnection for HA
func (g *GravityClient) handleServerDisconnection(reason string) {
	g.mu.Lock()
	if g.closing {
		g.logger.Debug("already closing, ignoring disconnection event")
		g.mu.Unlock()
		return
	}

	if g.reconnecting {
		g.logger.Debug("reconnection already in progress, ignoring additional disconnection event: %s", reason)
		g.mu.Unlock()
		return
	}

	wasConnected := g.connected
	ni := g.networkInterface

	g.logger.Info("handling server disconnection: %s", reason)

	// Mark as disconnected but don't close completely
	g.connected = false
	g.sessionReady = make(chan struct{})
	g.mu.Unlock()

	if ni != nil {
		if err := ni.UnrouteTraffic(); err != nil {
			g.logger.Error("failed to unroute traffic: %v", err)
		}
	} else {
		g.logger.Debug("no tunInterface present, skipping traffic unrouting")
	}

	g.mu.Lock()
	// Clean up current connections without full shutdown
	g.disconnectStreams()

	if wasConnected && g.skipAutoReconnect {
		g.logger.Debug("client is configured to skip auto-reconnect")
		g.closed <- struct{}{}
		g.mu.Unlock()
		return
	}

	g.reconnecting = true
	g.mu.Unlock()

	// Start reconnection process in background
	go g.attemptReconnection(reason)
}

// triggerEndpointReconnectByURL finds the endpoint index for a URL and
// triggers disconnection + reconnection. Used when an endpoint has a
// healthy control stream but no healthy tunnel streams.
func (g *GravityClient) triggerEndpointReconnectByURL(url string) {
	g.endpointsMu.RLock()
	idx := -1
	for i, ep := range g.endpoints {
		if ep != nil && ep.URL == url {
			idx = i
			break
		}
	}
	g.endpointsMu.RUnlock()
	if idx < 0 {
		return
	}

	// Dispatch to a goroutine that re-validates the URL before tearing down
	// the endpoint. Between our lookup and the disconnect, the slot may have
	// been recycled (reconnection replaced the endpoint). Re-checking avoids
	// accidentally tearing down a newly-cycled healthy endpoint.
	go func(expectedIdx int, expectedURL string) {
		g.endpointsMu.RLock()
		if expectedIdx >= len(g.endpoints) || g.endpoints[expectedIdx] == nil || g.endpoints[expectedIdx].URL != expectedURL {
			g.endpointsMu.RUnlock()
			return // endpoint was replaced — skip
		}
		g.endpointsMu.RUnlock()
		g.handleEndpointDisconnection(expectedIdx, "no healthy tunnel streams")
	}(idx, url)
}

// triggerAllEndpointReconnections forces reconnection for every unhealthy
// endpoint that isn't already reconnecting. Healthy endpoints are skipped to
// prevent a single-endpoint failure from cascading to all endpoints.
//
// Called when WritePacket fails repeatedly or the tunnel liveness monitor
// detects stale streams. The healthy-endpoint check acts as a safety net:
// callers should ideally target specific endpoints, but if they invoke this
// function, only genuinely broken endpoints will be disrupted.
func (g *GravityClient) triggerAllEndpointReconnections(reason string) {
	g.mu.RLock()
	if g.closing {
		g.mu.RUnlock()
		return
	}
	g.mu.RUnlock()

	// Snapshot endpoint URL and health state while holding the lock. The
	// slice header copy (endpoints = g.endpoints) is safe for the header
	// itself, but entries can be mutated by other goroutines after release.
	// Copying into a local struct slice avoids reading stale/torn values.
	type epSnapshot struct {
		url     string
		healthy bool
	}
	g.endpointsMu.RLock()
	snapshots := make([]epSnapshot, len(g.endpoints))
	for i, ep := range g.endpoints {
		if ep != nil {
			snapshots[i] = epSnapshot{url: ep.URL, healthy: ep.healthy.Load()}
		}
	}
	g.endpointsMu.RUnlock()

	reconnected := 0
	skipped := 0
	for i := range g.endpointReconnecting {
		// Safety net: skip endpoints that are still healthy. This prevents a
		// single-endpoint failure from cascading to all endpoints — the root
		// cause of total outage when one ion dies while others are fine.
		if i < len(snapshots) && snapshots[i].healthy {
			skipped++
			g.logger.Debug("aggressive reconnect: skipping healthy endpoint %d (%s)", i, reason)
			continue
		}
		if g.endpointReconnecting[i].CompareAndSwap(false, true) {
			reconnected++
			var epURL string
			if i < len(snapshots) {
				epURL = snapshots[i].url
			}
			g.logger.Info("aggressive reconnect: triggering endpoint %d (%s) reconnection (%s)", i, epURL, reason)
			g.disconnectEndpointStreams(i)
			go g.reconnectEndpoint(i, reason)
		}
	}
	if skipped > 0 {
		g.logger.Info("aggressive reconnect: skipped %d healthy endpoint(s), reconnected %d (%s)", skipped, reconnected, reason)
	}
}

func (g *GravityClient) handleEndpointDisconnection(endpointIndex int, reason string) {
	g.mu.RLock()
	if g.closing {
		g.mu.RUnlock()
		return
	}
	g.mu.RUnlock()

	if endpointIndex < 0 || endpointIndex >= len(g.endpointReconnecting) {
		g.logger.Warn("invalid endpoint index %d for disconnection reason=%s", endpointIndex, reason)
		go g.handleServerDisconnection(reason)
		return
	}

	if !g.endpointReconnecting[endpointIndex].CompareAndSwap(false, true) {
		g.logger.Debug("endpoint %d already reconnecting, ignoring disconnection: %s", endpointIndex, reason)
		return
	}

	g.logger.Info("handling endpoint %d disconnection: %s", endpointIndex, reason)
	g.disconnectEndpointStreams(endpointIndex)

	if g.hasHealthyEndpoint() {
		g.logger.Info("operating in degraded mode: endpoint %d down, other endpoint(s) still healthy", endpointIndex)
	} else {
		g.logger.Warn("all endpoints down, reconnecting endpoint %d independently", endpointIndex)
	}
	// Always use per-endpoint reconnection in multi-endpoint mode.
	// Each endpoint reconnects independently as its gravity server
	// becomes available. This is better than full reconnect which
	// requires ALL servers to be up simultaneously.
	go g.reconnectEndpoint(endpointIndex, reason)
}

func (g *GravityClient) disconnectEndpointStreams(endpointIndex int) {
	g.streamManager.controlMu.Lock()
	if endpointIndex >= 0 && endpointIndex < len(g.streamManager.cancels) {
		if cancel := g.streamManager.cancels[endpointIndex]; cancel != nil {
			cancel()
			g.streamManager.cancels[endpointIndex] = nil
		}
	}
	if endpointIndex >= 0 && endpointIndex < len(g.streamManager.controlStreams) {
		g.streamManager.controlStreams[endpointIndex] = nil
	}
	if endpointIndex >= 0 && endpointIndex < len(g.streamManager.contexts) {
		g.streamManager.contexts[endpointIndex] = nil
	}
	g.streamManager.controlMu.Unlock()

	g.streamManager.tunnelMu.Lock()
	for _, si := range g.streamManager.tunnelStreams {
		if si != nil && si.connIndex == endpointIndex {
			if si.stream != nil {
				_ = si.stream.CloseSend()
			}
			si.isHealthy = false
		}
	}
	g.streamManager.tunnelMu.Unlock()

	g.streamManager.healthMu.Lock()
	if endpointIndex >= 0 && endpointIndex < len(g.streamManager.connectionHealth) {
		g.streamManager.connectionHealth[endpointIndex] = false
	}
	g.streamManager.healthMu.Unlock()

	g.mu.Lock()
	if endpointIndex >= 0 && endpointIndex < len(g.connections) {
		if conn := g.connections[endpointIndex]; conn != nil {
			_ = conn.Close()
			g.connections[endpointIndex] = nil
		}
	}
	if endpointIndex >= 0 && endpointIndex < len(g.sessionClients) {
		g.sessionClients[endpointIndex] = nil
	}
	g.mu.Unlock()

	g.endpointsMu.RLock()
	if endpointIndex >= 0 && endpointIndex < len(g.endpoints) && g.endpoints[endpointIndex] != nil {
		g.endpoints[endpointIndex].healthy.Store(false)
		g.wakePeerDiscovery()
	}
	g.endpointsMu.RUnlock()

	g.rebuildEndpointStreamIndices()
	g.refreshEndpointHealth()
}

func (g *GravityClient) hasHealthyEndpoint() bool {
	hasControl := false
	g.streamManager.controlMu.RLock()
	for _, stream := range g.streamManager.controlStreams {
		if stream != nil {
			hasControl = true
			break
		}
	}
	g.streamManager.controlMu.RUnlock()
	if !hasControl {
		return false
	}

	g.streamManager.tunnelMu.RLock()
	defer g.streamManager.tunnelMu.RUnlock()
	for _, si := range g.streamManager.tunnelStreams {
		if si != nil && si.isHealthy {
			return true
		}
	}
	return false
}

func (g *GravityClient) preferredTLSServerName(endpointURL string) string {
	if strings.TrimSpace(endpointURL) == "" {
		return ""
	}
	// Extract hostname directly from the URL without DNS resolution.
	// parseGRPCURL resolves /etc/hosts which can replace the hostname
	// with an IP, breaking TLS SNI. We need the original hostname.
	raw := endpointURL
	if strings.HasPrefix(raw, "grpc://") {
		raw = "https://" + raw[7:] // make it parseable by url.Parse
	} else if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return g.defaultServerName
	}
	host := u.Hostname()
	if host == "" || net.ParseIP(host) != nil {
		return g.defaultServerName
	}
	return host
}

func (g *GravityClient) resetEndpointFailureCount(endpointIndex int) {
	g.mu.RLock()
	if endpointIndex >= 0 && endpointIndex < len(g.endpointFailCount) {
		g.endpointFailCount[endpointIndex].Store(0)
	}
	g.mu.RUnlock()
}

func (g *GravityClient) incrementEndpointFailureCount(endpointIndex int) int32 {
	g.mu.RLock()
	if endpointIndex < 0 || endpointIndex >= len(g.endpointFailCount) {
		g.mu.RUnlock()
		return 0
	}
	count := g.endpointFailCount[endpointIndex].Add(1)
	g.mu.RUnlock()
	return count
}

func (g *GravityClient) handleEndpointReconnectFailure(endpointIndex int, currentURL string) string {
	failures := g.incrementEndpointFailureCount(endpointIndex)
	if failures == 0 {
		return currentURL
	}

	if failures%endpointDiscoveryRefreshFailureThreshold != 0 {
		return currentURL
	}

	g.logger.Info("endpoint %d reached %d consecutive reconnection failures; refreshing service discovery",
		endpointIndex, failures)

	refreshedURL := g.refreshFailingEndpointFromDiscovery(endpointIndex, currentURL)
	if refreshedURL == "" || refreshedURL == currentURL {
		g.logger.Info("endpoint %d discovery refresh found no replacement after %d failures", endpointIndex, failures)
		return currentURL
	}

	g.logger.Info("endpoint %d discovery refresh replaced %s -> %s after %d failures",
		endpointIndex, currentURL, refreshedURL, failures)
	g.resetEndpointFailureCount(endpointIndex)
	return refreshedURL
}

func (g *GravityClient) refreshFailingEndpointFromDiscovery(endpointIndex int, currentURL string) string {
	if g.discoveryResolveFunc == nil {
		return ""
	}

	candidates := g.discoveryResolveFunc()
	if len(candidates) == 0 {
		return ""
	}

	unique := make([]string, 0, len(candidates))
	seen := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		unique = append(unique, c)
	}
	if len(unique) == 0 {
		return ""
	}

	inUse := make(map[string]bool)
	g.endpointsMu.RLock()
	for i, ep := range g.endpoints {
		if i == endpointIndex || ep == nil || strings.TrimSpace(ep.URL) == "" {
			continue
		}
		inUse[ep.URL] = true
	}
	g.endpointsMu.RUnlock()

	var replacement string
	for _, c := range unique {
		if c == currentURL || inUse[c] {
			continue
		}
		replacement = c
		break
	}
	if replacement == "" {
		return ""
	}

	tlsServerName := g.preferredTLSServerName(replacement)

	g.endpointsMu.Lock()
	if endpointIndex >= 0 && endpointIndex < len(g.endpoints) && g.endpoints[endpointIndex] != nil {
		g.endpoints[endpointIndex].URL = replacement
		if tlsServerName != "" {
			g.endpoints[endpointIndex].TLSServerName = tlsServerName
		}
	}
	g.endpointsMu.Unlock()

	g.mu.Lock()
	if endpointIndex >= 0 && endpointIndex < len(g.connectionURLs) {
		g.connectionURLs[endpointIndex] = replacement
	}
	g.mu.Unlock()

	return replacement
}

func (g *GravityClient) reconnectEndpoint(endpointIndex int, reason string) {
	defer g.endpointReconnecting[endpointIndex].Store(false)

	g.mu.RLock()
	if g.closing {
		g.mu.RUnlock()
		return
	}
	g.mu.RUnlock()

	// Prefer endpoints[i].URL which may have been updated by reResolveEndpointURL
	// to a healthy IP. connectionURLs[i] can be stale (pointing to a dead ion
	// from a previous rollout). Fall back to connectionURLs only if endpoints
	// doesn't have a URL (e.g., during initial setup before endpoints are populated).
	endpointURL := ""
	g.endpointsMu.RLock()
	if endpointIndex >= 0 && endpointIndex < len(g.endpoints) && g.endpoints[endpointIndex] != nil {
		endpointURL = g.endpoints[endpointIndex].URL
	}
	g.endpointsMu.RUnlock()

	if strings.TrimSpace(endpointURL) == "" {
		g.mu.RLock()
		if endpointIndex >= 0 && endpointIndex < len(g.connectionURLs) {
			endpointURL = g.connectionURLs[endpointIndex]
		}
		g.mu.RUnlock()
	}
	if strings.TrimSpace(endpointURL) == "" {
		g.logger.Error("endpoint %d reconnection aborted: missing endpoint URL", endpointIndex)
		return
	}

	g.logger.Info("starting endpoint %d (%s) reconnection due to: %s", endpointIndex, endpointURL, reason)

	// Retry indefinitely with capped exponential backoff. Per-endpoint
	// reconnection must never give up — the Gravity server may be down
	// for minutes (restart, deploy, OOM) and hadron should recover as
	// soon as it comes back. Only stop on client shutdown or context
	// cancellation.
	backoff := time.Second
	maxBackoff := 3 * time.Second
	attempt := 0

	for {
		g.mu.RLock()
		closing := g.closing
		g.mu.RUnlock()
		if closing {
			return
		}

		attempt++

		// Re-resolve DNS on every attempt. When ions restart, they get
		// new connections and the old IP may be stale. Re-resolving
		// discovers any new/changed IPs immediately.
		if newURL := g.reResolveEndpointURL(endpointIndex, endpointURL); newURL != "" && newURL != endpointURL {
			g.logger.Info("endpoint %d re-resolved DNS: %s → %s", endpointIndex, endpointURL, newURL)
			endpointURL = newURL
		}

		g.logger.Info("endpoint %d reconnection attempt %d", endpointIndex, attempt)
		if err := g.reconnectSingleEndpoint(endpointIndex, endpointURL); err != nil {
			g.logger.Warn("endpoint %d reconnection attempt %d failed: %v", endpointIndex, attempt, err)
			endpointURL = g.handleEndpointReconnectFailure(endpointIndex, endpointURL)
		} else {
			g.resetEndpointFailureCount(endpointIndex)
			g.logger.Info("endpoint %d (%s) reconnected successfully after %d attempt(s)", endpointIndex, endpointURL, attempt)
			return
		}

		// Jitter: 10% of backoff
		jitter := time.Duration(float64(backoff) * 0.1 * (float64(time.Now().UnixNano()%100) / 100.0))
		select {
		case <-time.After(backoff + jitter):
			if backoff < maxBackoff {
				backoff = min(backoff*2, maxBackoff)
			}
		case <-g.ctx.Done():
			return
		}
	}
}

// reResolveEndpointURL re-resolves DNS for an endpoint that was originally
// resolved from a hostname to an IP. Returns the new URL if the IP changed,
// or empty string if no change or re-resolution isn't applicable.
//
// When DNS returns a mix of live and stale IPs (e.g., after multiple rollouts
// without DNS cleanup), this function avoids IPs already in use by other
// endpoints and skips the current failed IP. Each re-resolve call picks a
// different candidate, cycling through the pool until a live one is found.
func (g *GravityClient) reResolveEndpointURL(endpointIndex int, currentURL string) string {
	// Get the original hostname from TLSServerName (set during initial DNS resolution)
	g.endpointsMu.RLock()
	var originalHostname string
	if endpointIndex >= 0 && endpointIndex < len(g.endpoints) && g.endpoints[endpointIndex] != nil {
		originalHostname = g.endpoints[endpointIndex].TLSServerName
	}
	// Collect IPs already used by other endpoints so we don't pick a
	// known-failing address that another endpoint is already retrying.
	inUse := make(map[string]bool, len(g.endpoints))
	for i, ep := range g.endpoints {
		if ep == nil || i == endpointIndex {
			continue
		}
		if parsed, err := g.parseGRPCURL(ep.URL); err == nil {
			if h, _, splitErr := net.SplitHostPort(parsed); splitErr == nil {
				inUse[h] = true
			}
		}
	}
	g.endpointsMu.RUnlock()

	if originalHostname == "" {
		return "" // Not an IP-resolved endpoint, nothing to re-resolve
	}

	// Extract port and current IP from URL
	parsedHost, err := g.parseGRPCURL(currentURL)
	if err != nil {
		return ""
	}
	currentHost, port, _ := net.SplitHostPort(parsedHost)
	if port == "" {
		port = "443"
	}

	// Re-resolve the original hostname
	lookupFn := g.dnsLookupMulti
	if lookupFn == nil {
		lookupFn = dns.DefaultDNS.LookupMulti
	}
	ok, ips, err := lookupFn(g.context, originalHostname)
	if err != nil || !ok || len(ips) == 0 {
		g.logger.Debug("endpoint %d DNS re-resolve for %s failed: %v", endpointIndex, originalHostname, err)
		return ""
	}

	// Skip health probes during reconnection. The 503 health check creates
	// a chicken-and-egg problem: ion returns 503 ("no routable hadrons")
	// until hadron connects, but hadron waits up to 15s for the 503 to
	// clear. Just connect directly — if the ion is up, the gRPC dial
	// will succeed; if not, the dial timeout handles it.
	probeOK := make(map[string]bool, len(ips))
	for _, ip := range ips {
		ipStr := ip.String()
		if ipStr == currentHost {
			continue
		}
		probeOK[ipStr] = true // assume healthy — let gRPC dial verify
	}

	// In multi-endpoint mode, NEVER connect two endpoints to the same IP.
	// Having multiple connections to the same ion provides no redundancy —
	// if that ion dies, ALL overlapping endpoints go down simultaneously.
	multiMode := g.multiEndpointMode.Load()

	// First pass: healthy + not in use by another endpoint (best case).
	for _, ip := range ips {
		ipStr := ip.String()
		if ipStr != currentHost && !inUse[ipStr] && probeOK[ipStr] {
			newURL := "grpc://" + net.JoinHostPort(ipStr, port)
			g.logger.Info("endpoint %d health probe passed for %s", endpointIndex, ipStr)
			g.endpointsMu.Lock()
			if endpointIndex >= 0 && endpointIndex < len(g.endpoints) && g.endpoints[endpointIndex] != nil {
				g.endpoints[endpointIndex].URL = newURL
			}
			g.endpointsMu.Unlock()
			return newURL
		}
	}

	// Second pass: healthy but no unique IP available. Only allow overlap
	// in single-endpoint mode. In multi-endpoint mode, overlapping defeats
	// the purpose of having multiple connections.
	if !multiMode {
		for _, ip := range ips {
			ipStr := ip.String()
			if ipStr != currentHost && probeOK[ipStr] {
				newURL := "grpc://" + net.JoinHostPort(ipStr, port)
				g.logger.Info("endpoint %d health probe passed for %s (single-endpoint overlap)", endpointIndex, ipStr)
				g.endpointsMu.Lock()
				if endpointIndex >= 0 && endpointIndex < len(g.endpoints) && g.endpoints[endpointIndex] != nil {
					g.endpoints[endpointIndex].URL = newURL
				}
				g.endpointsMu.Unlock()
				return newURL
			}
		}
	}

	// Third pass: no healthy unique IPs. Pick any different IP not in use,
	// without probe (maybe health endpoint is down but gRPC works).
	for _, ip := range ips {
		ipStr := ip.String()
		if ipStr != currentHost && !inUse[ipStr] {
			newURL := "grpc://" + net.JoinHostPort(ipStr, port)
			g.logger.Warn("endpoint %d no healthy IPs found, falling back to %s without probe", endpointIndex, ipStr)
			g.endpointsMu.Lock()
			if endpointIndex >= 0 && endpointIndex < len(g.endpoints) && g.endpoints[endpointIndex] != nil {
				g.endpoints[endpointIndex].URL = newURL
			}
			g.endpointsMu.Unlock()
			return newURL
		}
	}

	// All resolved IPs are identical to current — no change
	return ""
}

// probeHealthEndpoint checks if a gravity endpoint is alive and ready by
// hitting its HTTP health check endpoint. Returns true if the endpoint
// responds with 200, false for any error, timeout, or non-200 status.
//
// Distinguishes dead nodes (timeout/connection refused → give up immediately)
// from starting nodes (503 → retry for up to 15s). This handles the race
// where hadron probes ions during their startup grace period.
func (g *GravityClient) probeHealthEndpoint(host, port string) bool {
	// Use injectable probe function if set (for tests)
	if g.healthProbe != nil {
		return g.healthProbe(host, port)
	}
	probeURL := "http://" + net.JoinHostPort(host, "80") + "/__health"

	// First probe with short timeout to classify the endpoint.
	status, err := g.doHealthProbe(probeURL)
	if err != nil {
		return false // timeout or connection refused → dead
	}
	if status == 200 {
		return true // healthy
	}
	if status != 503 {
		return false // unexpected status → don't retry
	}

	// 503 = alive but starting (e.g. "no routable hadrons"). Don't wait
	// long — the ion is reachable, just not fully ready. Hadron connecting
	// is what makes the ion healthy, so waiting creates a chicken-and-egg.
	// Use a short 3s probe to detect truly dead ions vs starting ones.
	g.logger.Debug("health probe %s returned 503 (starting) — brief readiness check", host)
	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			g.logger.Warn("health probe %s still 503 after 15s — giving up", host)
			return false
		case <-g.context.Done():
			return false
		case <-ticker.C:
			status, err := g.doHealthProbe(probeURL)
			if err != nil {
				return false // went from 503 to unreachable → dead
			}
			if status == 200 {
				g.logger.Info("health probe %s now ready (was 503)", host)
				return true
			}
			// Still 503, keep polling
		}
	}
}

// doHealthProbe performs a single HTTP health check with 3s timeout.
// Returns the HTTP status code and any error (timeout, connection refused, etc.)
func (g *GravityClient) doHealthProbe(probeURL string) (int, error) {
	ctx, cancel := context.WithTimeout(g.context, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", probeURL, nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	// Drain body so the HTTP transport can reuse the connection.
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode, nil
}

func (g *GravityClient) reconnectSingleEndpoint(endpointIndex int, endpointURL string) error {
	grpcURL, err := g.parseGRPCURL(endpointURL)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", endpointURL, err)
	}

	// Use the endpoint's TLSServerName if available (set during DNS resolution),
	// otherwise extract from URL.
	var hostname string
	g.endpointsMu.RLock()
	if endpointIndex >= 0 && endpointIndex < len(g.endpoints) && g.endpoints[endpointIndex] != nil {
		hostname = g.endpoints[endpointIndex].TLSServerName
	}
	g.endpointsMu.RUnlock()
	if hostname == "" {
		hostname, err = g.extractHostnameFromURL(endpointURL)
		if err != nil {
			return fmt.Errorf("failed to extract hostname from %q: %w", endpointURL, err)
		}
	}

	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if ok := pool.AppendCertsFromPEM([]byte(g.caCert)); !ok {
		return fmt.Errorf("failed to load embedded CA certificate")
	}

	if g.tlsCert == nil {
		return fmt.Errorf("failed to load TLS certificate, self-signed cert was nil")
	}
	cert := *g.tlsCert
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ServerName:   hostname,
		MinVersion:   tls.VersionTLS13,
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			if g.tlsCert != nil {
				return g.tlsCert, nil
			}
			return nil, fmt.Errorf("no client certificate available")
		},
		RootCAs: pool,
	}

	conn, err := grpc.NewClient(grpcURL,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithInitialWindowSize(1<<20),
		grpc.WithInitialConnWindowSize(4<<20),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", addr)
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to create gRPC client to %s: %w", grpcURL, err)
	}

	client := pb.NewGravitySessionServiceClient(conn)

	ctx, cancel := context.WithCancel(g.ctx)
	stream, err := client.EstablishSession(ctx, grpc.UseCompressor(grpcgzip.Name))
	if err != nil {
		cancel()
		_ = conn.Close()
		return fmt.Errorf("failed to establish control stream: %w", err)
	}

	g.mu.Lock()
	if endpointIndex < 0 || endpointIndex >= len(g.connections) {
		g.mu.Unlock()
		cancel()
		_ = conn.Close()
		return fmt.Errorf("endpoint index %d out of range", endpointIndex)
	}
	g.connections[endpointIndex] = conn
	g.sessionClients[endpointIndex] = client
	g.circuitBreakers[endpointIndex] = NewCircuitBreaker(DefaultCircuitBreakerConfig())
	g.connectionURLs[endpointIndex] = endpointURL
	g.mu.Unlock()

	// Publish the control stream so handleControlStream can receive the
	// hello response, but do NOT mark it healthy yet — that happens after
	// the handshake completes so sendSessionMessageAsync won't route
	// messages to a half-open endpoint.
	g.streamManager.controlMu.Lock()
	g.streamManager.controlStreams[endpointIndex] = stream
	g.streamManager.contexts[endpointIndex] = ctx
	g.streamManager.cancels[endpointIndex] = cancel
	g.streamManager.controlMu.Unlock()

	go g.handleControlStream(endpointIndex, stream)

	g.drainConnectionIDChan()
	if err := g.sendSessionHelloOnStream(endpointIndex, stream); err != nil {
		cancel()
		_ = conn.Close()
		return fmt.Errorf("failed to send session hello: %w", err)
	}

	select {
	case id := <-g.connectionIDChan:
		if id == "" {
			cancel()
			_ = conn.Close()
			return fmt.Errorf("session hello rejected by server")
		}
		g.logger.Debug("endpoint %d session hello accepted (machine ID: %s)", endpointIndex, id)
	case <-time.After(30 * time.Second):
		cancel()
		_ = conn.Close()
		return fmt.Errorf("timeout waiting for session hello response")
	case <-g.ctx.Done():
		cancel()
		_ = conn.Close()
		return g.ctx.Err()
	}

	// Handshake succeeded — now mark the connection healthy so
	// sendSessionMessageAsync and stream selectors can use it.
	g.streamManager.healthMu.Lock()
	if endpointIndex >= 0 && endpointIndex < len(g.streamManager.connectionHealth) {
		g.streamManager.connectionHealth[endpointIndex] = true
	}
	g.streamManager.healthMu.Unlock()

	g.fireOnEndpointReady(endpointIndex)

	streamsPerGravity := g.poolConfig.StreamsPerGravity
	if streamsPerGravity <= 0 {
		streamsPerGravity = DefaultStreamsPerGravity
	}
	tunnelOffset := endpointIndex * streamsPerGravity

	streamsToStart := make([]struct {
		idx      int
		stream   pb.GravitySessionService_StreamSessionPacketsClient
		streamID string
	}, 0, streamsPerGravity)

	// Build tunnel streams locally first so a partial failure doesn't
	// leave orphaned isHealthy=true entries in the shared slice.
	type pendingTunnel struct {
		idx      int
		info     *StreamInfo
		streamID string
		stream   pb.GravitySessionService_StreamSessionPacketsClient
	}
	pending := make([]pendingTunnel, 0, streamsPerGravity)

	for i := 0; i < streamsPerGravity; i++ {
		tunnelIdx := tunnelOffset + i
		if tunnelIdx < 0 || tunnelIdx >= len(g.streamManager.tunnelStreams) {
			continue
		}

		streamID := fmt.Sprintf("stream_%s", rand.Text())
		tctx := context.WithValue(g.ctx, machineIDKey, g.machineID)
		tctx = context.WithValue(tctx, streamIDKey, streamID)
		md := metadata.Pairs("machine-id", g.machineID, "stream-id", streamID)
		tctx = metadata.NewOutgoingContext(tctx, md)

		tstream, terr := client.StreamSessionPackets(tctx)
		if terr != nil {
			// This cancel may cause the in-flight handleControlStream goroutine to
			// observe a disconnection. That path is safe: reconnectEndpoint already
			// set endpointReconnecting[endpointIndex]=true, and
			// handleEndpointDisconnection uses a CAS guard to avoid re-entrant
			// reconnection for the same endpoint.
			cancel()
			_ = conn.Close()
			return fmt.Errorf("failed to create tunnel stream %d: %w", i, terr)
		}

		pending = append(pending, pendingTunnel{
			idx: tunnelIdx,
			info: &StreamInfo{
				stream:    tstream,
				connIndex: endpointIndex,
				streamID:  streamID,
				isHealthy: true,
				loadCount: 0,
				lastUsed:  time.Now(),
			},
			streamID: streamID,
			stream:   tstream,
		})
	}

	// All tunnel streams created successfully — commit into shared state.
	g.streamManager.tunnelMu.Lock()
	for _, p := range pending {
		g.streamManager.tunnelStreams[p.idx] = p.info
	}
	g.streamManager.tunnelMu.Unlock()

	g.streamManager.metricsMu.Lock()
	for _, p := range pending {
		g.streamManager.streamMetrics[p.streamID] = &StreamMetrics{}
	}
	g.streamManager.metricsMu.Unlock()

	streamsToStart = make([]struct {
		idx      int
		stream   pb.GravitySessionService_StreamSessionPacketsClient
		streamID string
	}, 0, len(pending))
	for _, p := range pending {
		streamsToStart = append(streamsToStart, struct {
			idx      int
			stream   pb.GravitySessionService_StreamSessionPacketsClient
			streamID string
		}{idx: p.idx, stream: p.stream, streamID: p.streamID})
	}

	for _, s := range streamsToStart {
		go g.handleTunnelStream(s.idx, s.stream, s.streamID)
	}

	g.endpointsMu.RLock()
	if endpointIndex >= 0 && endpointIndex < len(g.endpoints) && g.endpoints[endpointIndex] != nil {
		g.endpoints[endpointIndex].healthy.Store(true)
		g.endpoints[endpointIndex].lastHeartbeat.Store(time.Now().Unix())
	}
	g.endpointsMu.RUnlock()

	g.rebuildEndpointStreamIndices()
	g.refreshEndpointHealth()

	return nil
}

func (g *GravityClient) drainConnectionIDChan() {
	for {
		select {
		case <-g.connectionIDChan:
		default:
			return
		}
	}
}

// disconnectStreams closes all streams but keeps the client ready for reconnection
func (g *GravityClient) disconnectStreams() {
	g.logger.Debug("disconnectStreams called")
	// Cancel all stream contexts
	for _, cancel := range g.streamManager.cancels {
		if cancel != nil {
			cancel()
		}
	}

	// Close all connections
	for _, conn := range g.connections {
		if conn != nil {
			conn.Close()
		}
	}
}

// attemptReconnection attempts to reconnect to gravity servers with backoff.
// Each attempt is bounded by a timeout to prevent indefinite hanging (e.g. when
// Gravity is unreachable and gRPC blocks without a deadline). After exhausting
// the configured maximum attempts, ReconnectionFailedCallback is invoked —
// typically to crash the process so a supervisor (systemd) can restart it.
func (g *GravityClient) attemptReconnection(reason string) {
	g.logger.Debug("attemptReconnection called: %s", reason)
	defer func() {
		g.mu.Lock()
		g.reconnecting = false
		g.mu.Unlock()
	}()

	g.logger.Info("starting reconnection attempts due to: %s", reason)

	// Use exponential backoff for reconnection attempts.
	// Cap at 10s — ion restarts typically complete in 5-10s, and we
	// want hadron to reconnect promptly after a restart, not wait 30s.
	backoff := time.Second
	maxBackoff := 3 * time.Second
	attempts := 0

	maxAttempts := g.maxReconnectAttempts
	// Default: unlimited. Hadron must keep retrying indefinitely —
	// ion restarts, network disruptions, and cloud migrations are
	// normal events. The capped backoff (30s) prevents hammering.
	// Systemd handles truly broken processes.

	attemptTimeout := g.reconnectAttemptTimeout
	if attemptTimeout <= 0 {
		attemptTimeout = 2 * time.Minute // Default: 2 minutes per attempt
	}

	for !g.closing {
		attempts++
		if maxAttempts > 0 {
			g.logger.Info("reconnection attempt %d/%d (backoff: %v, timeout: %v)", attempts, maxAttempts, backoff, attemptTimeout)
		} else {
			g.logger.Info("reconnection attempt %d (backoff: %v, timeout: %v)", attempts, backoff, attemptTimeout)
		}

		// Each reconnection attempt has a timeout to prevent indefinite hanging.
		// Without this, Start() can block forever when Gravity is unreachable
		// because gRPC stream establishment has no inherent deadline.
		if err := g.reconnectWithTimeout(attemptTimeout); err != nil {
			if maxAttempts > 0 {
				g.logger.Error("reconnection attempt %d/%d failed: %v", attempts, maxAttempts, err)
			} else {
				g.logger.Error("reconnection attempt %d failed: %v", attempts, err)
			}

			// Check if we've exhausted all attempts (0 = unlimited)
			if maxAttempts > 0 && attempts >= maxAttempts {
				g.logger.Error("exhausted all %d reconnection attempts (last error: %v)", maxAttempts, err)
				if g.reconnectionFailedCallback != nil {
					g.reconnectionFailedCallback(attempts, err)
				}
				return
			}

			// Wait before next attempt with exponential backoff
			select {
			case <-time.After(backoff):
				// Increase backoff exponentially, capped at maxBackoff
				backoff = min(backoff*2, maxBackoff)
			case <-g.ctx.Done():
				g.logger.Info("reconnection cancelled due to context cancellation")
				return
			}
		} else {
			g.logger.Info("successfully reconnected after %d attempts", attempts)
			return
		}
	}

	g.logger.Info("reconnection stopped due to client shutdown")
}

// reconnectWithTimeout wraps reconnect() with a timeout to prevent indefinite
// hanging. When Gravity is unreachable, gRPC stream establishment can block
// forever because the client context has no deadline. This method ensures each
// attempt is bounded: if reconnect() does not complete within the timeout,
// in-progress connections are cleaned up and an error is returned so the retry
// loop can advance to the next attempt.
func (g *GravityClient) reconnectWithTimeout(timeout time.Duration) error {
	g.logger.Debug("reconnectWithTimeout called: %s", timeout)
	done := make(chan error, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				buf := make([]byte, 4096)
				n := runtime.Stack(buf, false)
				g.logger.Error("reconnection panicked: %v\nstack:\n%s", r, string(buf[:n]))
				done <- fmt.Errorf("reconnection panicked: %v", r)
			}
		}()
		done <- g.reconnect()
	}()

	select {
	case err := <-done:
		g.logger.Debug("reconnectWithTimeout is done")
		return err
	case <-time.After(timeout):
		g.logger.Warn("reconnection attempt timed out after %v, cleaning up stale connections", timeout)
		// reconnectWithTimeout is only used by attemptReconnection(), which is the
		// single-endpoint reconnect path (handleServerDisconnection). Multi-endpoint
		// reconnection uses reconnectEndpoint/reconnectSingleEndpoint and does not
		// call this function, so full cleanup here is scoped to single-endpoint mode.
		// Cancel in-progress stream contexts and close connections to unblock
		// the gRPC operations inside Start(). This causes EstablishSession,
		// StreamSessionPackets, etc. to return with errors, allowing the
		// background goroutine to exit.
		g.cleanup()
		// Wait for the goroutine to finish after cleanup
		select {
		case <-done:
			// Goroutine exited after cleanup — good
		case <-time.After(10 * time.Second):
			g.logger.Warn("reconnection goroutine did not exit promptly after cleanup")
		}
		return fmt.Errorf("reconnection attempt timed out after %v", timeout)
	case <-g.ctx.Done():
		return g.ctx.Err()
	}
}

// reconnect resets connection state and attempts to start a new connection
func (g *GravityClient) reconnect() error {
	g.logger.Debug("reconnect called")
	// Reset connection state without holding lock during Start()
	g.mu.Lock()
	g.ensureConnectionContextLocked()
	g.connected = false
	g.closing = false // Reset closing flag to allow reconnection
	g.sessionReady = make(chan struct{})

	// Clear connection IDs from previous connection
	g.connectionIDs = make([]string, 0, g.poolConfig.PoolSize)

	// Reset connections slice to avoid conflicts in Start()
	g.connections = nil
	g.connectionURLs = nil
	g.endpointStreamIndices = make(map[string][]int)

	// Reset stream manager state
	g.streamManager.tunnelMu.Lock()
	g.streamManager.tunnelStreams = nil
	g.streamManager.tunnelMu.Unlock()

	g.streamManager.controlMu.Lock()
	g.streamManager.controlStreams = nil
	g.streamManager.contexts = nil
	g.streamManager.cancels = nil
	g.streamManager.controlMu.Unlock()

	// Drain the connection ID channel to avoid stale data
	g.drainConnectionIDChan()

	// Clear hello acks from previous session so establishTunnelStreams
	// only sees acknowledgements from the new session.
	g.helloAckedStreams.Range(func(key, _ any) bool {
		g.helloAckedStreams.Delete(key)
		return true
	})
	g.mu.Unlock()
	if provisioningProvider, ok := g.provider.(provider.ProvisioningProvider); ok {
		// Log deployment synchronization intent
		deploymentCount := len(provisioningProvider.Resources())
		if deploymentCount > 0 {
			g.logger.Info("connection state reset, attempting to start new connection and synchronize %d existing deployments", deploymentCount)
		} else {
			g.logger.Info("connection state reset, attempting to start new connection")
		}
	}

	// Start new connection
	return g.Start()
}

// Helper functions

func (g *GravityClient) parseGRPCURL(inputURL string) (string, error) {
	// Handle grpc://host:port or host:port directly
	if len(inputURL) < 3 {
		return "", fmt.Errorf("invalid URL")
	}

	var host string
	if len(inputURL) >= 7 && inputURL[:7] == "grpc://" {
		host = inputURL[7:]
	} else {
		// Assume it's already host:port format
		host = inputURL
	}

	// Remove path if present
	if idx := strings.Index(host, "/"); idx != -1 {
		host = host[:idx]
	}

	u, err := url.Parse(inputURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}

	if u.Port() == "" {
		u.Host = u.Host + ":443"
	}

	// Resolve the hostname via /etc/hosts first, then fall back to DNS.
	// gRPC's built-in DNS resolver does not consult /etc/hosts, which causes
	// connection failures in environments where /etc/hosts has the correct
	// mapping but system DNS returns a different address (e.g., Lima VMs,
	// containers with custom host entries).
	hostname := u.Hostname()
	port := u.Port()
	if ip := resolveFromHostsFile(hostname); ip != "" {
		resolved := net.JoinHostPort(ip, port)
		g.logger.Debug("parsed gRPC URL: input=%s, resolved=%s (via /etc/hosts)", inputURL, resolved)
		return resolved, nil
	}

	g.logger.Debug("parsed gRPC URL: input=%s, host=%s", inputURL, u.Host)
	return u.Host, nil
}

// resolveFromHostsFile looks up a hostname in /etc/hosts (cached).
// Returns the first matching IP address, or empty string if not found.
func resolveFromHostsFile(hostname string) string {
	if ip := dns.ResolveFromHostsFile(hostname); ip != nil {
		return ip.String()
	}
	return ""
}

// extractHostnameFromURL extracts the hostname (without port) from a gRPC URL for TLS ServerName
func (g *GravityClient) extractHostnameFromURL(inputURL string) (string, error) {
	hostname, err := extractHostnameFromGravityURL(inputURL, g.defaultServerName)
	if err != nil {
		return "", err
	}
	g.logger.Debug("extracted hostname for TLS ServerName: %s", hostname)
	return hostname, nil
}

func (g *GravityClient) cleanup() {
	g.logger.Debug("cleanup called")

	// Cancel all stream contexts
	for _, cancel := range g.streamManager.cancels {
		if cancel != nil {
			cancel()
		}
	}

	// Close all connections
	for _, conn := range g.connections {
		if conn != nil {
			conn.Close()
		}
	}
}

func generateMessageID() string {
	uid, err := uuid.NewV7()
	if err != nil {
		return uuid.New().String()
	}
	return uid.String()
}

// sendSessionMessageAsync sends a session message without waiting for response (async)
func (g *GravityClient) sendSessionMessageAsync(msg *pb.SessionMessage) error {
	stream, streamIndex := g.pickControlStream()
	if stream == nil || streamIndex < 0 {
		return fmt.Errorf("no control streams available")
	}
	return g.sendOnStream(stream, streamIndex, msg)
}

// broadcastSessionMessage sends a message to ALL healthy control streams.
// Used for deployment/resource registration so every connected ion gets the
// VIP routing. Returns an error if ANY endpoint fails — partial VIP
// registration can cause routing inconsistencies.
func (g *GravityClient) broadcastSessionMessage(msg *pb.SessionMessage) error {
	streams := g.allHealthyControlStreams()
	if len(streams) == 0 {
		return fmt.Errorf("no control streams available")
	}

	var errs []error
	sent := 0
	for _, s := range streams {
		if err := g.sendOnStream(s.stream, s.index, msg); err != nil {
			g.logger.Warn("broadcastSessionMessage: failed to send to endpoint %d: %v", s.index, err)
			errs = append(errs, fmt.Errorf("endpoint %d: %w", s.index, err))
		} else {
			sent++
		}
	}
	if sent == 0 {
		return fmt.Errorf("failed to send to any endpoint: %w", errors.Join(errs...))
	}
	if len(errs) > 0 {
		return fmt.Errorf("partial broadcast: delivered to %d/%d endpoints: %w", sent, len(streams), errors.Join(errs...))
	}
	return nil
}

type indexedStream struct {
	stream pb.GravitySessionService_EstablishSessionClient
	index  int
}

// pickControlStream returns the first healthy control stream.
func (g *GravityClient) pickControlStream() (pb.GravitySessionService_EstablishSessionClient, int) {
	g.streamManager.controlMu.RLock()
	g.streamManager.healthMu.RLock()
	defer g.streamManager.healthMu.RUnlock()
	defer g.streamManager.controlMu.RUnlock()

	for i, s := range g.streamManager.controlStreams {
		if s == nil {
			continue
		}
		if i < len(g.streamManager.connectionHealth) && !g.streamManager.connectionHealth[i] {
			continue
		}
		return s, i
	}
	// Fallback: pick any non-nil stream
	for i, s := range g.streamManager.controlStreams {
		if s != nil {
			return s, i
		}
	}
	return nil, -1
}

// allHealthyControlStreams returns all healthy control streams.
func (g *GravityClient) allHealthyControlStreams() []indexedStream {
	g.streamManager.controlMu.RLock()
	g.streamManager.healthMu.RLock()
	defer g.streamManager.healthMu.RUnlock()
	defer g.streamManager.controlMu.RUnlock()

	var streams []indexedStream
	for i, s := range g.streamManager.controlStreams {
		if s == nil {
			continue
		}
		if i < len(g.streamManager.connectionHealth) && !g.streamManager.connectionHealth[i] {
			continue
		}
		streams = append(streams, indexedStream{stream: s, index: i})
	}
	return streams
}

func (g *GravityClient) sendOnStream(stream pb.GravitySessionService_EstablishSessionClient, streamIndex int, msg *pb.SessionMessage) error {
	if streamIndex >= len(g.circuitBreakers) {
		return fmt.Errorf("circuit breaker index %d out of range (len=%d)", streamIndex, len(g.circuitBreakers))
	}
	if streamIndex >= len(g.streamManager.controlSendMu) {
		return fmt.Errorf("controlSendMu index %d out of range (len=%d)", streamIndex, len(g.streamManager.controlSendMu))
	}

	circuitBreaker := g.circuitBreakers[streamIndex]

	ctx, cancel := context.WithTimeout(context.WithoutCancel(g.ctx), 10*time.Second)
	defer cancel()

	sendMu := &g.streamManager.controlSendMu[streamIndex]
	return RetryWithCircuitBreaker(ctx, g.retryConfig, circuitBreaker, func() error {
		sendMu.Lock()
		err := stream.Send(msg)
		sendMu.Unlock()
		return err
	})
}

// SendRouteDeploymentRequest sends a route deployment request and waits for response (sync)
func (g *GravityClient) SendRouteDeploymentRequest(deploymentID, virtualIP, ownerID string, timeout time.Duration) (*pb.RouteDeploymentResponse, error) {
	// Read sessionReady under lock to avoid racing with reconnection
	// which replaces the channel.
	g.mu.RLock()
	ready := g.sessionReady
	g.mu.RUnlock()

	select {
	case <-ready:
		// Session is ready
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for session ready before route deployment request")
	case <-g.ctx.Done():
		return nil, fmt.Errorf("context cancelled while waiting for session ready")
	}

	msgID := generateMessageID()

	// Create response channel
	responseChan := make(chan routeDeploymentResult, 1)

	// Register pending request
	g.pendingRouteDeploymentMu.Lock()
	g.pendingRouteDeployment[msgID] = responseChan
	g.pendingRouteDeploymentMu.Unlock()

	// Cleanup pending request on exit
	defer func() {
		g.pendingRouteDeploymentMu.Lock()
		delete(g.pendingRouteDeployment, msgID)
		g.pendingRouteDeploymentMu.Unlock()
	}()

	// Create and send the request
	msg := &pb.SessionMessage{
		Id: msgID,
		MessageType: &pb.SessionMessage_RouteDeployment{
			RouteDeployment: &pb.RouteDeploymentRequest{
				DeploymentId: deploymentID,
				VirtualIp:    virtualIP,
				OwnerId:      ownerID,
			},
		},
	}

	// Broadcast to ALL connected ions so every ion with a tunnel to this
	// hadron gets the VIP route. Without this, only one ion registers the
	// deployment VIP, and requests hitting other ions get 502.
	if err := g.broadcastSessionMessage(msg); err != nil {
		return nil, fmt.Errorf("failed to send route deployment request: %w", err)
	}

	// Wait for response with timeout
	select {
	case result := <-responseChan:
		if result.Error != "" {
			return nil, fmt.Errorf("route deployment request failed: %s", result.Error)
		}
		return result.Response, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for route deployment response")
	case <-g.ctx.Done():
		return nil, fmt.Errorf("context cancelled while waiting for route deployment response")
	}
}

// SendRouteSandboxRequest sends a route sandbox request and waits for response (sync)
func (g *GravityClient) SendRouteSandboxRequest(sandboxID, virtualIP, ownerID string, timeout time.Duration) (*pb.RouteSandboxResponse, error) {
	// Read sessionReady under lock to avoid racing with reconnection
	// which replaces the channel.
	g.mu.RLock()
	ready := g.sessionReady
	g.mu.RUnlock()

	select {
	case <-ready:
		// Session is ready
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for session ready before route sandbox request")
	case <-g.ctx.Done():
		return nil, fmt.Errorf("context cancelled while waiting for session ready")
	}

	msgID := generateMessageID()

	// Create response channel
	responseChan := make(chan routeSandboxResult, 1)

	// Register pending request
	g.pendingRouteSandboxMu.Lock()
	g.pendingRouteSandbox[msgID] = responseChan
	g.pendingRouteSandboxMu.Unlock()

	// Cleanup pending request on exit
	defer func() {
		g.pendingRouteSandboxMu.Lock()
		delete(g.pendingRouteSandbox, msgID)
		g.pendingRouteSandboxMu.Unlock()
	}()

	// Create and send the request
	msg := &pb.SessionMessage{
		Id: msgID,
		MessageType: &pb.SessionMessage_RouteSandbox{
			RouteSandbox: &pb.RouteSandboxRequest{
				SandboxId: sandboxID,
				VirtualIp: virtualIP,
				OwnerId:   ownerID,
			},
		},
	}

	// Broadcast to ALL connected ions so every ion with a tunnel to this
	// hadron gets the sandbox VIP route.
	g.logger.Info("SendRouteSandboxRequest: broadcasting sandbox %s (vip=%s) to all endpoints", sandboxID, virtualIP)
	if err := g.broadcastSessionMessage(msg); err != nil {
		return nil, fmt.Errorf("failed to send route sandbox request: %w", err)
	}

	// Wait for first response with timeout — all ions process independently.
	select {
	case result := <-responseChan:
		if result.Error != "" {
			return nil, fmt.Errorf("route sandbox request failed: %s", result.Error)
		}
		return result.Response, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for route sandbox response")
	case <-g.ctx.Done():
		return nil, fmt.Errorf("context cancelled while waiting for route sandbox response")
	}
}

// selectOptimalTunnelStream finds the healthiest tunnel stream with lowest load
func (sm *StreamManager) selectOptimalTunnelStream() *StreamInfo {
	sm.tunnelMu.Lock()
	defer sm.tunnelMu.Unlock()

	// Find healthy stream with lowest load
	var bestStream *StreamInfo
	var minLoad int64 = -1

	for _, stream := range sm.tunnelStreams {
		if stream == nil {
			continue
		}
		if !stream.isHealthy {
			continue
		}

		if minLoad == -1 || stream.loadCount < minLoad {
			minLoad = stream.loadCount
			bestStream = stream
		}
	}

	if bestStream != nil {
		bestStream.loadCount++
		bestStream.lastUsed = time.Now()
	}

	return bestStream
}

// releaseStream decrements the load count for the given stream
func (sm *StreamManager) releaseStream(stream *StreamInfo) {
	if stream == nil {
		return
	}

	sm.tunnelMu.Lock()
	defer sm.tunnelMu.Unlock()

	if stream.loadCount > 0 {
		stream.loadCount--
	}
}

// Provider.Server interface implementations

// Unprovision sends an unprovision request to the gravity server.
// ownerID identifies the specific provision instance; gravity only removes the resource
// if ownerID matches the current owner (empty ownerID = unconditional remove for backward compat).
func (g *GravityClient) Unprovision(deploymentID string, ownerID string) error {
	g.logger.Info("Unprovision: broadcasting removal of deployment %s (ownerID=%s) to all endpoints", deploymentID, ownerID)

	req := &pb.UnprovisionRequest{
		DeploymentId: deploymentID,
		OwnerId:      ownerID,
	}

	msgID := generateMessageID()
	msg := &pb.SessionMessage{
		Id: msgID,
		MessageType: &pb.SessionMessage_Unprovision{
			Unprovision: req,
		},
	}

	// Broadcast to ALL connected ions so every ion removes the VIP route.
	if err := g.broadcastSessionMessage(msg); err != nil {
		g.logger.Error("Unprovision: broadcast failed for deployment %s: %v", deploymentID, err)
		return err
	}
	g.logger.Info("Unprovision: broadcast succeeded for deployment %s", deploymentID)
	return nil
}

// SendResourceSync sends the full set of active resources to gravity.
// Gravity compares this against its cached state for the machine and
// unprovisions any stale entries that hadron no longer has. This replaces
// per-resource RouteDeploymentRequest calls during reconnection, providing
// atomic full-state sync that prevents stale VIP entries in the gossip mesh.
func (g *GravityClient) SendResourceSync(deployments []ResourceSyncItem, sandboxes []ResourceSyncItem, timeout time.Duration) (*pb.ResourceSyncResponse, error) {
	g.mu.RLock()
	ready := g.sessionReady
	g.mu.RUnlock()

	select {
	case <-ready:
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for session ready before resource sync")
	case <-g.ctx.Done():
		return nil, fmt.Errorf("context cancelled while waiting for session ready")
	}

	pbDeployments := make([]*pb.ResourceSyncEntry, len(deployments))
	for i, d := range deployments {
		pbDeployments[i] = &pb.ResourceSyncEntry{
			Id:        d.ID,
			VirtualIp: d.VirtualIP,
			OwnerId:   d.OwnerID,
		}
	}
	pbSandboxes := make([]*pb.ResourceSyncEntry, len(sandboxes))
	for i, s := range sandboxes {
		pbSandboxes[i] = &pb.ResourceSyncEntry{
			Id:        s.ID,
			VirtualIp: s.VirtualIP,
			OwnerId:   s.OwnerID,
		}
	}

	msgID := generateMessageID()
	msg := &pb.SessionMessage{
		Id: msgID,
		MessageType: &pb.SessionMessage_ResourceSync{
			ResourceSync: &pb.ResourceSyncRequest{
				Deployments: pbDeployments,
				Sandboxes:   pbSandboxes,
			},
		},
	}

	// Broadcast to ALL connected ions.
	if err := g.broadcastSessionMessage(msg); err != nil {
		return nil, fmt.Errorf("failed to send resource sync: %w", err)
	}

	g.logger.Info("SendResourceSync: broadcast %d deployments + %d sandboxes", len(deployments), len(sandboxes))
	return &pb.ResourceSyncResponse{Success: true, Added: int32(len(deployments) + len(sandboxes))}, nil
}

// ResourceSyncItem represents a single resource in a sync request.
type ResourceSyncItem struct {
	ID        string
	VirtualIP string
	OwnerID   string
}

// SendEvacuateRequest sends a request to evacuate sandboxes on this machine.
func (g *GravityClient) SendEvacuateRequest(machineID, reason string, sandboxes []*pb.SandboxEvacInfo) error {
	msg := &pb.SessionMessage{
		Id: generateMessageID(),
		MessageType: &pb.SessionMessage_EvacuateRequest{
			EvacuateRequest: &pb.EvacuateRequest{
				MachineId: machineID,
				Reason:    reason,
				Sandboxes: sandboxes,
			},
		},
	}

	return g.sendSessionMessageAsync(msg)
}

// SendCheckpointURLRequest sends a checkpoint URL request and waits for response (sync).
// Used by suspend/resume operations to get presigned S3 URLs from Gravity.
func (g *GravityClient) SendCheckpointURLRequest(sandboxID string, operation pb.CheckpointURLOperation, checkpointKey string, orgID string, timeout time.Duration) (*pb.CheckpointURLResponse, error) {
	// Read sessionReady under lock to avoid racing with reconnection
	g.mu.RLock()
	ready := g.sessionReady
	g.mu.RUnlock()

	select {
	case <-ready:
		// Session is ready
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for session ready before checkpoint URL request")
	case <-g.ctx.Done():
		return nil, fmt.Errorf("context cancelled while waiting for session ready")
	}

	msgID := generateMessageID()

	// Create response channel
	responseChan := make(chan checkpointURLResult, 1)

	// Register pending request
	g.pendingCheckpointURLMu.Lock()
	g.pendingCheckpointURL[msgID] = responseChan
	g.pendingCheckpointURLMu.Unlock()

	// Cleanup pending request on exit
	defer func() {
		g.pendingCheckpointURLMu.Lock()
		delete(g.pendingCheckpointURL, msgID)
		g.pendingCheckpointURLMu.Unlock()
	}()

	// Create and send the request
	msg := &pb.SessionMessage{
		Id: msgID,
		MessageType: &pb.SessionMessage_CheckpointUrlRequest{
			CheckpointUrlRequest: &pb.CheckpointURLRequest{
				SandboxId:     sandboxID,
				Operation:     operation,
				CheckpointKey: checkpointKey,
				OrgId:         orgID,
			},
		},
	}

	if err := g.sendSessionMessageAsync(msg); err != nil {
		return nil, fmt.Errorf("failed to send checkpoint URL request: %w", err)
	}

	// Wait for response with timeout
	select {
	case result := <-responseChan:
		if result.Error != "" {
			return nil, fmt.Errorf("checkpoint URL request failed: %s", result.Error)
		}
		if !result.Response.Success {
			return nil, fmt.Errorf("checkpoint URL request failed: %s", result.Response.Error)
		}
		return result.Response, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for checkpoint URL response")
	case <-g.ctx.Done():
		return nil, fmt.Errorf("context cancelled while waiting for checkpoint URL response")
	}
}

// Pause sends a pause event to the gravity server
func (g *GravityClient) Pause(reason string) error {
	msg := &pb.SessionMessage{
		Id: generateMessageID(),
		MessageType: &pb.SessionMessage_Pause{
			Pause: &pb.PauseRequest{Reason: reason},
		},
	}

	return g.sendSessionMessageAsync(msg)
}

// Resume sends a resume event to the gravity server
func (g *GravityClient) Resume(reason string) error {
	msg := &pb.SessionMessage{
		Id: generateMessageID(),
		MessageType: &pb.SessionMessage_Resume{
			Resume: &pb.ResumeRequest{
				Reason: reason,
			},
		},
	}

	return g.sendSessionMessageAsync(msg)
}

// WritePacket sends a tunnel packet via gRPC tunnel stream using load balancing
func (g *GravityClient) WritePacket(payload []byte) error {
	if g.tracePackets {
		g.tracePacketLogger.Debug("writePacket called with %d bytes", len(payload))
	}
	g.mu.RLock()
	if !g.connected || g.closing {
		g.mu.RUnlock()
		if g.logger != nil {
			g.logger.Error("writePacket failed: connection closed or closing")
		}
		return ErrConnectionClosed
	}
	g.mu.RUnlock()

	g.outboundReceived.Add(1)

	// Detect TCP SYN-ACK in outbound packet — confirms container responded and
	// hadron is forwarding the SYN-ACK through the tunnel back to ion.
	// Intentionally chatty — will dial back once latency issue is diagnosed.
	var isSynAck bool
	if len(payload) >= 54 && payload[6] == 6 { // IPv6 Next Header == TCP
		flags := payload[53]
		if flags&0x12 == 0x12 { // SYN+ACK
			isSynAck = true
			srcIP := make(net.IP, 16)
			copy(srcIP, payload[8:24])
			dstIP := make(net.IP, 16)
			copy(dstIP, payload[24:40])
			srcPort := binary.BigEndian.Uint16(payload[40:42])
			dstPort := binary.BigEndian.Uint16(payload[42:44])
			g.logger.Trace("gravity.synack.sending src=%s:%d dst=%s:%d", srcIP, srcPort, dstIP, dstPort)
		}
	}

	// Multi-endpoint routing: sticky flow binding to selected endpoint.
	if stream, err := g.selectStreamForPacket(payload); err != nil {
		g.logger.Error("writePacket failed to select stream: %v", err)
		// Do NOT trigger reconnection here. selectStreamForPacket already
		// handles per-endpoint reconnection internally: when all streams for
		// an endpoint are unhealthy, it marks the endpoint unhealthy and calls
		// triggerEndpointReconnectByURL. The tunnel liveness monitor (AGNT
		// keepalive) provides the authoritative signal for dead endpoints.
		// Reacting to individual packet failures caused false-positive
		// cascading reconnection that killed healthy endpoints.
		return err
	} else {
		if g.tracePackets {
			g.tracePacketLogger.Debug("writePacket selected stream: %s", stream.streamID)
		}

		// Ensure load count is decremented in all exit paths
		defer g.streamManager.releaseStream(stream)

		tunnelPacket := &pb.TunnelPacket{
			Data:     payload,
			StreamId: stream.streamID,
		}
		if isSynAck {
			tunnelPacket.EnqueuedAtUs = time.Now().UnixMicro()
		}

		sendStart := time.Now()
		stream.sendMu.Lock()
		err = stream.stream.Send(tunnelPacket)
		stream.sendMu.Unlock()
		sendLatency := time.Since(sendStart)
		if err != nil {
			// Mark stream unhealthy and record per-stream error metrics,
			// mirroring the error handling in sendTunnelPacket.
			g.streamManager.tunnelMu.Lock()
			stream.isHealthy = false
			g.streamManager.tunnelMu.Unlock()
			g.outboundErrors.Add(1)
			now := time.Now()
			g.streamManager.metricsMu.Lock()
			if metrics := g.streamManager.streamMetrics[stream.streamID]; metrics != nil {
				metrics.ErrorCount++
				metrics.LastError = now
			}
			g.streamManager.metricsMu.Unlock()

			if errors.Is(err, io.EOF) {
				g.logger.Debug("gravity server closed, exiting")
				return nil
			}
			g.logger.Error("writePacket stream.Send failed: %v", err)
			// Do NOT trigger reconnection from Send failures. A single bad
			// packet (TCP retransmit, transient buffer pressure) must not
			// flap the connection. The stream is already marked unhealthy
			// above, so selectStreamForPacket will route future packets to
			// other streams/endpoints. The tunnel liveness monitor (AGNT
			// keepalive echo) is the authoritative signal for dead endpoints
			// and will trigger per-endpoint reconnection when appropriate.
		} else {
			if g.tracePackets {
				g.tracePacketLogger.Debug("writePacket stream.Send succeeded for stream %s", stream.streamID)
			}
			g.outboundSent.Add(1)
			g.outboundBytes.Add(uint64(len(payload)))
			g.streamManager.metricsMu.Lock()
			if metrics := g.streamManager.streamMetrics[stream.streamID]; metrics != nil {
				metrics.PacketsSent++
				metrics.BytesSent += int64(len(payload))
				metrics.LastSendUs = time.Now().UnixMicro()
				metrics.LastLatency = sendLatency
			}
			g.streamManager.metricsMu.Unlock()
		}

		return err
	}
}

func (g *GravityClient) selectStreamForPacket(payload []byte) (*StreamInfo, error) {
	g.endpointsMu.RLock()
	selector := g.selector
	endpoints := make([]*GravityEndpoint, len(g.endpoints))
	copy(endpoints, g.endpoints)
	g.endpointsMu.RUnlock()

	if selector != nil && len(endpoints) > 1 {
		// Try up to len(endpoints) times to find one with healthy tunnels.
		for attempt := 0; attempt < len(endpoints); attempt++ {
			endpoint := selector.Select(payload, endpoints)
			if endpoint == nil {
				// Log endpoint health summary for debugging selector failures.
				var healthSummary []string
				for i, ep := range endpoints {
					if ep != nil {
						healthSummary = append(healthSummary, fmt.Sprintf("[%d]%s(healthy=%v)", i, ep.URL, ep.healthy.Load()))
					} else {
						healthSummary = append(healthSummary, fmt.Sprintf("[%d]nil", i))
					}
				}
				g.logger.Debug("selectStream: no endpoint selected, health: %s", strings.Join(healthSummary, ", "))
				return nil, fmt.Errorf("no healthy gravity endpoints")
			}
			stream, err := g.selectStreamForEndpoint(payload, endpoint.URL)
			if err == nil {
				return stream, nil
			}
			// This endpoint has no healthy tunnels. Mark it unhealthy
			// so the selector skips it on the next iteration, and
			// trigger reconnection in the background.
			endpoint.healthy.Store(false)
			g.logger.Warn("endpoint %s has no healthy tunnels, marking unhealthy and triggering reconnect", endpoint.URL)
			g.triggerEndpointReconnectByURL(endpoint.URL)
			g.wakePeerDiscovery()
		}
		return nil, fmt.Errorf("no healthy tunnel streams on any endpoint")
	}

	// Backward-compatible path: existing global stream selection.
	g.streamManager.tunnelMu.Lock()
	defer g.streamManager.tunnelMu.Unlock()

	if len(g.streamManager.tunnelStreams) == 0 {
		return nil, fmt.Errorf("no healthy tunnel streams available")
	}

	streamIndex, err := g.selectOptimalStream(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to select stream: %w", err)
	}

	stream := g.streamManager.tunnelStreams[streamIndex]
	if stream == nil || !stream.isHealthy {
		streamIndex, err = g.selectHealthyStream()
		if err != nil {
			return nil, fmt.Errorf("no healthy tunnel streams available")
		}
		stream = g.streamManager.tunnelStreams[streamIndex]
	}
	if stream == nil || !stream.isHealthy {
		return nil, fmt.Errorf("no healthy tunnel streams available")
	}

	stream.loadCount++
	stream.lastUsed = time.Now()
	return stream, nil
}

func (g *GravityClient) selectStreamForEndpoint(payload []byte, endpointURL string) (*StreamInfo, error) {
	g.mu.RLock()
	candidateIndexes := append([]int(nil), g.endpointStreamIndices[endpointURL]...)
	g.mu.RUnlock()
	if len(candidateIndexes) == 0 {
		return nil, fmt.Errorf("no tunnel streams available for endpoint %s", endpointURL)
	}

	g.streamManager.tunnelMu.Lock()
	defer g.streamManager.tunnelMu.Unlock()

	selectedIndex := -1
	switch g.streamManager.allocationStrategy {
	case HashBased:
		hash := simpleHashBytes(payload)
		for i := 0; i < len(candidateIndexes); i++ {
			idx := candidateIndexes[(hash+i)%len(candidateIndexes)]
			if idx >= 0 && idx < len(g.streamManager.tunnelStreams) && g.streamManager.tunnelStreams[idx] != nil && g.streamManager.tunnelStreams[idx].isHealthy {
				selectedIndex = idx
				break
			}
		}
	case LeastConnections:
		minLoad := int64(^uint64(0) >> 1)
		for _, idx := range candidateIndexes {
			if idx < 0 || idx >= len(g.streamManager.tunnelStreams) {
				continue
			}
			stream := g.streamManager.tunnelStreams[idx]
			if stream == nil || !stream.isHealthy {
				continue
			}
			if stream.loadCount < minLoad {
				minLoad = stream.loadCount
				selectedIndex = idx
			}
		}
	default:
		start := g.streamManager.nextTunnelIndex
		g.streamManager.nextTunnelIndex++
		for i := 0; i < len(candidateIndexes); i++ {
			idx := candidateIndexes[(start+i)%len(candidateIndexes)]
			if idx >= 0 && idx < len(g.streamManager.tunnelStreams) && g.streamManager.tunnelStreams[idx] != nil && g.streamManager.tunnelStreams[idx].isHealthy {
				selectedIndex = idx
				break
			}
		}
	}

	if selectedIndex == -1 {
		return nil, fmt.Errorf("no healthy tunnel streams available for endpoint %s", endpointURL)
	}

	stream := g.streamManager.tunnelStreams[selectedIndex]
	stream.loadCount++
	stream.lastUsed = time.Now()
	return stream, nil
}

// Background packet handlers

func (g *GravityClient) handleInboundPackets() {
	connCtx := g.currentConnectionContext()
	for {
		select {
		case <-connCtx.Done():
			g.logger.Debug("handleInboundPackets is done and no longer reading")
			return
		case packet := <-g.inboundPackets:
			// Forward to provider for local processing
			pktData := packet.Buffer[:packet.Length]
			if debugTunnelPackets && len(pktData) >= 40 {
				srcIP := net.IP(pktData[8:24])
				dstIP := net.IP(pktData[24:40])
				g.logger.Info("[inbound-deliver] %d bytes SRC=%s DST=%s → ProcessInPacket", len(pktData), srcIP, dstIP)
			}
			g.provider.ProcessInPacket(pktData)
			g.inboundDelivered.Add(1)
			g.returnBuffer(packet)
		}
	}
}

func (g *GravityClient) handleOutboundPackets() {
	connCtx := g.currentConnectionContext()
	for {
		select {
		case <-connCtx.Done():
			g.logger.Debug("handleOutboundPackets is done and no longer reading")
			return
		case data := <-g.outboundPackets:
			// Send packet through tunnel stream (round-robin selection)
			err := g.sendTunnelPacket(data)
			if err != nil {
				g.logger.Error("error sending outbound packet: %v", err)
			}
		}
	}
}

func (g *GravityClient) sendTunnelPacket(data []byte) error {
	streamInfo, err := g.selectStreamForPacket(data)
	if err != nil {
		return fmt.Errorf("failed to select stream: %w", err)
	}

	// Ensure load count is decremented in all exit paths
	defer g.streamManager.releaseStream(streamInfo)

	packet := &pb.TunnelPacket{
		Data:     data,
		StreamId: streamInfo.streamID,
	}

	// Send packet with retry logic and circuit breaker
	connectionIndex := streamInfo.connIndex
	if connectionIndex < 0 || connectionIndex >= len(g.circuitBreakers) {
		return fmt.Errorf("circuit breaker index %d out of range (len=%d)", connectionIndex, len(g.circuitBreakers))
	}
	circuitBreaker := g.circuitBreakers[connectionIndex]

	sendStart := time.Now()

	err = RetryWithCircuitBreaker(context.WithoutCancel(g.ctx), g.retryConfig, circuitBreaker, func() error {
		streamInfo.sendMu.Lock()
		sendErr := streamInfo.stream.Send(packet)
		streamInfo.sendMu.Unlock()
		return sendErr
	})

	sendLatency := time.Since(sendStart)

	if err != nil {
		// Mark stream as unhealthy on error
		g.streamManager.tunnelMu.Lock()
		streamInfo.isHealthy = false
		g.streamManager.tunnelMu.Unlock()

		g.streamManager.metricsMu.Lock()
		if metrics := g.streamManager.streamMetrics[streamInfo.streamID]; metrics != nil {
			metrics.ErrorCount++
			metrics.LastError = time.Now()
		}
		g.streamManager.metricsMu.Unlock()

		g.outboundErrors.Add(1)
		return fmt.Errorf("failed to send packet after retries: %w", err)
	}

	// Record successful send metrics
	g.outboundSent.Add(1)
	g.outboundBytes.Add(uint64(len(data)))
	g.streamManager.metricsMu.Lock()
	if metrics := g.streamManager.streamMetrics[streamInfo.streamID]; metrics != nil {
		metrics.PacketsSent++
		metrics.LastLatency = sendLatency
		metrics.LastSendUs = time.Now().UnixMicro()
		metrics.BytesSent += int64(len(data))
	}
	g.streamManager.metricsMu.Unlock()

	return nil
}

// selectOptimalStream chooses the best stream based on the configured allocation strategy
func (g *GravityClient) selectOptimalStream(data []byte) (int, error) {
	switch g.streamManager.allocationStrategy {
	case RoundRobin:
		return g.selectRoundRobinStream()
	case HashBased:
		return g.selectHashBasedStream(data)
	case LeastConnections:
		return g.selectLeastConnectionsStream()
	case WeightedRoundRobin:
		return g.selectWeightedRoundRobinStream()
	default:
		return g.selectRoundRobinStream()
	}
}

// selectRoundRobinStream implements simple round-robin selection
func (g *GravityClient) selectRoundRobinStream() (int, error) {
	if len(g.streamManager.tunnelStreams) == 0 {
		return 0, fmt.Errorf("no streams available")
	}
	streamIndex := g.streamManager.nextTunnelIndex % len(g.streamManager.tunnelStreams)
	g.streamManager.nextTunnelIndex++
	return streamIndex, nil
}

// selectHashBasedStream uses consistent hashing for packet distribution
func (g *GravityClient) selectHashBasedStream(data []byte) (int, error) {
	if len(g.streamManager.tunnelStreams) == 0 {
		return 0, fmt.Errorf("no streams available")
	}
	// Use simple hash of packet data for consistent routing
	hash := simpleHashBytes(data)
	streamIndex := hash % len(g.streamManager.tunnelStreams)
	return streamIndex, nil
}

// selectLeastConnectionsStream chooses the stream with the lowest current load
func (g *GravityClient) selectLeastConnectionsStream() (int, error) {
	minLoad := int64(^uint64(0) >> 1) // Max int64
	selectedIndex := -1
	var selectedStream *StreamInfo

	for i, streamInfo := range g.streamManager.tunnelStreams {
		if streamInfo == nil {
			continue
		}
		if !streamInfo.isHealthy {
			continue
		}
		if streamInfo.loadCount < minLoad || (streamInfo.loadCount == minLoad && selectedStream != nil && streamInfo.lastUsed.Before(selectedStream.lastUsed)) {
			minLoad = streamInfo.loadCount
			selectedIndex = i
			selectedStream = streamInfo
		}
	}

	if selectedStream == nil || selectedIndex < 0 {
		return 0, fmt.Errorf("no healthy streams available")
	}

	return selectedIndex, nil
}

// selectWeightedRoundRobinStream implements weighted round-robin based on connection health
func (g *GravityClient) selectWeightedRoundRobinStream() (int, error) {
	// First, try to find streams from healthy connections
	g.streamManager.healthMu.RLock()
	defer g.streamManager.healthMu.RUnlock()

	healthyStreams := make([]int, 0)
	for i, streamInfo := range g.streamManager.tunnelStreams {
		if streamInfo == nil {
			continue
		}
		if streamInfo.isHealthy && streamInfo.connIndex >= 0 && streamInfo.connIndex < len(g.streamManager.connectionHealth) && g.streamManager.connectionHealth[streamInfo.connIndex] {
			healthyStreams = append(healthyStreams, i)
		}
	}

	if len(healthyStreams) == 0 {
		// Fall back to any healthy stream
		return g.selectHealthyStream()
	}

	// Round-robin among healthy streams
	selectedIndex := g.streamManager.nextTunnelIndex % len(healthyStreams)
	g.streamManager.nextTunnelIndex++
	return healthyStreams[selectedIndex], nil
}

// selectHealthyStream finds any available healthy stream
func (g *GravityClient) selectHealthyStream() (int, error) {
	for i, streamInfo := range g.streamManager.tunnelStreams {
		if streamInfo != nil && streamInfo.isHealthy {
			return i, nil
		}
	}
	return 0, fmt.Errorf("no healthy streams available")
}

// simpleHashBytes computes a simple hash of byte data for consistent routing
func simpleHashBytes(data []byte) int {
	hash := 0
	for i, b := range data {
		if i >= 40 { // Include IPv6 src+dst addresses (bytes 8..39)
			break
		}
		hash = hash*31 + int(b)
	}
	if hash < 0 {
		hash = -hash
	}
	return hash
}

// monitorConnectionHealth periodically checks connection and stream health
func (g *GravityClient) monitorConnectionHealth() {
	connCtx := g.currentConnectionContext()
	interval := g.poolConfig.HealthCheckInterval
	if interval <= 0 {
		g.logger.Warn("invalid health check interval %v, skipping health monitor", interval)
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-connCtx.Done():
			g.logger.Debug("monitorConnectionHealth is no and no longer processing")
			return
		case <-ticker.C:
			g.performHealthCheck()
		}
	}
}

// performHealthCheck checks the health of all connections and streams
func (g *GravityClient) performHealthCheck() {
	g.streamManager.tunnelMu.Lock()
	defer g.streamManager.tunnelMu.Unlock()

	g.streamManager.healthMu.Lock()
	defer g.streamManager.healthMu.Unlock()

	// Check connection health and recover IDLE connections.
	// gRPC connections can transition to IDLE after the server restarts
	// (e.g., during tableflip). In IDLE state, gRPC won't attempt to
	// reconnect until a new RPC is issued or Connect() is called explicitly.
	// Without this recovery, hadrons lose all tunnel connectivity to ions
	// that restarted and never reconnect — causing a full region outage.
	for i, conn := range g.connections {
		if i >= len(g.streamManager.connectionHealth) {
			break // connectionHealth not yet grown to match connections
		}
		if conn != nil {
			state := conn.GetState()
			isHealthy := state.String() == "READY" || state.String() == "CONNECTING"
			g.streamManager.connectionHealth[i] = isHealthy

			if !isHealthy {
				stateStr := state.String()
				if stateStr == "IDLE" {
					idleCount := 0
					if i < len(g.streamManager.connectionIdleCount) {
						g.streamManager.connectionIdleCount[i]++
						idleCount = g.streamManager.connectionIdleCount[i]
					}
					// Force gRPC to move from IDLE → CONNECTING → READY
					// (or TRANSIENT_FAILURE if the server is unreachable).
					conn.Connect()
					if idleCount > 3 {
						// Persistent IDLE — Connect() alone isn't recovering.
						// Escalate to full endpoint reconnection which tears
						// down and rebuilds streams.
						g.logger.Warn("connection %d is persistently IDLE (count=%d), escalating to endpoint reconnection", i, idleCount)
						go g.handleEndpointDisconnection(i, "persistent_idle")
					} else {
						g.logger.Info("connection %d is IDLE (count=%d), forced reconnection via Connect()", i, idleCount)
					}
				} else {
					g.logger.Debug("connection %d is unhealthy, state: %s", i, stateStr)
					// Reset idle counter on non-IDLE unhealthy states
					// (TRANSIENT_FAILURE, SHUTDOWN already have retry logic)
					if i < len(g.streamManager.connectionIdleCount) {
						g.streamManager.connectionIdleCount[i] = 0
					}
				}
			} else {
				// Connection is healthy — reset idle counter
				if i < len(g.streamManager.connectionIdleCount) {
					g.streamManager.connectionIdleCount[i] = 0
				}
			}
		} else {
			g.streamManager.connectionHealth[i] = false
		}
	}

	// Check stream health and reset unhealthy streams that haven't been used recently
	now := time.Now()
	for i, streamInfo := range g.streamManager.tunnelStreams {
		if streamInfo != nil {
			// Reset streams that have been unhealthy for too long
			if !streamInfo.isHealthy && now.Sub(streamInfo.lastUsed) > g.poolConfig.FailoverTimeout {
				connReady := false
				if streamInfo.stream != nil && streamInfo.connIndex >= 0 && streamInfo.connIndex < len(g.connections) {
					if conn := g.connections[streamInfo.connIndex]; conn != nil {
						connReady = conn.GetState().String() == "READY"
					}
				}

				if connReady {
					g.logger.Debug("attempting to recover stream %s", streamInfo.streamID)
					streamInfo.isHealthy = true // Try again
					streamInfo.loadCount = 0    // Reset load

					// Reset error metrics
					g.streamManager.metricsMu.Lock()
					if metrics := g.streamManager.streamMetrics[streamInfo.streamID]; metrics != nil {
						metrics.ErrorCount = 0
					}
					g.streamManager.metricsMu.Unlock()
				} else {
					g.logger.Debug("stream %s remains unhealthy: stream nil=%v conn ready=%v", streamInfo.streamID, streamInfo.stream == nil, connReady)
				}
			}

			// Log unhealthy streams
			if !streamInfo.isHealthy {
				g.logger.Debug("stream %s (index %d) is unhealthy", streamInfo.streamID, i)
			}
		}
	}

	go g.refreshEndpointHealth()
}

// GetConnectionPoolStats returns current connection pool statistics for monitoring
func (g *GravityClient) GetConnectionPoolStats() map[string]any {
	g.streamManager.tunnelMu.RLock()
	g.streamManager.healthMu.RLock()
	g.streamManager.metricsMu.RLock()
	defer g.streamManager.tunnelMu.RUnlock()
	defer g.streamManager.healthMu.RUnlock()
	defer g.streamManager.metricsMu.RUnlock()

	healthyConnections := 0
	for _, healthy := range g.streamManager.connectionHealth {
		if healthy {
			healthyConnections++
		}
	}

	healthyStreams := 0
	totalLoad := int64(0)
	for _, streamInfo := range g.streamManager.tunnelStreams {
		if streamInfo != nil && streamInfo.isHealthy {
			healthyStreams++
			totalLoad += streamInfo.loadCount
		}
	}

	stats := map[string]any{
		"pool_size":              g.poolConfig.PoolSize,
		"streams_per_connection": g.poolConfig.StreamsPerConnection,
		"allocation_strategy":    g.poolConfig.AllocationStrategy.String(),
		"healthy_connections":    healthyConnections,
		"total_connections":      len(g.connections),
		"healthy_streams":        healthyStreams,
		"total_streams":          len(g.streamManager.tunnelStreams),
		"total_load":             totalLoad,
		"stream_metrics":         copyStreamMetrics(g.streamManager.streamMetrics),
	}

	return stats
}

func copyStreamMetrics(metrics map[string]*StreamMetrics) map[string]*StreamMetrics {
	if metrics == nil {
		return nil
	}
	copied := make(map[string]*StreamMetrics, len(metrics))
	for key, value := range metrics {
		if value == nil {
			copied[key] = nil
			continue
		}
		v := *value
		copied[key] = &v
	}
	return copied
}

// TunnelStatsSnapshot is a point-in-time snapshot of tunnel counters.
// All values are cumulative totals (deltas are computed by the caller).
type TunnelStatsSnapshot struct {
	// Inbound (gravity → hadron → container)
	InboundReceived  uint64
	InboundDelivered uint64
	InboundDropped   uint64
	InboundBytes     uint64

	// Outbound (container → hadron → gravity)
	OutboundReceived uint64
	OutboundSent     uint64
	OutboundErrors   uint64
	OutboundBytes    uint64

	// Control plane
	PingsSent      uint64
	PongsReceived  uint64
	PingTimeouts   uint64
	LastPingSentUs int64
	LastPongRecvUs int64

	// Channel
	InboundChannelLen int
	InboundChannelCap int
	InboundHighWater  int32

	// Connection pool (point-in-time)
	TotalConnections      int
	HealthyConnections    int
	TotalTunnelStreams    int
	HealthyTunnelStreams  int
	TotalControlStreams   int
	HealthyControlStreams int

	// Per-stream metrics
	StreamMetrics map[string]StreamMetricsSnapshot
}

// StreamMetricsSnapshot is a point-in-time copy of per-stream metrics.
type StreamMetricsSnapshot struct {
	StreamID        string
	ConnectionIndex int
	Healthy         bool
	PacketsSent     int64
	PacketsReceived int64
	BytesSent       int64
	BytesReceived   int64
	ErrorCount      int64
	LastSendUs      int64
	LastRecvUs      int64
}

// TunnelStats returns a point-in-time snapshot of all tunnel health counters.
// This is safe to call from any goroutine (all reads are atomic or under lock).
// NOTE: InboundHighWater is reset to 0 on each call (atomic Swap).
func (g *GravityClient) TunnelStats() TunnelStatsSnapshot {
	snap := TunnelStatsSnapshot{
		InboundReceived:  g.inboundReceived.Load(),
		InboundDelivered: g.inboundDelivered.Load(),
		InboundDropped:   g.inboundDropped.Load(),
		InboundBytes:     g.inboundBytes.Load(),

		OutboundReceived: g.outboundReceived.Load(),
		OutboundSent:     g.outboundSent.Load(),
		OutboundErrors:   g.outboundErrors.Load(),
		OutboundBytes:    g.outboundBytes.Load(),

		PingsSent:      g.pingsSent.Load(),
		PongsReceived:  g.pongsReceived.Load(),
		PingTimeouts:   g.pingTimeouts.Load(),
		LastPingSentUs: g.lastPingSentUs.Load(),
		LastPongRecvUs: g.lastPongRecvUs.Load(),

		InboundChannelLen: len(g.inboundPackets),
		InboundChannelCap: cap(g.inboundPackets),
		InboundHighWater:  g.inboundHighWater.Swap(0), // reset on read
	}

	// Connection pool stats (requires locks)
	g.streamManager.healthMu.RLock()
	for _, healthy := range g.streamManager.connectionHealth {
		snap.TotalConnections++
		if healthy {
			snap.HealthyConnections++
		}
	}
	g.streamManager.healthMu.RUnlock()

	g.streamManager.tunnelMu.RLock()
	snap.StreamMetrics = make(map[string]StreamMetricsSnapshot, len(g.streamManager.tunnelStreams))
	for _, si := range g.streamManager.tunnelStreams {
		if si == nil {
			continue
		}
		snap.TotalTunnelStreams++
		if si.isHealthy {
			snap.HealthyTunnelStreams++
		}
		snap.StreamMetrics[si.streamID] = StreamMetricsSnapshot{
			StreamID:        si.streamID,
			ConnectionIndex: si.connIndex,
			Healthy:         si.isHealthy,
		}
	}
	g.streamManager.tunnelMu.RUnlock()

	g.streamManager.controlMu.RLock()
	for _, cs := range g.streamManager.controlStreams {
		snap.TotalControlStreams++
		if cs != nil {
			snap.HealthyControlStreams++
		}
	}
	g.streamManager.controlMu.RUnlock()

	// Overlay per-stream packet metrics
	g.streamManager.metricsMu.RLock()
	for id, m := range g.streamManager.streamMetrics {
		if existing, ok := snap.StreamMetrics[id]; ok {
			existing.PacketsSent = m.PacketsSent
			existing.PacketsReceived = m.PacketsReceived
			existing.BytesSent = m.BytesSent
			existing.BytesReceived = m.BytesReceived
			existing.ErrorCount = m.ErrorCount
			existing.LastSendUs = m.LastSendUs
			existing.LastRecvUs = m.LastRecvUs
			snap.StreamMetrics[id] = existing
		}
	}
	g.streamManager.metricsMu.RUnlock()

	return snap
}

// String method for StreamAllocationStrategy enum
func (s StreamAllocationStrategy) String() string {
	switch s {
	case RoundRobin:
		return "RoundRobin"
	case HashBased:
		return "HashBased"
	case LeastConnections:
		return "LeastConnections"
	case WeightedRoundRobin:
		return "WeightedRoundRobin"
	default:
		return "Unknown"
	}
}

func (g *GravityClient) handleTextMessages() {
	connCtx := g.currentConnectionContext()
	for {
		select {
		case <-connCtx.Done():
			return
		case msg := <-g.textMessages:
			// Placeholder path: currently there is no text-message producer.
			// We still drain and return buffers so pooled memory is not leaked.
			g.returnBuffer(msg)
		}
	}
}

// Additional helper functions

const defaultGravityServerName = "gravity.agentuity.com"

// extractHostnameFromGravityURL extracts hostname from URL for TLS configuration.
// When the URL contains an IP address instead of a hostname, fallbackServerName
// is used for TLS SNI verification. If fallbackServerName is empty, it defaults
// to "gravity.agentuity.com".
func extractHostnameFromGravityURL(inputURL string, fallbackServerName string) (string, error) {
	// Parse the URL to extract just the hostname
	var urlStr string
	if len(inputURL) >= 7 && inputURL[:7] == "grpc://" {
		// Convert grpc:// to http:// for URL parsing
		urlStr = "http://" + inputURL[7:]
	} else {
		urlStr = "http://" + inputURL
	}

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}

	hostname := parsedURL.Hostname()
	if hostname == "" {
		return "", fmt.Errorf("no hostname found in URL")
	}

	if net.ParseIP(hostname) != nil {
		if fallbackServerName != "" {
			hostname = fallbackServerName
		} else {
			hostname = defaultGravityServerName
		}
	}

	return hostname, nil
}

func createSelfSignedTLSConfig(privateKey *ecdsa.PrivateKey, instanceID string) (*tls.Certificate, error) {
	// Generate self-signed certificate from ECDSA private key
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			CommonName: instanceID,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour), // 1 year; cert regenerated each startup
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create self-signed certificate: %w", err)
	}

	leaf, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("failed to parse self-signed certificate: %w", err)
	}

	cert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  privateKey,
		Leaf:        leaf,
	}

	return &cert, nil
}

func getHostInfo(config GravityConfig) (*pb.HostInfo, error) {
	// Get runtime information about the host
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-host"
	}

	// Get CPU count
	cpuCount := runtime.NumCPU()

	// Get actual memory info using system calls
	memoryBytes := getSystemMemory()

	// Get disk info for current working directory
	diskBytes := getDiskFreeSpace(config.WorkingDir)

	return &pb.HostInfo{
		Started:          uint64(time.Now().UnixMilli()),
		Cpu:              uint32(cpuCount),
		Memory:           memoryBytes,
		Disk:             diskBytes,
		Ipv4Address:      config.IP4Address,
		Ipv6Address:      config.IP6Address,
		Hostname:         hostname,
		InstanceId:       config.InstanceID,
		Region:           config.Region,
		AvailabilityZone: config.AvailabilityZone,
		Provider:         config.CloudProvider,
		InstanceType:     config.InstanceType,
		InstanceTags:     config.InstanceTags,
		RestartCount:     config.RestartCount,
	}, nil
}

// SendMonitorReport sends a NodeMonitorReport to the gravity server via the control stream.
// This is fire-and-forget — no response is expected. If the stream is unavailable,
// the report is silently dropped (stale data is worse than missing data).
// The send is bounded by a short timeout so it cannot block the monitor loop.
func (g *GravityClient) SendMonitorReport(report *pb.NodeMonitorReport) error {
	g.streamManager.controlMu.RLock()
	if len(g.streamManager.controlStreams) == 0 || g.streamManager.controlStreams[0] == nil {
		g.streamManager.controlMu.RUnlock()
		return nil // silently drop if no stream available
	}
	controlStream := g.streamManager.controlStreams[0]
	g.streamManager.controlMu.RUnlock()

	msg := &pb.SessionMessage{
		Id: generateMessageID(),
		MessageType: &pb.SessionMessage_MonitorReport{
			MonitorReport: report,
		},
	}

	// Use a bounded send so controlStream.Send cannot block indefinitely.
	done := make(chan error, 1)
	go func() {
		if len(g.streamManager.controlSendMu) > 0 {
			g.streamManager.controlSendMu[0].Lock()
			defer g.streamManager.controlSendMu[0].Unlock()
		}
		done <- controlStream.Send(msg)
	}()

	select {
	case err := <-done:
		if err != nil {
			g.logger.Debug("failed to send monitor report: %v", err)
			return err
		}
		return nil
	case <-time.After(5 * time.Second):
		g.logger.Debug("monitor report send timed out, dropping")
		return nil
	}
}

// handlePingHeartbeat sends periodic ping messages to maintain connection health
func (g *GravityClient) handlePingHeartbeat() {
	connCtx := g.currentConnectionContext()
	if g.pingInterval <= 0 {
		g.logger.Debug("ping interval is disabled (zero or negative), skipping heartbeat")
		return
	}

	ticker := time.NewTicker(g.pingInterval)
	defer ticker.Stop()

	g.logger.Debug("starting heartbeat ping ticker with interval: %v", g.pingInterval)

	for {
		select {
		case <-connCtx.Done():
			g.logger.Debug("ping heartbeat goroutine stopped")
			return
		case <-ticker.C:
			g.sendPing()
		}
	}
}

// sendPing sends a ping message to the server and starts a deadline timer
// to detect pong timeouts. If the corresponding pong is not received within
// one ping interval, pingTimeouts is incremented.
func (g *GravityClient) sendPing() {
	g.streamManager.controlMu.RLock()
	if len(g.streamManager.controlStreams) == 0 {
		g.streamManager.controlMu.RUnlock()
		g.logger.Debug("no control streams available for ping")
		return
	}
	streams := make([]pb.GravitySessionService_EstablishSessionClient, len(g.streamManager.controlStreams))
	copy(streams, g.streamManager.controlStreams)
	g.streamManager.controlMu.RUnlock()

	if g.multiEndpointMode.Load() {
		for i, stream := range streams {
			if stream == nil {
				continue
			}
			g.sendPingOnStream(i, stream)
		}
		return
	}

	if len(streams) == 0 || streams[0] == nil {
		g.logger.Debug("no control streams available for ping")
		return
	}

	g.sendPingOnStream(0, streams[0])
}

func (g *GravityClient) sendPingOnStream(streamIndex int, controlStream pb.GravitySessionService_EstablishSessionClient) {
	if controlStream == nil {
		return
	}

	started := time.Now()
	pingID := generateMessageID()

	// Register for pong response before sending so handlePong() can
	// correlate the reply by message ID and signal the deadline goroutine.
	responseChan := make(chan *pb.ProtocolResponse, 1)
	g.pendingMu.Lock()
	g.pending[pingID] = responseChan
	g.pendingMu.Unlock()

	pingMsg := &pb.SessionMessage{
		Id: pingID,
		MessageType: &pb.SessionMessage_Ping{
			Ping: &pb.PingRequest{
				Timestamp: timestamppb.New(started),
			},
		},
	}

	// Guard against a blocked Send(): if the control stream is wedged,
	// Send() blocks indefinitely, wedging the heartbeat goroutine.
	// Fire a timer that records the timeout and triggers reconnection
	// (which cancels stream contexts, unblocking Send).
	sendBlocked := time.AfterFunc(g.pingInterval, func() {
		g.pingTimeouts.Add(1)
		g.logger.Info("ping %s send blocked on control stream %d for %v", pingID, streamIndex, g.pingInterval)
		g.pendingMu.Lock()
		delete(g.pending, pingID)
		g.pendingMu.Unlock()
		if g.multiEndpointMode.Load() {
			go g.handleEndpointDisconnection(streamIndex, "ping_send_blocked")
		} else {
			go g.handleServerDisconnection("ping_send_blocked")
		}
	})

	if streamIndex < 0 || streamIndex >= len(g.streamManager.controlSendMu) {
		g.logger.Error("ping aborted: controlSendMu index %d out of range (len=%d)", streamIndex, len(g.streamManager.controlSendMu))
		g.pendingMu.Lock()
		delete(g.pending, pingID)
		g.pendingMu.Unlock()
		return
	}
	g.streamManager.controlSendMu[streamIndex].Lock()
	err := controlStream.Send(pingMsg)
	g.streamManager.controlSendMu[streamIndex].Unlock()
	sendBlocked.Stop()

	if err != nil {
		g.logger.Error("failed to send ping: %v", err)
		g.pendingMu.Lock()
		delete(g.pending, pingID)
		g.pendingMu.Unlock()
		return
	}

	// If the sendBlocked timer fired before Send() returned (race between
	// Stop and the timer goroutine), it already incremented pingTimeouts
	// and deleted pending[pingID]. Check the map to avoid double-counting.
	g.pendingMu.RLock()
	_, stillPending := g.pending[pingID]
	g.pendingMu.RUnlock()
	if !stillPending {
		// Timer already handled this ping — don't start the pong wait.
		return
	}

	g.pingsSent.Add(1)
	g.lastPingSentUs.Store(time.Now().UnixMicro())
	if g.traceLog {
		g.logger.Trace("sent ping message on control stream %d: id=%s, took=%v", streamIndex, pingID, time.Since(started))
	}

	// Wait for the pong with a deadline of one ping interval. If the pong
	// doesn't arrive in time, increment pingTimeouts. The goroutine is
	// short-lived and bounded by the deadline or client context cancellation.
	go func() {
		connCtx := g.currentConnectionContext()
		defer func() {
			g.pendingMu.Lock()
			delete(g.pending, pingID)
			g.pendingMu.Unlock()
		}()

		select {
		case <-responseChan:
			// Pong received in time — already counted by handlePong.
		case <-time.After(g.pingInterval):
			g.pingTimeouts.Add(1)
			g.logger.Info("ping %s pong timed out on control stream %d after %v", pingID, streamIndex, g.pingInterval)
		case <-connCtx.Done():
			// Client shutting down.
		}
	}()
}

// Buffer management

func (g *GravityClient) getBuffer(payload []byte) *PooledBuffer {
	if len(payload) > maxBufferSize {
		buf := make([]byte, len(payload))
		copy(buf, payload)
		return &PooledBuffer{
			Buffer: buf,
			Length: len(payload),
		}
	}

	buf := g.bufferPool.Get().([]byte)
	if len(payload) > len(buf) {
		g.bufferPool.Put(buf)
		buf = make([]byte, len(payload))
	}
	copy(buf, payload)
	return &PooledBuffer{
		Buffer: buf,
		Length: len(payload),
	}
}

func (g *GravityClient) returnBuffer(pooledBuf *PooledBuffer) {
	if pooledBuf == nil {
		return
	}
	if len(pooledBuf.Buffer) == maxBufferSize {
		g.bufferPool.Put(pooledBuf.Buffer)
	}
}

// GetDeploymentMetadata makes a gRPC call to get deployment metadata
func (g *GravityClient) GetDeploymentMetadata(ctx context.Context, deploymentID, orgID string) (*pb.DeploymentMetadataResponse, error) {
	if len(g.sessionClients) == 0 {
		return nil, fmt.Errorf("no gRPC session clients available")
	}

	metadataRequest := &pb.DeploymentMetadataRequest{
		DeploymentId: deploymentID,
		OrgId:        orgID,
	}

	md := metadata.Pairs("authorization", "Bearer "+g.authorizationToken)
	authCtx := metadata.NewOutgoingContext(ctx, md)

	return g.sessionClients[0].GetDeploymentMetadata(authCtx, metadataRequest)
}

// GetSandboxMetadata makes a gRPC call to get sandbox metadata
func (g *GravityClient) GetSandboxMetadata(ctx context.Context, sandboxID, orgID string, generateCertificate bool) (*pb.SandboxMetadataResponse, error) {
	if len(g.sessionClients) == 0 {
		return nil, fmt.Errorf("no gRPC session clients available")
	}

	metadataRequest := &pb.SandboxMetadataRequest{
		SandboxId:           sandboxID,
		OrgId:               orgID,
		GenerateCertificate: generateCertificate,
	}

	md := metadata.Pairs("authorization", "Bearer "+g.authorizationToken)
	authCtx := metadata.NewOutgoingContext(ctx, md)

	return g.sessionClients[0].GetSandboxMetadata(authCtx, metadataRequest)
}

// GetIPv6Address returns the IPv6 address for external use
func (g *GravityClient) GetIPv6Address() string {
	return g.ip6Address
}

// GetSecret returns the authentication secret for external use
func (g *GravityClient) GetSecret() string {
	return g.authorizationToken
}

// GetAPIURL returns the API URL received from gravity server
func (g *GravityClient) GetAPIURL() string {
	return g.apiURL
}
