package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// GCEDiscoverer discovers peers by listing GCE instances with a specific tag.
// Uses workload identity for authentication (no explicit credentials).
//
// It calls the GCE compute REST API directly using the metadata server for
// authentication, avoiding the heavy google.golang.org/api dependency.
type GCEDiscoverer struct {
	ProjectID string // GCP project ID
	Zone      string // GCE zone (e.g., "us-central1-a")
	SelfName  string // this instance's name (excluded from results)
	Tag       string // instance tag to filter by (e.g., "gluon-ion")

	// HTTPClient is an optional HTTP client for making API calls.
	// If nil, a default client is used. Useful for testing.
	HTTPClient *http.Client

	// TokenURL overrides the metadata server URL for fetching access tokens.
	// Defaults to the GCE metadata server. Useful for testing.
	TokenURL string

	// BaseURL overrides the Compute API base URL.
	// Defaults to https://compute.googleapis.com. Useful for testing.
	BaseURL string
}

const (
	defaultTokenURL = "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token"
	defaultBaseURL  = "https://compute.googleapis.com"
)

// gceTokenResponse is the response from the GCE metadata token endpoint.
type gceTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

// gceInstanceList is a subset of the GCE instances.list response.
type gceInstanceList struct {
	Items []gceInstance `json:"items"`
}

// gceAggregatedList is a subset of the GCE instances.aggregatedList response.
type gceAggregatedList struct {
	Items map[string]gceInstancesScopedList `json:"items"`
}

// gceInstancesScopedList is the per-zone instance list in an aggregatedList response.
type gceInstancesScopedList struct {
	Instances []gceInstance `json:"instances"`
}

// gceInstance is a subset of the GCE instance resource.
type gceInstance struct {
	Name              string                `json:"name"`
	Status            string                `json:"status"`
	Tags              *gceInstanceTags      `json:"tags"`
	NetworkInterfaces []gceNetworkInterface `json:"networkInterfaces"`
}

// gceInstanceTags represents the tags on a GCE instance.
type gceInstanceTags struct {
	Items []string `json:"items"`
}

// gceNetworkInterface is a subset of the GCE network interface resource.
type gceNetworkInterface struct {
	NetworkIP   string `json:"networkIP"`
	IPv6Address string `json:"ipv6Address"`
}

// Discover lists GCE instances matching the configured tag, excludes self,
// and returns the internal IPs of running instances.
func (g *GCEDiscoverer) Discover(ctx context.Context) ([]string, error) {
	client := g.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	token, err := g.fetchToken(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("discovery: failed to fetch access token: %w", err)
	}

	instances, err := g.listInstances(ctx, client, token)
	if err != nil {
		return nil, fmt.Errorf("discovery: failed to list instances: %w", err)
	}

	var peers []string
	for _, inst := range instances {
		// Skip self.
		if inst.Name == g.SelfName {
			continue
		}
		// Only include running instances.
		if inst.Status != "RUNNING" {
			continue
		}
		// Filter by tag.
		if !g.hasTag(inst) {
			continue
		}
		// Extract the internal IP, preferring IPv6 (memberlist binds to IPv6
		// when available, so peers must be reachable on their IPv6 address).
		for _, iface := range inst.NetworkInterfaces {
			if iface.IPv6Address != "" {
				peers = append(peers, iface.IPv6Address)
				break
			}
			if iface.NetworkIP != "" {
				peers = append(peers, iface.NetworkIP)
				break
			}
		}
	}
	return peers, nil
}

// fetchToken retrieves an access token from the GCE metadata server.
func (g *GCEDiscoverer) fetchToken(ctx context.Context, client *http.Client) (string, error) {
	tokenURL := g.TokenURL
	if tokenURL == "" {
		tokenURL = defaultTokenURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata-Flavor", "Google")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("metadata server returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp gceTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}
	return tokenResp.AccessToken, nil
}

// listInstances calls the GCE compute instances.aggregatedList API to discover
// peers across all zones in the project. This is necessary because instances in
// the same region may be spread across different zones (e.g., us-west1-a, -b, -c).
func (g *GCEDiscoverer) listInstances(ctx context.Context, client *http.Client, token string) ([]gceInstance, error) {
	baseURL := g.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	url := fmt.Sprintf("%s/compute/v1/projects/%s/aggregated/instances", baseURL, g.ProjectID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("compute API returned %d: %s", resp.StatusCode, string(body))
	}

	var list gceAggregatedList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("failed to decode aggregated instance list: %w", err)
	}

	var instances []gceInstance
	for _, scoped := range list.Items {
		instances = append(instances, scoped.Instances...)
	}
	return instances, nil
}

// hasTag reports whether the instance has the configured tag.
func (g *GCEDiscoverer) hasTag(inst gceInstance) bool {
	if inst.Tags == nil {
		return false
	}
	for _, tag := range inst.Tags.Items {
		if tag == g.Tag {
			return true
		}
	}
	return false
}

// Name returns "gce".
func (g *GCEDiscoverer) Name() string {
	return "gce"
}
