package gravity

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/agentuity/go-common/crypto"
	pb "github.com/agentuity/go-common/gravity/proto"
	"github.com/agentuity/go-common/gravity/api"
	"github.com/agentuity/go-common/gravity/provider"
	"github.com/cespare/xxhash/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/grpc/metadata"
)

// StartHTTPAPIServer starts the TLS HTTP API server on port 443
func (g *GRPCGravityServer) StartHTTPAPIServer() error {
	if g.httpServer == nil {

		// Create HTTP server with routes
		mux := http.NewServeMux()

		// Register API routes
		g.registerAPIRoutes(mux)

		g.httpServer = &http.Server{
			Addr:         fmt.Sprintf("[%s]:443", g.ip6Address),
			Handler:      mux,
			TLSConfig:    g.tlsConfig,
			ReadTimeout:  time.Minute * 5,
			WriteTimeout: time.Minute * 5,
			IdleTimeout:  60 * time.Second,
		}

		g.logger.Info("Starting HTTPS API server on %s", g.httpServer.Addr)

		// Start server in goroutine
		go func() {
			// For now, log that we would start the server
			g.logger.Info("HTTP API server ready to start once certificates are available")

			if err := g.httpServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
				g.logger.Error("HTTPS API server error: %v", err)
			}
		}()
	}

	return nil
}

// getBufferedNonceChecker returns a concurrency-safe, O(1) rolling nonce checker
// that tracks exactly the last N nonces using a ring buffer.
func getBufferedNonceChecker(bufferSize int) func(string) error {
	if bufferSize < 1 {
		return func(string) error { return nil }
	}
	var (
		mu    sync.Mutex
		idx   int // next insertion index
		count int // number of populated slots (<= bufferSize)
		queue = make([]uint64, bufferSize)
		seen  = make(map[uint64]struct{}, bufferSize)
	)
	return func(nonce string) error {
		hashed := xxhash.Sum64String(nonce)
		mu.Lock()
		defer mu.Unlock()
		if _, ok := seen[hashed]; ok {
			return errors.New("nonce already used")
		}
		// Evict only when buffer is full; otherwise grow until full.
		if count == bufferSize {
			delete(seen, queue[idx])
		} else {
			count++
		}
		queue[idx] = hashed
		seen[hashed] = struct{}{}
		idx = (idx + 1) % bufferSize
		return nil
	}
}

// registerAPIRoutes registers all HTTP API routes
func (g *GRPCGravityServer) registerAPIRoutes(mux *http.ServeMux) {
	// Add request logging middleware
	mux.HandleFunc("/_health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("server", "hadron/"+g.hadronVersion)
		w.Write([]byte("OK"))
	})

	checkNonce := getBufferedNonceChecker(10_000)
	var publicKey *ecdsa.PublicKey
	var publicKeyMu sync.RWMutex
	extractor := otel.GetTextMapPropagator()

	apiMiddleware := func(next func(ctx context.Context, w http.ResponseWriter, r *http.Request, body []byte)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ctx, span := g.tracer.Start(extractor.Extract(r.Context(), propagation.HeaderCarrier(r.Header)), "apiMiddleware")
			defer span.End()

			// Load API public key
			publicKeyMu.RLock()
			getKey := publicKey == nil
			publicKeyMu.RUnlock()
			if getKey {
				publicKeyMu.Lock()
				apiPublicKey, err := api.GetPublicKey(g.context, g.logger, g.apiURL)
				if err != nil {
					g.logger.Error("failed to load API public key: %s", err)
					g.writeErrorResponse(w, http.StatusBadRequest, "Error reading request body")
					return
				}
				publicKey = apiPublicKey
				publicKeyMu.Unlock()
			}

			// Parse JSON request body
			body, err := io.ReadAll(r.Body)
			if err != nil {
				g.logger.Error("error reading provision request body: %v", err)
				g.writeErrorResponse(w, http.StatusBadRequest, "Error reading request body")
				return
			}
			r.Body.Close()
			publicKeyMu.RLock()
			if err := crypto.VerifyHTTPRequest(publicKey, r, body, checkNonce); err != nil {
				publicKeyMu.RUnlock()
				g.logger.Error("error verifying request: %v", err)
				g.writeErrorResponse(w, http.StatusUnauthorized, "Unauthorized")
				return
			}
			publicKeyMu.RUnlock()

			next(ctx, w, r, body)
		}
	}

	// Register provision and deprovision endpoints
	mux.HandleFunc("POST /provision", apiMiddleware(g.handleProvisionHTTP))
	mux.HandleFunc("POST /deprovision", apiMiddleware(g.handleDeprovisionHTTP))
}

// DeprovisionRequest represents the JSON structure for deprovision requests
type DeprovisionRequest struct {
	DeploymentID string `json:"deployment_id"`
}

// DeprovisionResponse represents the JSON response for deprovision requests
type DeprovisionResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// copied from catalyst
type ProjectSpec struct {
	OrgID          string `json:"orgId"`
	ProjectID      string `json:"projectId"`
	DeploymentID   string `json:"deploymentId"`
	CPUUnits       int64  `json:"cpu"`
	MemoryUnits    int64  `json:"memory"`
	Disk           int64  `json:"disk"`
	UsedPrivateKey bool   `json:"usedPrivateKey,omitempty"`
}

