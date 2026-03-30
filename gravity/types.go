package gravity

import (
	"context"
	"crypto/ecdsa"
	"time"

	"github.com/agentuity/go-common/gravity/network"
	pb "github.com/agentuity/go-common/gravity/proto"
	"github.com/agentuity/go-common/gravity/provider"
	"github.com/agentuity/go-common/logger"
)

// Buffer pool constants
const maxBufferSize = 65536 // 64KB max buffer size

// Protocol version
const protocolVersion = 1

// PooledBuffer represents a buffer from the pool
type PooledBuffer struct {
	Buffer []byte
	Length int
}

// GravityConfig contains configuration for the Gravity client
type GravityConfig struct {
	Context  context.Context
	Logger   logger.Logger
	Provider provider.Provider
	URL      string
	// GravityURLs is a list of Gravity server URLs to connect to.
	// If set, this takes precedence over URL.
	// Hadron connects to up to MaxGravityPeers servers from this list.
	GravityURLs          []string
	CACert               string
	IP4Address           string
	IP6Address           string
	ECDSAPrivateKey      *ecdsa.PrivateKey
	InstanceID           string
	Region               string // Region where the instance is located, for display only
	AvailabilityZone     string // Availability zone where the instance is located, for display only
	CloudProvider        string // Type of cloud provider (e.g., aws, gcp, azure), for display only
	ClientVersion        string
	ClientName           string
	Capabilities         *pb.ClientCapabilities
	PingInterval         time.Duration
	WorkingDir           string
	TraceLogPackets      bool
	NetworkInterface     network.NetworkInterface
	ConnectionPoolConfig *ConnectionPoolConfig
	SkipAutoReconnect    bool
	InstanceTags         []string // Tags for display only
	InstanceType         string   // Type of instance (e.g., t2.micro)
	DefaultServerName    string   // Fallback TLS ServerName when connecting via IP address (default: "gravity.agentuity.com")

	// DiscoveryResolveFunc is called periodically to re-resolve the set of
	// available Gravity server URLs. If nil, no re-resolution or cycling occurs.
	// The function should return ALL available URLs (not capped by MaxGravityPeers).
	// The GravityClient handles selection and capping internally.
	DiscoveryResolveFunc func() []string

	// PeerDiscoveryInterval is how often to re-resolve DNS for new Gravity peers.
	// Default: 30 minutes.
	PeerDiscoveryInterval time.Duration

	// PeerCycleInterval is how often to rotate one connection when there are more
	// available Gravities than active connections. Only cycles if DNS returns more
	// IPs than MaxGravityPeers. Default: 2 hours.
	PeerCycleInterval time.Duration

	// MaxReconnectAttempts is the maximum number of reconnection attempts before
	// invoking ReconnectionFailedCallback. Default: 10
	MaxReconnectAttempts int

	// ReconnectAttemptTimeout is the timeout for each individual reconnection
	// attempt. If a single attempt takes longer than this, it is cancelled and
	// the next attempt begins. Default: 2 minutes
	ReconnectAttemptTimeout time.Duration

	// ReconnectionFailedCallback is invoked when all reconnection attempts are
	// exhausted. This is typically used to crash the process so a supervisor
	// (e.g. systemd) can perform a clean restart. If nil, the client simply
	// stops reconnecting.
	ReconnectionFailedCallback func(attempts int, lastErr error)
}
