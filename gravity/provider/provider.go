package provider

import (
	"context"
	"time"

	pb "github.com/agentuity/go-common/gravity/proto"
	"github.com/agentuity/go-common/logger"
)

// Resource represents a provisioned resource
type Resource struct {
	ID        string `json:"-"`
	Hostname  string `json:"-"`
	IPAddress string `json:"-"`
	// Hostport is the hostname and port of the resource.
	// Usually it is [ip]:[port]
	Hostport       string             `json:"hostport"`
	Paused         bool               `json:"-"`
	PausedDuration time.Duration      `json:"-"`
	Errored        bool               `json:"-"`
	Spec           *pb.DeploymentSpec `json:"-"`
	IPV6Address    string             `json:"-"`
	Started        time.Time          `json:"-"`
}

// Server interface for gravity server communication
type Server interface {
	// Unprovision is called to inform the server that we are unprovisioning the deployment
	Unprovision(deploymentID string) error
	// Pause is called to tell the server to pause sending provisioned events
	Pause(reason string) error
	// Resume is called to tell the server to resume sending provisioned events
	Resume(reason string) error

	// Write a packet to the gravity server
	WritePacket(payload []byte) error
}

// Configuration for provider setup
type Configuration struct {
	// Server is the gravity server that should be used
	Server Server
	// Context is the context that should be used for telemetry
	Context context.Context
	// Logger is the logger that should be used for telemetry
	Logger logger.Logger
	// APIURL is the url of the api server
	APIURL string
	// TelemetryURL is the url of the telemetry server
	TelemetryURL string
	// TelemetryAPIKey is the API key for the telemetry server (if needed)
	TelemetryAPIKey string
	// GravityURL is the url of the gravity server
	GravityURL string
	// AgentuityCACert is the ca cert of the gravity server
	AgentuityCACert string
	// HostMapping is the host mapping for the provider
	HostMapping []*pb.HostMapping
	// Environment is the environment for the provider
	Environment []string
	// SubnetRoutes
	SubnetRoutes []string
	// Hostname if the client requested a dynamic hostname
	Hostname string
}

// ProvisionRequest for new deployment
type ProvisionRequest struct {
	DeploymentId string             `json:"deployment_id,omitempty"` // Unique identifier for the deployment
	Spec         *pb.DeploymentSpec `json:"spec,omitempty"`          // Container and runtime specification
	AuthToken    string             `json:"auth_token,omitempty"`    // Authentication token for this deployment
	OtlpToken    string             `json:"otlp_token,omitempty"`    // OpenTelemetry token for metrics
}

// DeprovisionReason specifies why a resource is being deprovisioned
type DeprovisionReason string

const (
	DeprovisionReasonIdleTimeout DeprovisionReason = "idle_timeout"
	DeprovisionReasonError       DeprovisionReason = "error"
	DeprovisionReasonExited      DeprovisionReason = "exit"
	DeprovisionReasonShutdown    DeprovisionReason = "shutdown"
	DeprovisionReasonUnprovision DeprovisionReason = "unprovision"
)

// ProjectRuntimeStatsCollector interface for collecting project runtime statistics
type ProjectRuntimeStatsCollector interface {
	UpdateRuntimeStats(deploymentID string, stats interface{})
	RemoveRuntimeStats(deploymentID string)
	PauseRuntimeStats(deploymentID string)
	UnpauseRuntimeStats(deploymentID string)
}

// Provider interface defines the minimal set of methods required by the gravity client
type Provider interface {
	// Configure will be called to configure the provider with the given configuration
	Configure(config Configuration) error

	// Provision provisions a resource for a given spec
	Provision(ctx context.Context, request *ProvisionRequest) (*Resource, error)

	// Deprovision deprovisions a provisioned resource
	Deprovision(ctx context.Context, resourceID string, reason DeprovisionReason) error

	// Resources returns a list of all resources regardless of state
	Resources() []*Resource

	// SetMetricsCollector sets the metrics collector for runtime stats collection
	SetMetricsCollector(collector ProjectRuntimeStatsCollector)

	// ProcessInPacket processes an inbound packet from the gravity server
	ProcessInPacket(payload []byte)
}