// handleProvisionHTTP handles HTTP provision requests similar to handleProvisionRequest
func (g *GRPCGravityServer) handleProvisionHTTP(ctx context.Context, w http.ResponseWriter, r *http.Request, body []byte) {
	ctx, span := g.tracer.Start(ctx, "handleProvisionHTTP")
	defer span.End()
	var spec ProjectSpec
	if err := json.Unmarshal(body, &spec); err != nil {
		g.logger.Error("Error parsing provision request JSON: %v", err)
		g.writeErrorResponse(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	if spec.DeploymentID == "" {
		g.logger.Error("Missing deployment ID in provision request")
		g.writeErrorResponse(w, http.StatusBadRequest, "Missing deployment ID")
		return
	}

	g.logger.Info("Received HTTP provision request: deployment_id=%s", spec.DeploymentID)

	// First, request deployment metadata from gravity server
	orgID := spec.OrgID

	// Use the first gRPC control client to make the RPC call
	if len(g.controlClients) == 0 {
		g.logger.Error("No gRPC control clients available for deployment metadata request")
		g.writeErrorResponse(w, http.StatusInternalServerError, "no gRPC connections available")
		return
	}

	metadataRequest := &pb.DeploymentMetadataRequest{
		DeploymentId: spec.DeploymentID,
		OrgId:        orgID,
	}

	// Add authorization metadata for authentication (same pattern as establishControlStreams)
	md := metadata.Pairs("authorization", "Bearer "+g.secret)
	authCtx := metadata.NewOutgoingContext(ctx, md)

	metadataResponse, err := g.controlClients[0].GetDeploymentMetadata(authCtx, metadataRequest)
	if err != nil {
		g.logger.Error("Failed to get deployment metadata for deployment %s: %v", spec.DeploymentID, err)
		g.writeErrorResponse(w, http.StatusInternalServerError, fmt.Sprintf("failed to get deployment metadata: %v", err))
		return
	}

	if !metadataResponse.Success {
		g.logger.Error("Deployment metadata request failed for deployment %s: %s", spec.DeploymentID, metadataResponse.Error)
		g.writeErrorResponse(w, http.StatusInternalServerError, fmt.Sprintf("deployment metadata failed: %s", metadataResponse.Error))
		return
	}

	// Log out the deployment metadata as requested
	g.logger.Info("Successfully retrieved deployment metadata for deployment %s", spec.DeploymentID)

	// Call provider to provision the deployment (same logic as handleProvisionRequest)
	req := provider.ProvisionRequest{
		DeploymentId: spec.DeploymentID,
		Spec: &pb.DeploymentSpec{
			Id:             spec.DeploymentID,
			SkipPrivateKey: !spec.UsedPrivateKey,
			Resources: &pb.ResourceRequirements{
				CpuLimit:      spec.CPUUnits,
				MemoryLimit:   spec.MemoryUnits,
				CpuRequest:    spec.CPUUnits,
				MemoryRequest: spec.MemoryUnits,
				// TODO: disk
			},
			OrganizationCert: metadataResponse.DeploymentCert,
			Env:              metadataResponse.CodeMetadata.GetEnv(),     // TODO: get from spec!
			Secrets:          metadataResponse.CodeMetadata.GetSecrets(), // TODO: get from spec!
		},
		AuthToken: metadataResponse.AuthToken,
		OtlpToken: metadataResponse.OtlpToken,
	}

	resource, err := g.provider.Provision(ctx, &req)
	if err != nil {
		g.logger.Error("HTTP provision failed for deployment %s: %v", req.DeploymentId, err)
		g.writeErrorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Send route deployment request on control channel and wait for response
	route, err := g.sendRouteDeploymentRequest(resource.ID, metadataResponse.DeploymentCert.GetDnsname(), resource.IPV6Address, time.Minute)
	if err != nil {
		g.logger.Error("Failed to route deployment %s: %v", resource.ID, err)
		g.writeErrorResponse(w, http.StatusInternalServerError, fmt.Sprintf("failed to route deployment: %v", err))
		return
	}

	g.logger.Info("HTTP provision successful for deployment %s: %s", req.DeploymentId, resource.Hostport)

	g.writeJSONResponse(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"hostport": fmt.Sprintf("[%s]", route.Ip),
		},
	})
}

// handleDeprovisionHTTP handles HTTP deprovision requests similar to handleProvisionRequest
func (g *GRPCGravityServer) handleDeprovisionHTTP(ctx context.Context, w http.ResponseWriter, r *http.Request, body []byte) {
	ctx, span := g.tracer.Start(ctx, "handleDeprovisionHTTP")
	defer span.End()
	var req DeprovisionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		g.logger.Error("Error parsing deprovision request JSON: %v", err)
		g.writeErrorResponse(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	g.logger.Info("Received HTTP deprovision request: deployment_id=%s", req.DeploymentID)

	// Call provider to deprovision the deployment

	// Prepare response
	if err := g.provider.Deprovision(ctx, req.DeploymentID, provider.DeprovisionReasonShutdown); err != nil {
		g.logger.Error("HTTP deprovision failed for deployment %s: %v", req.DeploymentID, err)
		g.writeErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	g.logger.Info("HTTP deprovision successful for deployment %s", req.DeploymentID)

	g.writeJSONResponse(w, http.StatusOK, DeprovisionResponse{
		Success: true,
	})
}

type errResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// writeErrorResponse writes a JSON response with the given status code and error message
func (g *GRPCGravityServer) writeErrorResponse(w http.ResponseWriter, statusCode int, errorMsg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("server", "hadron/"+g.hadronVersion)
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(errResponse{
		Success: false,
		Message: errorMsg,
	}); err != nil {
		g.logger.Error("Error encoding JSON response: %v", err)
	}
}

// writeJSONResponse writes a JSON response with the given status code
func (g *GRPCGravityServer) writeJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("server", "hadron/"+g.hadronVersion)
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		g.logger.Error("Error encoding JSON response: %v", err)
	}
}
