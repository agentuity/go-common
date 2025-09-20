package authentication

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentuity/go-common/project"
)

func TestAgentAuthenticationMiddleware_BearerAuth_Fixed(t *testing.T) {
	// Setup test agents
	agents := []project.AgentConfig{
		{
			ID:   "agent_bearer_test",
			Name: "Bearer Test Agent",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeBearer,
				Fields: map[string]any{
					"token": "secret_bearer_token",
				},
			},
		},
	}

	// Mock lookup function
	lookup := func(key string) (string, bool) {
		secrets := map[string]string{
			"AGENTUITY_BEARER_TEST_BEARER_TOKEN": "secret_bearer_token",
		}
		val, exists := secrets[key]
		return val, exists
	}

	// Create a test handler that tracks if it was called
	handlerCalled := false
	testHandler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}

	middleware := AgentAuthenticationMiddleware(agents, lookup, testHandler)

	tests := []struct {
		name           string
		path           string
		headers        map[string]string
		expectedStatus int
		expectedCalled bool
	}{
		{
			name: "valid bearer token for API endpoint",
			path: "/api/agent_bearer_test",
			headers: map[string]string{
				"Authorization": "Bearer secret_bearer_token",
			},
			expectedStatus: http.StatusOK,
			expectedCalled: true,
		},
		{
			name: "valid bearer token for SMS endpoint",
			path: "/sms/agent_bearer_test",
			headers: map[string]string{
				"Authorization": "Bearer secret_bearer_token",
			},
			expectedStatus: http.StatusOK,
			expectedCalled: true,
		},
		{
			name: "valid bearer token for webhook endpoint",
			path: "/webhook/agent_bearer_test",
			headers: map[string]string{
				"Authorization": "Bearer secret_bearer_token",
			},
			expectedStatus: http.StatusOK,
			expectedCalled: true,
		},
		{
			name: "invalid bearer token",
			path: "/api/agent_bearer_test",
			headers: map[string]string{
				"Authorization": "Bearer wrong_token",
			},
			expectedStatus: http.StatusUnauthorized,
			expectedCalled: false,
		},
		{
			name:           "missing authorization header",
			path:           "/api/agent_bearer_test",
			headers:        map[string]string{},
			expectedStatus: http.StatusUnauthorized,
			expectedCalled: false,
		},
		{
			name: "non-agent path should pass through",
			path: "/api/some-other-endpoint",
			headers: map[string]string{
				"Authorization": "Bearer anything",
			},
			expectedStatus: http.StatusOK,
			expectedCalled: true,
		},
		{
			name:           "root path should pass through",
			path:           "/",
			headers:        map[string]string{},
			expectedStatus: http.StatusOK,
			expectedCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlerCalled = false

			req := httptest.NewRequest("POST", tt.path, nil)
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}

			w := httptest.NewRecorder()
			middleware(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if handlerCalled != tt.expectedCalled {
				t.Errorf("Expected handler called=%v, got %v", tt.expectedCalled, handlerCalled)
			}
		})
	}
}

func TestAgentAuthenticationMiddleware_BasicAuth_Fixed(t *testing.T) {
	agents := []project.AgentConfig{
		{
			ID:   "agent_basic_test",
			Name: "Basic Test Agent",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeBasic,
				Fields: map[string]any{
					"username": "testuser",
					"password": "{{testpass}}",
				},
			},
		},
	}

	lookup := func(key string) (string, bool) {
		if key == "{{testpass}}" {
			return "testpass", true
		}
		secrets := map[string]string{
			"AGENTUITY_BASIC_TEST_BASIC_TOKEN": "testpass",
		}
		val, exists := secrets[key]
		return val, exists
	}

	handlerCalled := false
	testHandler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}

	middleware := AgentAuthenticationMiddleware(agents, lookup, testHandler)

	validCredentials := base64.StdEncoding.EncodeToString([]byte("testuser:testpass"))
	invalidCredentials := base64.StdEncoding.EncodeToString([]byte("testuser:wrongpass"))

	tests := []struct {
		name           string
		path           string
		authHeader     string
		expectedStatus int
		expectedCalled bool
	}{
		{
			name:           "valid basic auth",
			path:           "/api/agent_basic_test",
			authHeader:     "Basic " + validCredentials,
			expectedStatus: http.StatusOK,
			expectedCalled: true,
		},
		{
			name:           "invalid basic auth",
			path:           "/api/agent_basic_test",
			authHeader:     "Basic " + invalidCredentials,
			expectedStatus: http.StatusUnauthorized,
			expectedCalled: false,
		},
		{
			name:           "malformed basic auth",
			path:           "/api/agent_basic_test",
			authHeader:     "Basic invalid_base64!@#",
			expectedStatus: http.StatusUnauthorized,
			expectedCalled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlerCalled = false

			req := httptest.NewRequest("POST", tt.path, nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			w := httptest.NewRecorder()
			middleware(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if handlerCalled != tt.expectedCalled {
				t.Errorf("Expected handler called=%v, got %v", tt.expectedCalled, handlerCalled)
			}
		})
	}
}
