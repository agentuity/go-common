package gravity

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

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
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
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
}

// StreamAllocationStrategy defines how streams are selected for load distribution
type StreamAllocationStrategy int

const (
	RoundRobin StreamAllocationStrategy = iota
	HashBased
	LeastConnections
	WeightedRoundRobin
)

// GravityClient implements the provider.Server interface using gRPC transport
type GravityClient struct {
	// Configuration
	context            context.Context
	logger             logger.Logger
	provider           provider.Provider
	url                string
	authorizationToken string
	ip4Address         string
	ip6Address         string
	clientVersion      string
	clientName         string
	capabilities       *pb.ClientCapabilities
	hostInfo           *pb.HostInfo
	workingDir         string
	reportStats        bool

	// Connection pool configuration
	poolConfig ConnectionPoolConfig

	// gRPC connections and streams
	connections    []*grpc.ClientConn // Connection pool (4-8 connections)
	controlClients []pb.GravityControlClient
	tunnelClients  []pb.GravityTunnelClient
	streamManager  *StreamManager

	// Fault tolerance
	retryConfig     RetryConfig
	circuitBreakers []*CircuitBreaker // One per connection

	// Performance monitoring - optional
	serverMetrics *ServerMetrics

	// Connection management
	mu                sync.RWMutex
	closing           bool
	ctx               context.Context
	cancel            context.CancelFunc
	tlsConfig         *tls.Config
	skipAutoReconnect bool
	closed            chan struct{}

	// State management
	connected        bool
	reconnecting     bool        // Tracks if reconnection is in progress
	connectionIDs    []string    // Stores connection IDs from server responses
	connectionIDChan chan string // Channel to signal when connection ID is received
	otlpURL          string
	otlpToken        string
	apiURL           string
	hostMapping      []*pb.HostMapping
	hostEnvironment  []string
	once             sync.Once

	// Messaging
	response  chan *pb.ProtocolResponse
	pending   map[string]chan *pb.ProtocolResponse
	pendingMu sync.RWMutex

	// Route deployment responses
	pendingRouteDeployment   map[string]chan *pb.RouteDeploymentResponse
	pendingRouteDeploymentMu sync.RWMutex

	// Packet channels for network device multiplexing
	inboundPackets  chan *PooledBuffer
	outboundPackets chan []byte
	textMessages    chan *PooledBuffer

	// agentuity internal certificate
	caCert string

	// Buffer pool for memory efficiency
	bufferPool sync.Pool

	// heartbeat configuration
	pingInterval time.Duration

	// report delivery configuration
	reportInterval time.Duration

	// otel
	tracer trace.Tracer

	// trace packet logging
	tracePackets      bool
	tracePacketLogger logger.Logger

	// network interface for routing
	networkInterface network.NetworkInterface
}

// StreamInfo tracks individual stream health and load
type StreamInfo struct {
	stream    pb.GravityTunnel_StreamPacketsClient
	connIndex int       // Connection index this stream belongs to
	streamID  string    // Unique stream identifier
	isHealthy bool      // Stream health status
	loadCount int64     // Current load (packets in flight)
	lastUsed  time.Time // Last time this stream was used
}

// StreamManager manages multiple gRPC streams for multiplexing with advanced load balancing
type StreamManager struct {
	// Control streams (one per connection) - now using tunnel service
	controlStreams []pb.GravityTunnel_EstablishTunnelClient
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
	connectionHealth []bool // Health status per connection
	healthMu         sync.RWMutex

	// Stream contexts for cancellation
	contexts []context.Context
	cancels  []context.CancelFunc
}

// StreamMetrics tracks performance metrics for individual streams
type StreamMetrics struct {
	PacketsSent     int64
	PacketsReceived int64
	LastLatency     time.Duration
	ErrorCount      int64
	LastError       time.Time
}

// New creates a new gRPC-based Gravity server client
func New(config GravityConfig) (*GravityClient, error) {
	tlsConfig, err := createTLSConfig(config.Cert, config.Key, config.CACert)
	if err != nil {
		return nil, fmt.Errorf("error creating TLS configuration: %w", err)
	}

	// Get host information
	hostInfo, err := getHostInfo(config.WorkingDir, config.IP4Address, config.IP6Address, config.InstanceID)
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
		authorizationToken:     config.AuthToken,
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
		tlsConfig:              tlsConfig,
		caCert:                 config.CACert,
		connectionIDs:          make([]string, 0, poolConfig.PoolSize),
		connectionIDChan:       make(chan string, 1), // Buffered channel for connection ID
		response:               make(chan *pb.ProtocolResponse, 100),
		pending:                make(map[string]chan *pb.ProtocolResponse),
		pendingRouteDeployment: make(map[string]chan *pb.RouteDeploymentResponse),
		inboundPackets:         make(chan *PooledBuffer, 1000),
		outboundPackets:        make(chan []byte, 1000),
		textMessages:           make(chan *PooledBuffer, 100),
		pingInterval:           config.PingInterval,
		reportInterval:         config.ReportInterval,
		streamManager: &StreamManager{
			streamMetrics:      make(map[string]*StreamMetrics),
			allocationStrategy: poolConfig.AllocationStrategy,
		},
		serverMetrics:     NewServerMetrics(),
		workingDir:        config.WorkingDir,
		tracePackets:      config.TraceLogPackets,
		reportStats:       config.ReportStats,
		skipAutoReconnect: config.SkipAutoReconnect,
		closed:            make(chan struct{}, 1),
		tracer:            otel.Tracer("@agentuity/gravity/client"),
	}

	if config.TraceLogPackets {
		g.tracePacketLogger = logger.NewConsoleLogger()
	}

	// Initialize buffer pool
	g.bufferPool.New = func() any {
		return make([]byte, maxBufferSize)
	}

	return g, nil
}

