package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time interface compliance checks.
var (
	_ PeerDiscoverer = (*StaticDiscoverer)(nil)
	_ PeerDiscoverer = (*GCEDiscoverer)(nil)
)

func TestStaticDiscoverer(t *testing.T) {
	tests := []struct {
		name     string
		peers    []string
		expected []string
	}{
		{
			name:     "returns configured peers",
			peers:    []string{"10.0.0.1:7946", "10.0.0.2:7946", "10.0.0.3:7946"},
			expected: []string{"10.0.0.1:7946", "10.0.0.2:7946", "10.0.0.3:7946"},
		},
		{
			name:     "returns single peer",
			peers:    []string{"192.168.1.100"},
			expected: []string{"192.168.1.100"},
		},
		{
			name:     "empty list returns empty",
			peers:    []string{},
			expected: []string{},
		},
		{
			name:     "nil list returns nil",
			peers:    nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &StaticDiscoverer{Peers: tt.peers}
			result, err := d.Discover(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStaticDiscoverer_ReturnsCopy(t *testing.T) {
	original := []string{"10.0.0.1", "10.0.0.2"}
	d := &StaticDiscoverer{Peers: original}

	result, err := d.Discover(context.Background())
	require.NoError(t, err)

	// Mutating the result should not affect the original.
	result[0] = "mutated"
	assert.Equal(t, "10.0.0.1", d.Peers[0], "original peers should not be mutated")
}

func TestStaticDiscoverer_Name(t *testing.T) {
	d := &StaticDiscoverer{}
	assert.Equal(t, "static", d.Name())
}

func TestGCEDiscoverer_Name(t *testing.T) {
	d := &GCEDiscoverer{
		ProjectID: "test-project",
		Zone:      "us-central1-a",
		SelfName:  "instance-1",
		Tag:       "gluon-ion",
	}
	assert.Equal(t, "gce", d.Name())
}

func TestGCEDiscoverer_InterfaceCompliance(t *testing.T) {
	var d PeerDiscoverer = &GCEDiscoverer{
		ProjectID: "test-project",
		Zone:      "us-central1-a",
		SelfName:  "instance-1",
		Tag:       "gluon-ion",
	}
	assert.Equal(t, "gce", d.Name())
}

// newFakeGCEServer creates a test HTTP server that serves both the metadata
// token endpoint and the compute instances.aggregatedList endpoint.
func newFakeGCEServer(t *testing.T, instances []gceInstance) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Metadata-Flavor") != "Google" {
			http.Error(w, "missing Metadata-Flavor header", http.StatusBadRequest)
			return
		}
		resp := gceTokenResponse{
			AccessToken: "fake-token-12345",
			TokenType:   "Bearer",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/compute/v1/projects/test-project/aggregated/instances", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fake-token-12345" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// Wrap instances in the aggregatedList response shape.
		list := gceAggregatedList{
			Items: map[string]gceInstancesScopedList{
				"zones/us-central1-a": {Instances: instances},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(list)
	})

	return httptest.NewServer(mux)
}

func TestGCEDiscoverer_Discover(t *testing.T) {
	tests := []struct {
		name      string
		selfName  string
		tag       string
		instances []gceInstance
		expected  []string
	}{
		{
			name:     "discovers peers excluding self",
			selfName: "instance-1",
			tag:      "gluon-ion",
			instances: []gceInstance{
				{
					Name:   "instance-1",
					Status: "RUNNING",
					Tags:   &gceInstanceTags{Items: []string{"gluon-ion"}},
					NetworkInterfaces: []gceNetworkInterface{
						{NetworkIP: "10.128.0.1"},
					},
				},
				{
					Name:   "instance-2",
					Status: "RUNNING",
					Tags:   &gceInstanceTags{Items: []string{"gluon-ion"}},
					NetworkInterfaces: []gceNetworkInterface{
						{NetworkIP: "10.128.0.2"},
					},
				},
				{
					Name:   "instance-3",
					Status: "RUNNING",
					Tags:   &gceInstanceTags{Items: []string{"gluon-ion"}},
					NetworkInterfaces: []gceNetworkInterface{
						{NetworkIP: "10.128.0.3"},
					},
				},
			},
			expected: []string{"10.128.0.2", "10.128.0.3"},
		},
		{
			name:     "filters by tag",
			selfName: "instance-1",
			tag:      "gluon-ion",
			instances: []gceInstance{
				{
					Name:   "instance-2",
					Status: "RUNNING",
					Tags:   &gceInstanceTags{Items: []string{"gluon-ion"}},
					NetworkInterfaces: []gceNetworkInterface{
						{NetworkIP: "10.128.0.2"},
					},
				},
				{
					Name:   "instance-3",
					Status: "RUNNING",
					Tags:   &gceInstanceTags{Items: []string{"other-tag"}},
					NetworkInterfaces: []gceNetworkInterface{
						{NetworkIP: "10.128.0.3"},
					},
				},
			},
			expected: []string{"10.128.0.2"},
		},
		{
			name:     "excludes non-running instances",
			selfName: "instance-1",
			tag:      "gluon-ion",
			instances: []gceInstance{
				{
					Name:   "instance-2",
					Status: "RUNNING",
					Tags:   &gceInstanceTags{Items: []string{"gluon-ion"}},
					NetworkInterfaces: []gceNetworkInterface{
						{NetworkIP: "10.128.0.2"},
					},
				},
				{
					Name:   "instance-3",
					Status: "TERMINATED",
					Tags:   &gceInstanceTags{Items: []string{"gluon-ion"}},
					NetworkInterfaces: []gceNetworkInterface{
						{NetworkIP: "10.128.0.3"},
					},
				},
				{
					Name:   "instance-4",
					Status: "STAGING",
					Tags:   &gceInstanceTags{Items: []string{"gluon-ion"}},
					NetworkInterfaces: []gceNetworkInterface{
						{NetworkIP: "10.128.0.4"},
					},
				},
			},
			expected: []string{"10.128.0.2"},
		},
		{
			name:     "handles instances with no tags",
			selfName: "instance-1",
			tag:      "gluon-ion",
			instances: []gceInstance{
				{
					Name:   "instance-2",
					Status: "RUNNING",
					Tags:   nil,
					NetworkInterfaces: []gceNetworkInterface{
						{NetworkIP: "10.128.0.2"},
					},
				},
			},
			expected: nil,
		},
		{
			name:      "empty instance list",
			selfName:  "instance-1",
			tag:       "gluon-ion",
			instances: []gceInstance{},
			expected:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newFakeGCEServer(t, tt.instances)
			defer srv.Close()

			d := &GCEDiscoverer{
				ProjectID:  "test-project",
				Zone:       "us-central1-a",
				SelfName:   tt.selfName,
				Tag:        tt.tag,
				HTTPClient: srv.Client(),
				TokenURL:   srv.URL + "/token",
				BaseURL:    srv.URL,
			}

			result, err := d.Discover(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGCEDiscoverer_TokenError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "metadata unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	d := &GCEDiscoverer{
		ProjectID:  "test-project",
		Zone:       "us-central1-a",
		SelfName:   "instance-1",
		Tag:        "gluon-ion",
		HTTPClient: srv.Client(),
		TokenURL:   srv.URL + "/token",
		BaseURL:    srv.URL,
	}

	_, err := d.Discover(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch access token")
}

func TestGCEDiscoverer_APIError(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// Token request succeeds.
			resp := gceTokenResponse{AccessToken: "fake-token", TokenType: "Bearer"}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		// Instance list request fails.
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	d := &GCEDiscoverer{
		ProjectID:  "test-project",
		Zone:       "us-central1-a",
		SelfName:   "instance-1",
		Tag:        "gluon-ion",
		HTTPClient: srv.Client(),
		TokenURL:   srv.URL + "/token",
		BaseURL:    srv.URL,
	}

	_, err := d.Discover(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list instances")
}
