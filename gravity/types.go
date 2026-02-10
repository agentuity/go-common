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
	Context              context.Context
	Logger               logger.Logger
	Provider             provider.Provider
	URL                  string
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
	ReportInterval       time.Duration
	WorkingDir           string
	TraceLogPackets      bool
	NetworkInterface     network.NetworkInterface
	ConnectionPoolConfig *ConnectionPoolConfig
	ReportStats          bool
	SkipAutoReconnect    bool
	InstanceTags         []string // Tags for display only
	InstanceType         string   // Type of instance (e.g., t2.micro)
	DefaultServerName    string   // Fallback TLS ServerName when connecting via IP address (default: "gravity.agentuity.com")
}