// Start establishes gRPC connections and starts the client
func (g *GravityClient) Start() error {
	g.logger.Debug("starting gRPC Gravity client...")
	g.mu.Lock()

	if g.connected {
		g.mu.Unlock()
		return fmt.Errorf("already connected")
	}

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

	g.logger.Debug("creating TLS configuration...")
	// Create TLS config that includes both client certificates (mTLS) and server verification
	tlsConfig := &tls.Config{
		Certificates: g.tlsConfig.Certificates,    // Client certificates for mTLS
		RootCAs:      g.tlsConfig.RootCAs.Clone(), // CA for server verification
		ServerName:   hostname,                    // Dynamic server name for SNI and verification
		MinVersion:   tls.VersionTLS13,
	}
	creds := credentials.NewTLS(tlsConfig)
	g.logger.Debug("TLS credentials created successfully")

	// Create connection pool using configuration
	connectionCount := g.poolConfig.PoolSize
	g.logger.Debug("creating connection pool with %d connections", connectionCount)
	g.connections = make([]*grpc.ClientConn, connectionCount)
	g.controlClients = make([]pb.GravityControlClient, connectionCount)
	g.tunnelClients = make([]pb.GravityTunnelClient, connectionCount)
	g.circuitBreakers = make([]*CircuitBreaker, connectionCount)

	// Initialize connection health tracking
	g.streamManager.connectionHealth = make([]bool, connectionCount)
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
		)
		if err != nil {
			g.logger.Error("failed to create gRPC client %d: %v", i+1, err)
			// Cleanup partial connections
			g.cleanup()
			return fmt.Errorf("failed to create gRPC client to %s: %w", grpcURL, err)
		}
		g.logger.Debug("gRPC client %d created successfully", i+1)

		g.connections[i] = conn
		g.controlClients[i] = pb.NewGravityControlClient(conn)
		g.tunnelClients[i] = pb.NewGravityTunnelClient(conn)
		g.circuitBreakers[i] = NewCircuitBreaker(DefaultCircuitBreakerConfig())
	}
	g.logger.Debug("all %d gRPC clients created successfully", connectionCount)

	// Release mutex before blocking operations (control streams, connect, tunnel streams)
	g.mu.Unlock()

	g.logger.Debug("establishing control streams...")
	// Establish control streams (one per connection)
	err = g.establishControlStreams()
	if err != nil {
		g.logger.Error("failed to establish control streams: %v", err)
		g.cleanup()
		return fmt.Errorf("failed to establish control streams: %w", err)
	}
	g.logger.Debug("control streams established successfully")

	g.logger.Debug("sending initial connect message...")
	// Send initial connect message first to get connection IDs
	err = g.sendConnectMessage()
	if err != nil {
		g.logger.Error("failed to send connect message: %v", err)
		g.cleanup()
		return fmt.Errorf("failed to send connect message: %w", err)
	}
	g.logger.Debug("connect message sent successfully")

	g.logger.Debug("establishing tunnel streams...")
	// Establish tunnel streams (multiple per connection) - after connect message
	err = g.establishTunnelStreams()
	if err != nil {
		g.logger.Error("failed to establish tunnel streams: %v", err)
		g.cleanup()
		return fmt.Errorf("failed to establish tunnel streams: %w", err)
	}
	g.logger.Debug("tunnel streams established successfully")

	// Metrics collector is now hadron-specific and handled externally

	// Set up stats collection if provider supports it
	// All providers should have these methods based on the Provider interface
	g.logger.Debug("configuring stats collection for provider")
	// Note: SetMetricsCollector will be called externally by hadron

	// Re-acquire mutex for final state updates
	g.mu.Lock()
	g.connected = true
	g.mu.Unlock()

	g.serverMetrics.UpdateConnection(true)
	g.logger.Debug("connected to Gravity server via gRPC: %s", grpcURL)

	g.logger.Debug("starting background goroutines...")
	// Start background goroutines
	go g.handleInboundPackets()
	go g.handleOutboundPackets()
	go g.handleTextMessages()

	go g.monitorConnectionHealth()
	go g.handlePingHeartbeat()

	if g.reportStats {
		go g.handleReportDelivery()
	}
	g.logger.Debug("all background goroutines started successfully")

	g.logger.Debug("gRPC Gravity client startup completed successfully")
	return nil
}

// establishControlStreams creates control streams for each connection
func (g *GravityClient) establishControlStreams() error {
	g.logger.Debug("creating control streams for %d connections", len(g.connections))
	g.streamManager.controlStreams = make([]pb.GravityTunnel_EstablishTunnelClient, len(g.connections))
	g.streamManager.contexts = make([]context.Context, len(g.connections))
	g.streamManager.cancels = make([]context.CancelFunc, len(g.connections))

	for i, client := range g.tunnelClients {
		g.logger.Debug("establishing control stream %d/%d", i+1, len(g.connections))
		ctx, cancel := context.WithCancel(g.ctx)
		g.streamManager.contexts[i] = ctx
		g.streamManager.cancels[i] = cancel

		// Add authorization metadata for control stream authentication
		md := metadata.Pairs("authorization", "Bearer "+g.authorizationToken)
		ctx = metadata.NewOutgoingContext(ctx, md)

		// Enable gzip compression for control streams (text-based control messages)
		// Use the grpc.UseCompressor call option for selective compression
		stream, err := client.EstablishTunnel(ctx, grpc.UseCompressor(grpcgzip.Name))
		if err != nil {
			g.logger.Error("failed to establish control stream %d: %v", i+1, err)
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

// establishTunnelStreams creates tunnel streams for packet forwarding
func (g *GravityClient) establishTunnelStreams() error {
	// Wait for connection ID from primary control stream using channel
	g.logger.Debug("waiting for connection ID from server...")

	var connectionID string
	select {
	case connectionID = <-g.connectionIDChan:
		if connectionID == "" {
			g.logger.Error("connection failed - authentication rejected by server")
			return fmt.Errorf("connection failed - authentication rejected by server")
		}
		g.logger.Debug("connection ID received: %s, proceeding with tunnel streams", connectionID)
	case <-time.After(time.Minute):
		g.logger.Error("timeout waiting for connection ID from server - possible server issue or network problem")
		return fmt.Errorf("timeout waiting for connection ID from server")
	}

	// Use configured streams per connection
	streamsPerConnection := g.poolConfig.StreamsPerConnection
	totalStreams := len(g.connections) * streamsPerConnection

	g.streamManager.tunnelStreams = make([]*StreamInfo, totalStreams)

	streamIndex := 0
	for connIndex, client := range g.tunnelClients {
		for range streamsPerConnection {
			streamID := fmt.Sprintf("stream_%s", rand.Text())
			ctx := context.WithValue(g.ctx, "connection-id", connectionID)
			ctx = context.WithValue(ctx, "stream-id", streamID)

			// Add metadata for stream identification and authentication
			md := metadata.Pairs(
				"connection-id", connectionID, // Use the single connection ID for all streams
				"stream-id", streamID,
				"authorization", "Bearer "+g.authorizationToken, // Add auth token
			)
			ctx = metadata.NewOutgoingContext(ctx, md)

			// No compression for tunnel streams (binary packet data for performance)
			// Tunnel streams remain uncompressed as per selective compression strategy

			stream, err := client.StreamPackets(ctx)
			if err != nil {
				return fmt.Errorf("failed to establish tunnel stream %d: %w", streamIndex, err)
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

			g.streamManager.tunnelStreams[streamIndex] = streamInfo

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
			go g.handleTunnelStream(streamIndex, stream)

			streamIndex++
		}
	}

	return nil
}

// sendConnectMessage sends the initial connect message
func (g *GravityClient) sendConnectMessage() error {
	// Convert existing deployments to protobuf format
	var existingDeployments []*pb.ExistingDeployment
	resources := g.provider.Resources() // Use Resources() method from Provider interface

	g.logger.Debug("gathering current deployment state for server synchronization...")
	g.logger.Debug("found %d existing deployments to synchronize with server", len(resources))

	for _, res := range resources {
		g.logger.Debug("synchronizing deployment: ID=%s, IPv6=%s, Started=%s", res.GetId(), res.GetIpv6Address(), res.GetStarted().AsTime().Format("2006-01-02 15:04:05"))
		existingDeployments = append(existingDeployments, res)
	}

	if len(existingDeployments) > 0 {
		g.logger.Debug("sending %d existing deployments to gravity server for state synchronization", len(existingDeployments))
	} else {
		g.logger.Debug("no existing deployments to synchronize - this is a fresh connection")
	}

	// Create connect request
	connectReq := &pb.ConnectRequest{
		ProtocolVersion: protocolVersion,
		ClientVersion:   g.clientVersion,
		ClientName:      g.clientName,
		Capabilities:    g.capabilities,
		Deployments:     existingDeployments,
		HostInfo:        g.hostInfo,
	}

	// Send on the first control stream
	msg := &pb.ControlMessage{
		Id:       "connect",
		StreamId: "connect",
		MessageType: &pb.ControlMessage_Connect{
			Connect: connectReq,
		},
	}

	// Send connect message on FIRST control stream only to establish client identity
	g.logger.Debug("sending connect message on primary control stream")
	g.logger.Debug("connect message details: ID=%s, ProtocolVersion=%d, ClientName=%s, ClientVersion=%s, Capabilities=[Provision:%v, Unprovision:%v, ProjectRouting:%s, DynamicHostname:%v]",
		msg.Id, connectReq.ProtocolVersion, connectReq.ClientName, connectReq.ClientVersion,
		connectReq.Capabilities.GetProvisionDeployments(), connectReq.Capabilities.GetUnprovisionDeployments(), connectReq.Capabilities.GetDynamicProjectRouting(), connectReq.Capabilities.GetDynamicHostname())

	stream := g.streamManager.controlStreams[0]
	circuitBreaker := g.circuitBreakers[0]

	err := RetryWithCircuitBreaker(context.Background(), g.retryConfig, circuitBreaker, func() error {
		g.logger.Debug("actually sending connect message on primary control stream")
		return stream.Send(msg)
	})

	if err != nil {
		return fmt.Errorf("failed to send connect message: %w", err)
	}

	return nil
}

// handleControlStream processes messages from a control stream
func (g *GravityClient) handleControlStream(streamIndex int, stream pb.GravityTunnel_EstablishTunnelClient) {
	defer func() {
		if r := recover(); r != nil {
			g.logger.Error("control stream %d panic: %v", streamIndex, r)
		}
		g.logger.Debug("control stream %d handler exiting", streamIndex)
	}()

	g.logger.Debug("control stream %d handler started", streamIndex)
	for {
		g.logger.Debug("control stream %d waiting for message", streamIndex)
		msg, err := stream.Recv()
		if err != nil {
			// Check if this is a context cancellation (graceful shutdown)
			if st, ok := status.FromError(err); ok && st.Code() == codes.Canceled {
				g.logger.Debug("control stream %d closed due to context cancellation", streamIndex)
			} else {
				g.logger.Error("control stream %d receive error: %v", streamIndex, err)
				// Check if this is a connection error that requires reconnection
				if errors.Is(err, io.EOF) {
					g.logger.Warn("control stream %d connection lost, triggering reconnection", streamIndex)
					go g.handleServerDisconnection(fmt.Sprintf("control_stream_%d_error", streamIndex))
				} else if st, ok := status.FromError(err); ok && st.Code() == codes.Unavailable {
					g.logger.Warn("control stream %d connection lost, triggering reconnection", streamIndex)
					go g.handleServerDisconnection(fmt.Sprintf("control_stream_%d_error", streamIndex))
				}
			}
			return
		}

		g.logger.Debug("control stream %d received message: ID=%s, Type=%T", streamIndex, msg.Id, msg.MessageType)
		g.logger.Debug("processing control message: ID=%s, Type=%T", msg.Id, msg.MessageType)
		// Process control message
		g.processControlMessage(msg)
	}
}

// handleTunnelStream processes packets from a tunnel stream
func (g *GravityClient) handleTunnelStream(streamIndex int, stream pb.GravityTunnel_StreamPacketsClient) {
	defer func() {
		if r := recover(); r != nil {
			g.logger.Error("tunnel stream %d panic: %v", streamIndex, r)
		}
	}()

	g.logger.Debug("handleTunnelStream: starting receive loop for stream %d", streamIndex)
	for {
		if g.tracePackets {
			g.tracePacketLogger.Debug("handleTunnelStream: calling stream.Recv() for stream %d", streamIndex)
		}
		packet, err := stream.Recv()
		if err != nil {
			// Check if this is a context cancellation (graceful shutdown)
			if st, ok := status.FromError(err); ok && st.Code() == codes.Canceled {
				g.logger.Debug("tunnel stream %d closed due to context cancellation", streamIndex)
			} else {
				g.logger.Error("tunnel stream %d receive error: %v", streamIndex, err)
				// Check if this is a connection error that requires reconnection
				if errors.Is(err, io.EOF) {
					g.logger.Info("tunnel stream %d connection lost, triggering reconnection", streamIndex)
					go g.handleServerDisconnection(fmt.Sprintf("tunnel_stream_%d_error", streamIndex))
				} else if st, ok := status.FromError(err); ok && st.Code() == codes.Unavailable {
					g.logger.Info("tunnel stream %d connection lost, triggering reconnection", streamIndex)
					go g.handleServerDisconnection(fmt.Sprintf("tunnel_stream_%d_error", streamIndex))
				}
			}
			return
		}

		if g.tracePackets {
			g.tracePacketLogger.Debug("handleTunnelStream: received packet with %d bytes on stream %d", len(packet.Data), streamIndex)
		}
		// Forward packet to local processing
		pooledBuf := g.getBuffer(packet.Data)
		select {
		case g.inboundPackets <- pooledBuf:
		default:
			// Channel full, drop packet
			g.returnBuffer(pooledBuf)
		}
	}
}

// processControlMessage processes incoming control messages
func (g *GravityClient) processControlMessage(msg *pb.ControlMessage) {
	switch m := msg.MessageType.(type) {
	case *pb.ControlMessage_ConnectResponse:
		g.logger.Debug("received ConnectResponse: msgID=%s, streamID=%s", msg.Id, msg.StreamId)
		g.handleConnectResponse(msg.Id, msg.StreamId, m.ConnectResponse)
	case *pb.ControlMessage_RouteDeploymentResponse:
		g.handleRouteDeploymentResponse(msg.Id, m.RouteDeploymentResponse)
	case *pb.ControlMessage_Unprovision:
		g.handleUnprovisionRequest(msg.Id, m.Unprovision)
	case *pb.ControlMessage_Report:
		// this is a server method
	case *pb.ControlMessage_Ping:
		g.handlePingRequest(msg.Id, m.Ping)
	case *pb.ControlMessage_Close:
		g.handleCloseRequest(msg.Id, m.Close)
	case *pb.ControlMessage_Pause:
		g.handlePauseRequest(msg.Id, m.Pause)
	case *pb.ControlMessage_Resume:
		g.handleResumeRequest(msg.Id, m.Resume)
	case *pb.ControlMessage_Response:
		g.handleGenericResponse(msg.Id, m.Response)
	case *pb.ControlMessage_Event:
		g.handleEvent(msg.Id, m.Event)
	case *pb.ControlMessage_Pong:
		g.handlePong(msg.Id, m.Pong)
	default:
		g.logger.Debug("unhandled control message type: %T", m)
	}
}

// Helper functions
func (g *GravityClient) handleConnectResponse(msgID string, connectionID string, response *pb.ConnectResponse) {
	g.logger.Debug("handleConnectResponse called: msgID=%s, connectionID=%s, gravityServer=%s response=%v", msgID, connectionID, response.GravityServer, response)

	g.logger.Debug("about to acquire mutex...")
	g.mu.Lock()
	g.logger.Debug("mutex acquired, storing connection ID...")

	// Store the connection ID for tunnel streams
	g.connectionIDs = append(g.connectionIDs, connectionID)
	numConnIDs := len(g.connectionIDs)
	g.logger.Debug("stored connection ID, now have %d connection IDs", numConnIDs)

	g.logger.Debug("setting response fields...")
	g.otlpURL = response.OtlpUrl
	g.apiURL = response.ApiUrl
	g.hostEnvironment = response.Environment

	// Store SSH public key if provided
	if len(response.SshPublicKey) > 0 {
		g.logger.Info("received SSH public key from Gravity (%d bytes)", len(response.SshPublicKey))
	}

	g.logger.Debug("storing host mappings...")
	// Store host mappings directly (already protobuf)
	g.hostMapping = response.HostMapping
	g.logger.Debug("about to release mutex...")
	g.mu.Unlock()
	g.logger.Debug("mutex released")

	// IMPORTANT: Send connection ID to channel FIRST before any blocking operations
	g.logger.Debug("about to send connection ID %s to channel...", connectionID)
	select {
	case g.connectionIDChan <- connectionID:
		g.logger.Debug("connection ID sent to channel successfully")
	default:
		g.logger.Error("connection ID channel full - this should not happen!")
		return // Early return if channel send fails
	}

	g.logger.Debug("configuring provider with gRPC server...")
	// Configure provider with the gRPC server as provider.Server interface
	if err := g.provider.Configure(provider.Configuration{
		Server:          g, // Critical: pass the gRPC server as provider.Server
		Context:         g.context,
		Logger:          g.logger,
		APIURL:          g.apiURL,
		SSHPublicKey:    response.SshPublicKey,
		TelemetryURL:    g.otlpURL,
		TelemetryAPIKey: response.OtlpKey,
		GravityURL:      g.url,
		AgentuityCACert: g.caCert,
		HostMapping:     g.hostMapping,
		Environment:     g.hostEnvironment,
		SubnetRoutes:    response.SubnetRoutes,
		Hostname:        response.Hostname,
		OrgID:           response.OrgId,
	}); err != nil {
		g.logger.Error("error configuring provider after connect: %v", err)
		return
	}
	g.logger.Debug("provider configured successfully")

	g.logger.Debug("configuring subnet routing for routes %v", response.SubnetRoutes)
	if err := g.networkInterface.RouteTraffic(response.SubnetRoutes); err != nil {
		g.logger.Error("failed to route traffic for gravity subnet: %v", err)
		return
	}

	g.logger.Debug("connected successfully to Gravity with connection ID: %s (total: %d)", connectionID, numConnIDs)

	// Log successful deployment synchronization
	deploymentCount := len(g.provider.Resources())
	if deploymentCount > 0 {
		g.logger.Debug("deployment state synchronization completed - server is now aware of %d existing deployments", deploymentCount)
	} else {
		g.logger.Debug("no existing deployments to synchronize - fresh connection established")
	}
}

func (g *GravityClient) handleGenericResponse(msgID string, response *pb.ProtocolResponse) {
	g.logger.Debug("received generic response: msgID=%s, success=%v, error=%s, event=%s",
		msgID, response.Success, response.Error, response.Event)

	// Check if this is an error response for a connect message
	if msgID == "connect" && !response.Success {
		g.logger.Error("connect request failed: %s", response.Error)
		// Signal error by closing the connection ID channel without sending a value
		select {
		case g.connectionIDChan <- "":
		default:
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
	} else {
		g.logger.Debug("no pending request found for msgID: %s", msgID)
	}
}

func (g *GravityClient) handleRouteDeploymentResponse(msgID string, response *pb.RouteDeploymentResponse) {
	g.logger.Debug("handleRouteDeploymentResponse: Received route deployment response for msgID=%s, ip=%s", msgID, response.Ip)

	// Find pending request and send response
	g.pendingRouteDeploymentMu.RLock()
	ch, exists := g.pendingRouteDeployment[msgID]
	g.pendingRouteDeploymentMu.RUnlock()

	if exists {
		select {
		case ch <- response:
		default:
		}
	} else {
		g.logger.Debug("handleRouteDeploymentResponse: No pending route deployment request found for msgID: %s", msgID)
	}
}

func (g *GravityClient) handleEvent(msgID string, event *pb.ProtocolEvent) {
	g.logger.Debug("received event: id=%s, event=%s", msgID, event.Event)

	switch event.Event {
	case "close":
		g.logger.Info("received close event from server")
		// For HA: disconnect and attempt reconnection instead of full shutdown
		g.handleServerDisconnection("close_event")
	case "provision":
		g.logger.Debug("received provision event from server")
		// Handle new deployment provisioning
		g.handleProvisionEvent(event)
	case "unprovision":
		g.logger.Debug("received unprovision event from server")
		// Handle deployment cleanup
		g.handleUnprovisionEvent(event)
	default:
		g.logger.Debug("unhandled event type: %s", event.Event)
	}
}

func (g *GravityClient) handleProvisionEvent(event *pb.ProtocolEvent) {
	g.logger.Debug("handling provision event: %s", string(event.Payload))

	// For now, just acknowledge the event
	// In a real implementation, this would trigger container/service provisioning
	response := &pb.ProtocolResponse{
		Id:      event.Id,
		Event:   "provision",
		Success: true,
	}

	responseMsg := &pb.ControlMessage{
		Id: event.Id,
		MessageType: &pb.ControlMessage_Response{
			Response: response,
		},
	}

	if len(g.streamManager.controlStreams) > 0 {
		if err := g.streamManager.controlStreams[0].Send(responseMsg); err != nil {
			g.logger.Error("failed to send provision response: %v", err)
		}
	} else {
		g.logger.Error("no control streams available for provision response")
	}
}

func (g *GravityClient) handleUnprovisionEvent(event *pb.ProtocolEvent) {
	g.logger.Debug("handling unprovision event: %s", string(event.Payload))

	// For now, just acknowledge the event
	// In a real implementation, this would trigger container/service cleanup
	response := &pb.ProtocolResponse{
		Id:      event.Id,
		Event:   "unprovision",
		Success: true,
	}

	responseMsg := &pb.ControlMessage{
		Id: event.Id,
		MessageType: &pb.ControlMessage_Response{
			Response: response,
		},
	}

	if len(g.streamManager.controlStreams) > 0 {
		if err := g.streamManager.controlStreams[0].Send(responseMsg); err != nil {
			g.logger.Error("failed to send unprovision response: %v", err)
		}
	} else {
		g.logger.Error("no control streams available for unprovision response")
	}
}

func (g *GravityClient) handlePong(msgID string, pong *pb.PongResponse) {
	g.logger.Debug("received pong response: id=%s", msgID)
	_ = pong

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

func (g *GravityClient) handleUnprovisionRequest(msgID string, request *pb.UnprovisionRequest) {
	g.logger.Debug("received unprovision request: deployment_id=%s", request.DeploymentId)

	// Call provider to deprovision the deployment
	ctx := context.WithoutCancel(g.context)
	err := g.provider.Deprovision(ctx, request.DeploymentId, provider.DeprovisionReasonUnprovision)

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
	responseMsg := &pb.ControlMessage{
		Id: msgID,
		MessageType: &pb.ControlMessage_Response{
			Response: response,
		},
	}

	if len(g.streamManager.controlStreams) > 0 && g.streamManager.controlStreams[0] != nil {
		err := g.streamManager.controlStreams[0].Send(responseMsg)
		if err != nil {
			g.logger.Error("failed to send unprovision response: %v", err)
		} else {
			g.logger.Debug("sent unprovision response for deployment %s: success=%v", request.DeploymentId, response.Success)
		}
	}
}

func (g *GravityClient) handlePingRequest(msgID string, request *pb.PingRequest) {
	g.logger.Debug("received ping request: id=%s", msgID)

	// Send pong response
	pongMsg := &pb.ControlMessage{
		Id: msgID,
		MessageType: &pb.ControlMessage_Pong{
			Pong: &pb.PongResponse{
				Timestamp: request.Timestamp, // Echo back the timestamp
			},
		},
	}

	if len(g.streamManager.controlStreams) > 0 && g.streamManager.controlStreams[0] != nil {
		err := g.streamManager.controlStreams[0].Send(pongMsg)
		if err != nil {
			g.logger.Error("failed to send pong response: %v", err)
		} else {
			g.logger.Debug("sent pong response for ping %s", msgID)
		}
	}
}

func (g *GravityClient) handleCloseRequest(msgID string, request *pb.CloseRequest) {
	g.logger.Debug("received close request: reason=%s", request.Reason)

	// Acknowledge the close request
	response := &pb.ProtocolResponse{
		Id:      msgID,
		Event:   "close",
		Success: true,
	}

	responseMsg := &pb.ControlMessage{
		Id: msgID,
		MessageType: &pb.ControlMessage_Response{
			Response: response,
		},
	}

	if len(g.streamManager.controlStreams) > 0 && g.streamManager.controlStreams[0] != nil {
		g.streamManager.controlStreams[0].Send(responseMsg)
	}

	// For HA: disconnect and attempt reconnection instead of full shutdown
	g.logger.Info("server requested close, attempting reconnection for HA")
	go g.handleServerDisconnection("close_request")
}

func (g *GravityClient) handlePauseRequest(msgID string, request *pb.PauseRequest) {
	g.logger.Debug("received pause request: reason=%s", request.Reason)

	// TODO: we need to implement pause from gravity

	g.logger.Debug("pausing hadron client operations: %s", request.Reason)

	// For now, just acknowledge the pause
	response := &pb.ProtocolResponse{
		Id:      msgID,
		Event:   "pause",
		Success: true,
	}

	responseMsg := &pb.ControlMessage{
		Id: msgID,
		MessageType: &pb.ControlMessage_Response{
			Response: response,
		},
	}

	if len(g.streamManager.controlStreams) > 0 && g.streamManager.controlStreams[0] != nil {
		err := g.streamManager.controlStreams[0].Send(responseMsg)
		if err != nil {
			g.logger.Error("failed to send pause response: %v", err)
		} else {
			g.logger.Debug("sent pause response, operations paused")
		}
	}
}

func (g *GravityClient) handleResumeRequest(msgID string, request *pb.ResumeRequest) {
	g.logger.Debug("received resume request: reason=%s", request.Reason)

	// Resume the hadron client operations
	// TODO: we need to implement pause from gravity
	g.logger.Debug("resuming hadron client operations: %s", request.Reason)

	// For now, just acknowledge the resume
	response := &pb.ProtocolResponse{
		Id:      msgID,
		Event:   "resume",
		Success: true,
	}

	responseMsg := &pb.ControlMessage{
		Id: msgID,
		MessageType: &pb.ControlMessage_Response{
			Response: response,
		},
	}

	if len(g.streamManager.controlStreams) > 0 && g.streamManager.controlStreams[0] != nil {
		err := g.streamManager.controlStreams[0].Send(responseMsg)
		if err != nil {
			g.logger.Error("failed to send resume response: %v", err)
		} else {
			g.logger.Debug("sent resume response, operations resumed")
		}
	}
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

func (g *GravityClient) stop() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.closing {
		return nil
	}

	g.closing = true
	g.cancel()

	// Update connection status
	g.serverMetrics.UpdateConnection(false)

	g.cleanup()
	g.connected = false

	return nil
}

// Close will shutdown the client
func (g *GravityClient) Close() error {
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
	defer g.mu.Unlock()

	if g.closing {
		g.logger.Debug("already closing, ignoring disconnection event")
		return
	}

	if g.reconnecting {
		g.logger.Debug("reconnection already in progress, ignoring additional disconnection event: %s", reason)
		return
	}

	wasConnected := g.connected

	g.logger.Info("handling server disconnection: %s", reason)

	// Mark as disconnected but don't close completely
	g.connected = false

	if g.networkInterface != nil {
		if err := g.networkInterface.UnrouteTraffic(); err != nil {
			g.logger.Error("failed to unroute traffic: %v", err)
		}
	} else {
		g.logger.Debug("no tunInterface present, skipping traffic unrouting")
	}

	// Clean up current connections without full shutdown
	g.disconnectStreams()

	// Update connection status
	g.serverMetrics.UpdateConnection(false)

	if wasConnected && g.skipAutoReconnect {
		g.logger.Debug("client is configured to skip auto-reconnect")
		g.closed <- struct{}{}
		return
	}

	g.reconnecting = true

	// Start reconnection process in background
	go g.attemptReconnection(reason)
}

// disconnectStreams closes all streams but keeps the client ready for reconnection
func (g *GravityClient) disconnectStreams() {
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

// attemptReconnection attempts to reconnect to gravity servers with backoff
func (g *GravityClient) attemptReconnection(reason string) {
	defer func() {
		g.mu.Lock()
		g.reconnecting = false
		g.mu.Unlock()
	}()

	g.logger.Info("starting reconnection attempts due to: %s", reason)

	// Use exponential backoff for reconnection attempts
	backoff := time.Second
	maxBackoff := 30 * time.Second
	attempts := 0

	for !g.closing {
		attempts++
		g.logger.Info("reconnection attempt %d (backoff: %v)", attempts, backoff)

		// Try to reconnect
		if err := g.reconnect(); err != nil {
			g.logger.Error("reconnection attempt %d failed: %v", attempts, err)

			// Wait before next attempt
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

// reconnect resets connection state and attempts to start a new connection
func (g *GravityClient) reconnect() error {
	// Reset connection state without holding lock during Start()
	g.mu.Lock()
	g.connected = false
	g.closing = false // Reset closing flag to allow reconnection

	// Clear connection IDs from previous connection
	g.connectionIDs = make([]string, 0, g.poolConfig.PoolSize)

	// Reset connections slice to avoid conflicts in Start()
	g.connections = nil

	// Reset stream manager state
	g.streamManager.tunnelStreams = nil
	g.streamManager.controlStreams = nil
	g.streamManager.contexts = nil
	g.streamManager.cancels = nil

	// Drain the connection ID channel to avoid stale data
	select {
	case <-g.connectionIDChan:
		g.logger.Debug("drained stale connection ID from channel during reconnect")
	default:
		// Channel already empty
	}
	g.mu.Unlock()

	// Log deployment synchronization intent
	deploymentCount := len(g.provider.Resources())
	if deploymentCount > 0 {
		g.logger.Info("connection state reset, attempting to start new connection and synchronize %d existing deployments", deploymentCount)
	} else {
		g.logger.Info("connection state reset, attempting to start new connection")
	}

	// Start new connection
	return g.Start()
}

// GetServerMetrics returns current server metrics with performance data
func (g *GravityClient) GetServerMetrics(reset bool) *pb.ServerMetrics {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// Update system metrics
	g.serverMetrics.UpdateSystemMetrics()

	// Update performance metrics from collector

	// Update gRPC-specific metrics
	grpcMetrics := g.getGRPCMetrics()
	g.serverMetrics.UpdateGRPCMetrics(grpcMetrics)

	// Add historical sample
	g.serverMetrics.AddHistoricalSample()

	res := g.serverMetrics.GetSnapshot()

	if reset {
		g.serverMetrics.Reset()
	}

	return res
}

// getGRPCMetrics collects current gRPC connection and stream metrics
func (g *GravityClient) getGRPCMetrics() *GRPCConnectionMetrics {
	metrics := &GRPCConnectionMetrics{
		PoolSize:              int32(len(g.connections)),
		ActiveConnections:     int32(len(g.connections)), // Simplified - all are considered active
		IdleConnections:       0,
		FailedConnections:     0,
		ConnectionStates:      make([]string, len(g.connections)),
		ProtocolVersion:       "HTTP/2",
		CompressionEnabled:    true,
		TlsVersion:            "TLS 1.3", // Simplified
		LastHealthCheck:       time.Now().UnixMilli(),
		HealthCheckIntervalNs: int64(g.poolConfig.HealthCheckInterval),
		StreamAllocation:      g.getStreamAllocationStrategyName(),
	}

	// Get stream information
	if g.streamManager != nil {
		g.streamManager.tunnelMu.RLock()
		metrics.TotalStreams = int32(len(g.streamManager.tunnelStreams))
		healthyCount := 0
		activeCount := 0
		unhealthyStreams := make([]string, 0)

		for _, streamInfo := range g.streamManager.tunnelStreams {
			if streamInfo.isHealthy {
				healthyCount++
				if streamInfo.loadCount > 0 {
					activeCount++
				}
			} else {
				unhealthyStreams = append(unhealthyStreams, streamInfo.streamID)
			}
		}

		metrics.HealthyStreams = int32(healthyCount)
		metrics.ActiveStreams = int32(activeCount)
		metrics.TunnelStreams = int32(len(g.streamManager.tunnelStreams))
		metrics.UnhealthyStreams = unhealthyStreams
		g.streamManager.tunnelMu.RUnlock()

		g.streamManager.controlMu.RLock()
		metrics.ControlStreams = int32(len(g.streamManager.controlStreams))
		g.streamManager.controlMu.RUnlock()
	}

	// Set connection states (simplified)
	for i := range metrics.ConnectionStates {
		if i < len(g.circuitBreakers) {
			cbState := g.circuitBreakers[i].State()
			if cbState == StateClosed {
				metrics.ConnectionStates[i] = "HEALTHY"
			} else {
				metrics.ConnectionStates[i] = cbState.String()
			}
		} else {
			metrics.ConnectionStates[i] = "HEALTHY"
		}
	}

	return metrics
}

// getStreamAllocationStrategyName returns the name of the current allocation strategy
func (g *GravityClient) getStreamAllocationStrategyName() string {
	switch g.poolConfig.AllocationStrategy {
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

// GetHealthScore returns the overall health score (0-100)
func (g *GravityClient) GetHealthScore() float64 {
	return g.serverMetrics.GetHealthScore()
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

	g.logger.Debug("parsed gRPC URL: input=%s, host=%s", inputURL, u.Host)
	return u.Host, nil
}

// extractHostnameFromURL extracts the hostname (without port) from a gRPC URL for TLS ServerName
func (g *GravityClient) extractHostnameFromURL(inputURL string) (string, error) {
	hostname, err := extractHostnameFromGravityURL(inputURL)
	if err != nil {
		return "", err
	}
	g.logger.Debug("extracted hostname for TLS ServerName: %s", hostname)
	return hostname, nil
}

func (g *GravityClient) cleanup() {
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

// sendControlMessageAsync sends a control message without waiting for response (async)
func (g *GravityClient) sendControlMessageAsync(msg *pb.ControlMessage) error {
	if len(g.streamManager.controlStreams) == 0 {
		return fmt.Errorf("no control streams available")
	}

	stream := g.streamManager.controlStreams[0] // Use primary control stream
	if stream == nil {
		return fmt.Errorf("control stream is nil")
	}

	circuitBreaker := g.circuitBreakers[0]

	ctx, cancel := context.WithTimeout(context.WithoutCancel(g.ctx), 10*time.Second)
	defer cancel()

	return RetryWithCircuitBreaker(ctx, g.retryConfig, circuitBreaker, func() error {
		return stream.Send(msg)
	})
}

// SendRouteDeploymentRequest sends a route deployment request and waits for response (sync)
func (g *GravityClient) SendRouteDeploymentRequest(deploymentID, hostname, virtualIP string, timeout time.Duration) (*pb.RouteDeploymentResponse, error) {
	msgID := generateMessageID()

	// Create response channel
	responseChan := make(chan *pb.RouteDeploymentResponse, 1)

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
	msg := &pb.ControlMessage{
		Id: msgID,
		MessageType: &pb.ControlMessage_RouteDeployment{
			RouteDeployment: &pb.RouteDeploymentRequest{
				DeploymentId: deploymentID,
				Hostname:     hostname,
				VirtualIp:    virtualIP,
			},
		},
	}

	if err := g.sendControlMessageAsync(msg); err != nil {
		return nil, fmt.Errorf("failed to send route deployment request: %w", err)
	}

	// Wait for response with timeout
	select {
	case response := <-responseChan:
		return response, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for route deployment response")
	case <-g.ctx.Done():
		return nil, fmt.Errorf("context cancelled while waiting for route deployment response")
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

// Unprovision sends an unprovision request to the gravity server
func (g *GravityClient) Unprovision(deploymentID string) error {
	req := &pb.UnprovisionRequest{
		DeploymentId: deploymentID,
	}

	msgID := generateMessageID()
	controlMsg := &pb.ControlMessage{
		Id: msgID,
		MessageType: &pb.ControlMessage_Unprovision{
			Unprovision: req,
		},
	}

	// Send asynchronously on primary control stream
	return g.sendControlMessageAsync(controlMsg)
}

// Pause sends a pause event to the gravity server
func (g *GravityClient) Pause(reason string) error {
	controlMsg := &pb.ControlMessage{
		Id: generateMessageID(),
		MessageType: &pb.ControlMessage_Pause{
			Pause: &pb.PauseRequest{Reason: reason},
		},
	}

	return g.sendControlMessageAsync(controlMsg)
}

// Resume sends a resume event to the gravity server
func (g *GravityClient) Resume(reason string) error {
	controlMsg := &pb.ControlMessage{
		Id: generateMessageID(),
		MessageType: &pb.ControlMessage_Resume{
			Resume: &pb.ResumeRequest{
				Reason: reason,
			},
		},
	}

	return g.sendControlMessageAsync(controlMsg)
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

	// Select optimal stream using load balancing
	stream := g.streamManager.selectOptimalTunnelStream()
	if stream == nil {
		g.logger.Error("writePacket failed: no healthy tunnel streams available")
		return fmt.Errorf("no healthy tunnel streams available")
	}
	if g.tracePackets {
		g.tracePacketLogger.Debug("writePacket selected stream: %s", stream.streamID)
	}

	// Ensure load count is decremented in all exit paths
	defer g.streamManager.releaseStream(stream)

	tunnelPacket := &pb.TunnelPacket{
		Data:     payload,
		StreamId: stream.streamID,
	}

	err := stream.stream.Send(tunnelPacket)
	if err != nil {
		if errors.Is(err, io.EOF) {
			g.logger.Debug("gravity server closed, exiting")
			return nil
		}
		g.logger.Error("writePacket stream.Send failed: %v", err)
	} else {
		if g.tracePackets {
			g.tracePacketLogger.Debug("writePacket stream.Send succeeded for stream %s", stream.streamID)
		}
	}

	// Metrics recording removed

	return err
}

// Background packet handlers

func (g *GravityClient) handleInboundPackets() {
	for {
		select {
		case <-g.ctx.Done():
			return
		case packet := <-g.inboundPackets:
			// Forward to provider for local processing
			g.provider.ProcessInPacket(packet.Buffer[:packet.Length])
			g.returnBuffer(packet)
		}
	}
}

func (g *GravityClient) handleOutboundPackets() {
	for {
		select {
		case <-g.ctx.Done():
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
	g.streamManager.tunnelMu.Lock()
	defer g.streamManager.tunnelMu.Unlock()

	if len(g.streamManager.tunnelStreams) == 0 {
		return fmt.Errorf("no tunnel streams available")
	}

	// Select optimal stream using configured strategy
	streamIndex, err := g.selectOptimalStream(data)
	if err != nil {
		return fmt.Errorf("failed to select stream: %w", err)
	}

	streamInfo := g.streamManager.tunnelStreams[streamIndex]
	if !streamInfo.isHealthy {
		// Try to find an alternative healthy stream
		streamIndex, err = g.selectHealthyStream()
		if err != nil {
			return fmt.Errorf("no healthy streams available: %w", err)
		}
		streamInfo = g.streamManager.tunnelStreams[streamIndex]
	}

	packet := &pb.TunnelPacket{
		Data:     data,
		StreamId: streamInfo.streamID,
	}

	// Update stream metrics
	streamInfo.loadCount++
	streamInfo.lastUsed = time.Now()

	// Track metrics
	g.streamManager.metricsMu.Lock()
	if metrics := g.streamManager.streamMetrics[streamInfo.streamID]; metrics != nil {
		metrics.PacketsSent++
	}
	g.streamManager.metricsMu.Unlock()

	// Send packet with retry logic and circuit breaker
	connectionIndex := streamInfo.connIndex
	circuitBreaker := g.circuitBreakers[connectionIndex]

	// Record packet transmission attempt

	err = RetryWithCircuitBreaker(context.WithoutCancel(g.ctx), g.retryConfig, circuitBreaker, func() error {
		return streamInfo.stream.Send(packet)
	})

	// Record latency

	if err != nil {
		// Mark stream as unhealthy on error
		streamInfo.isHealthy = false
		g.streamManager.metricsMu.Lock()
		if metrics := g.streamManager.streamMetrics[streamInfo.streamID]; metrics != nil {
			metrics.ErrorCount++
			metrics.LastError = time.Now()
		}
		g.streamManager.metricsMu.Unlock()

		// Record error
		return fmt.Errorf("failed to send packet after retries: %w", err)
	}

	// Decrement load count after successful send
	streamInfo.loadCount--
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
	streamIndex := g.streamManager.nextTunnelIndex % len(g.streamManager.tunnelStreams)
	g.streamManager.nextTunnelIndex++
	return streamIndex, nil
}

// selectHashBasedStream uses consistent hashing for packet distribution
func (g *GravityClient) selectHashBasedStream(data []byte) (int, error) {
	// Use simple hash of packet data for consistent routing
	hash := simpleHashBytes(data)
	streamIndex := hash % len(g.streamManager.tunnelStreams)
	return streamIndex, nil
}

// selectLeastConnectionsStream chooses the stream with the lowest current load
func (g *GravityClient) selectLeastConnectionsStream() (int, error) {
	minLoad := int64(^uint64(0) >> 1) // Max int64
	selectedIndex := 0

	for i, streamInfo := range g.streamManager.tunnelStreams {
		if streamInfo.isHealthy && streamInfo.loadCount < minLoad {
			minLoad = streamInfo.loadCount
			selectedIndex = i
		}
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
		if streamInfo.isHealthy && g.streamManager.connectionHealth[streamInfo.connIndex] {
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
		if streamInfo.isHealthy {
			return i, nil
		}
	}
	return 0, fmt.Errorf("no healthy streams available")
}

// simpleHashBytes computes a simple hash of byte data for consistent routing
func simpleHashBytes(data []byte) int {
	hash := 0
	for i, b := range data {
		if i >= 16 { // Hash first 16 bytes for performance
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
	ticker := time.NewTicker(g.poolConfig.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-g.ctx.Done():
			return
		case <-ticker.C:
			g.performHealthCheck()
		}
	}
}

// performHealthCheck checks the health of all connections and streams
func (g *GravityClient) performHealthCheck() {
	g.streamManager.healthMu.Lock()
	defer g.streamManager.healthMu.Unlock()

	// Check connection health
	for i, conn := range g.connections {
		if conn != nil {
			state := conn.GetState()
			isHealthy := state.String() == "READY" || state.String() == "CONNECTING"
			g.streamManager.connectionHealth[i] = isHealthy

			if !isHealthy {
				g.logger.Debug("connection %d is unhealthy, state: %s", i, state.String())
			}
		} else {
			g.streamManager.connectionHealth[i] = false
		}
	}

	// Check stream health and reset unhealthy streams that haven't been used recently
	g.streamManager.tunnelMu.Lock()
	defer g.streamManager.tunnelMu.Unlock()

	now := time.Now()
	for i, streamInfo := range g.streamManager.tunnelStreams {
		if streamInfo != nil {
			// Reset streams that have been unhealthy for too long
			if !streamInfo.isHealthy && now.Sub(streamInfo.lastUsed) > g.poolConfig.FailoverTimeout {
				g.logger.Debug("attempting to recover stream %s", streamInfo.streamID)
				streamInfo.isHealthy = true // Try again
				streamInfo.loadCount = 0    // Reset load

				// Reset error metrics
				g.streamManager.metricsMu.Lock()
				if metrics := g.streamManager.streamMetrics[streamInfo.streamID]; metrics != nil {
					metrics.ErrorCount = 0
				}
				g.streamManager.metricsMu.Unlock()
			}

			// Log unhealthy streams
			if !streamInfo.isHealthy {
				g.logger.Debug("stream %s (index %d) is unhealthy", streamInfo.streamID, i)
			}
		}
	}
}

// GetConnectionPoolStats returns current connection pool statistics for monitoring
func (g *GravityClient) GetConnectionPoolStats() map[string]any {
	g.streamManager.healthMu.RLock()
	g.streamManager.tunnelMu.RLock()
	g.streamManager.metricsMu.RLock()
	defer g.streamManager.healthMu.RUnlock()
	defer g.streamManager.tunnelMu.RUnlock()
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
		"stream_metrics":         g.streamManager.streamMetrics,
	}

	return stats
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
	for {
		select {
		case <-g.ctx.Done():
			return
		case msg := <-g.textMessages:
			// Process text messages
			g.returnBuffer(msg)
		}
	}
}

// Additional helper functions

// extractHostnameFromGravityURL extracts hostname from URL for TLS configuration
func extractHostnameFromGravityURL(inputURL string) (string, error) {
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
		hostname = "gravity.agentuity.com" // in case we provided a hardcoded ip
	}

	return hostname, nil
}

func createTLSConfig(certPEM, keyPEM, caCertPEM string) (*tls.Config, error) {
	// Load client certificate

	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, fmt.Errorf("failed to load client certificate: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM([]byte(caCertPEM)) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	return &tls.Config{
		Certificates:     []tls.Certificate{cert},
		RootCAs:          caCertPool,
		MinVersion:       tls.VersionTLS13,
		CurvePreferences: []tls.CurveID{tls.X25519, tls.X25519MLKEM768, tls.CurveP256},
		NextProtos:       []string{"h2", "http/1.1"}, // Prefer HTTP/2
	}, nil
}

func getHostInfo(workingDir, ip4Address, ip6Address, instanceID string) (*pb.HostInfo, error) {
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
	diskBytes := getDiskFreeSpace(workingDir)

	return &pb.HostInfo{
		Started:     uint64(time.Now().UnixMilli()),
		Cpu:         uint32(cpuCount),
		Memory:      memoryBytes,
		Disk:        diskBytes,
		Ipv4Address: ip4Address,
		Ipv6Address: ip6Address,
		Hostname:    hostname,
		InstanceId:  instanceID,
	}, nil
}

func (g *GravityClient) handleReportDelivery() {
	ticker := time.NewTicker(g.reportInterval)
	defer ticker.Stop()
	g.sendReport()
	for {
		select {
		case <-g.ctx.Done():
			return
		case <-ticker.C:
			g.logger.Debug("sending report to gravity server")
			g.sendReport()
		}
	}
}

// sendReport sends a report message to the server
func (g *GravityClient) sendReport() {
	g.streamManager.controlMu.RLock()
	if len(g.streamManager.controlStreams) == 0 || g.streamManager.controlStreams[0] == nil {
		g.streamManager.controlMu.RUnlock()
		g.logger.Debug("no control streams available for ping")
		return
	}
	controlStream := g.streamManager.controlStreams[0]
	g.streamManager.controlMu.RUnlock()

	started := time.Now()
	reportID := generateMessageID()

	reportMsg := &pb.ControlMessage{
		Id: reportID,
		MessageType: &pb.ControlMessage_Report{
			Report: &pb.ReportRequest{
				Metrics: g.GetServerMetrics(true),
			},
		},
	}

	err := controlStream.Send(reportMsg)
	if err != nil {
		g.logger.Error("failed to send report: %v", err)
		return
	}

	g.logger.Debug("sent report message: id=%s, took=%v", reportID, time.Since(started))
}

// handlePingHeartbeat sends periodic ping messages to maintain connection health
func (g *GravityClient) handlePingHeartbeat() {
	if g.pingInterval <= 0 {
		g.logger.Debug("ping interval is disabled (zero or negative), skipping heartbeat")
		return
	}

	ticker := time.NewTicker(g.pingInterval)
	defer ticker.Stop()

	g.logger.Debug("starting heartbeat ping ticker with interval: %v", g.pingInterval)

	for {
		select {
		case <-g.ctx.Done():
			g.logger.Debug("ping heartbeat goroutine stopped")
			return
		case <-ticker.C:
			g.sendPing()
		}
	}
}

// sendPing sends a ping message to the server
func (g *GravityClient) sendPing() {
	g.streamManager.controlMu.RLock()
	if len(g.streamManager.controlStreams) == 0 || g.streamManager.controlStreams[0] == nil {
		g.streamManager.controlMu.RUnlock()
		g.logger.Debug("no control streams available for ping")
		return
	}
	controlStream := g.streamManager.controlStreams[0]
	g.streamManager.controlMu.RUnlock()

	started := time.Now()
	pingID := generateMessageID()

	pingMsg := &pb.ControlMessage{
		Id: pingID,
		MessageType: &pb.ControlMessage_Ping{
			Ping: &pb.PingRequest{
				Timestamp: timestamppb.New(started),
			},
		},
	}

	err := controlStream.Send(pingMsg)
	if err != nil {
		g.logger.Error("failed to send ping: %v", err)
		return
	}

	g.logger.Debug("sent ping message: id=%s, took=%v", pingID, time.Since(started))
}

// Buffer management

func (g *GravityClient) getBuffer(payload []byte) *PooledBuffer {
	buf := g.bufferPool.Get().([]byte)
	if len(payload) > len(buf) {
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
// This is used by HTTP API server for provision requests
func (g *GravityClient) GetDeploymentMetadata(ctx context.Context, deploymentID, orgID string) (*pb.DeploymentMetadataResponse, error) {
	if len(g.controlClients) == 0 {
		return nil, fmt.Errorf("no gRPC control clients available")
	}

	metadataRequest := &pb.DeploymentMetadataRequest{
		DeploymentId: deploymentID,
		OrgId:        orgID,
	}

	// Add authorization metadata for authentication
	md := metadata.Pairs("authorization", "Bearer "+g.authorizationToken)
	authCtx := metadata.NewOutgoingContext(ctx, md)

	return g.controlClients[0].GetDeploymentMetadata(authCtx, metadataRequest)
}

// GetTLSConfig returns the TLS configuration for external use (like HTTP server)
func (g *GravityClient) GetTLSConfig() *tls.Config {
	return g.tlsConfig
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
