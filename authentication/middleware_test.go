package authentication

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentuity/go-common/project"
)

func TestAgentAuthenticationMiddleware_BearerAuth(t *testing.T) {
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

	// Build secrets for lookup
	secrets, err := BuildAgentSecrets(agents)
	if err != nil {
		t.Fatalf("Failed to build secrets: %v", err)
	}

	lookup := func(key string) (string, bool) {
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

func TestAgentAuthenticationMiddleware_BasicAuth(t *testing.T) {
	agents := []project.AgentConfig{
		{
			ID:   "agent_basic_test",
			Name: "Basic Test Agent",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeBasic,
				Fields: map[string]any{
					"username": "testuser",
					"password": "testpass",
				},
			},
		},
	}

	secrets, err := BuildAgentSecrets(agents)
	if err != nil {
		t.Fatalf("Failed to build secrets: %v", err)
	}

	lookup := func(key string) (string, bool) {
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

func TestAgentAuthenticationMiddleware_HeaderAuth(t *testing.T) {
	agents := []project.AgentConfig{
		{
			ID:   "agent_header_test",
			Name: "Header Test Agent",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeHeader,
				Fields: map[string]any{
					"name":  "X-API-Key",
					"value": "header_secret_value",
				},
			},
		},
	}

	secrets, err := BuildAgentSecrets(agents)
	if err != nil {
		t.Fatalf("Failed to build secrets: %v", err)
	}

	lookup := func(key string) (string, bool) {
		val, exists := secrets[key]
		return val, exists
	}

	handlerCalled := false
	testHandler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}

	middleware := AgentAuthenticationMiddleware(agents, lookup, testHandler)

	tests := []struct {
		name           string
		path           string
		headerValue    string
		expectedStatus int
		expectedCalled bool
	}{
		{
			name:           "valid header auth",
			path:           "/webhook/agent_header_test",
			headerValue:    "header_secret_value",
			expectedStatus: http.StatusOK,
			expectedCalled: true,
		},
		{
			name:           "invalid header value",
			path:           "/webhook/agent_header_test",
			headerValue:    "wrong_header_value",
			expectedStatus: http.StatusUnauthorized,
			expectedCalled: false,
		},
		{
			name:           "missing header",
			path:           "/webhook/agent_header_test",
			headerValue:    "",
			expectedStatus: http.StatusUnauthorized,
			expectedCalled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlerCalled = false

			req := httptest.NewRequest("POST", tt.path, nil)
			if tt.headerValue != "" {
				req.Header.Set("X-API-Key", tt.headerValue)
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

func TestAgentAuthenticationMiddleware_ProjectAuth(t *testing.T) {
	agents := []project.AgentConfig{
		{
			ID:   "agent_project_test",
			Name: "Project Test Agent",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeProject,
			},
		},
	}

	// Project auth doesn't use BuildSecrets, so we simulate environment lookup
	projectKey := "global_project_secret_key"
	lookup := func(key string) (string, bool) {
		if key == "AGENTUITY_PROJECT_KEY" {
			return projectKey, true
		}
		return "", false
	}

	handlerCalled := false
	testHandler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}

	middleware := AgentAuthenticationMiddleware(agents, lookup, testHandler)

	tests := []struct {
		name           string
		path           string
		authHeader     string
		expectedStatus int
		expectedCalled bool
	}{
		{
			name:           "valid project auth",
			path:           "/sms/agent_project_test",
			authHeader:     "Bearer " + projectKey,
			expectedStatus: http.StatusOK,
			expectedCalled: true,
		},
		{
			name:           "invalid project key",
			path:           "/sms/agent_project_test",
			authHeader:     "Bearer wrong_project_key",
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

func TestAgentAuthenticationMiddleware_MultipleAgents(t *testing.T) {
	agents := []project.AgentConfig{
		{
			ID:   "agent_bearer_1",
			Name: "Bearer Agent 1",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeBearer,
				Fields: map[string]any{
					"token": "bearer_token_1",
				},
			},
		},
		{
			ID:   "agent_basic_2",
			Name: "Basic Agent 2",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeBasic,
				Fields: map[string]any{
					"username": "user2",
					"password": "pass2",
				},
			},
		},
		{
			ID:   "agent_header_3",
			Name: "Header Agent 3",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeHeader,
				Fields: map[string]any{
					"name":  "X-Agent-Token",
					"value": "header_token_3",
				},
			},
		},
	}

	secrets, err := BuildAgentSecrets(agents)
	if err != nil {
		t.Fatalf("Failed to build secrets: %v", err)
	}

	lookup := func(key string) (string, bool) {
		val, exists := secrets[key]
		return val, exists
	}

	handlerCalled := false
	testHandler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}

	middleware := AgentAuthenticationMiddleware(agents, lookup, testHandler)

	basicCredentials := base64.StdEncoding.EncodeToString([]byte("user2:pass2"))

	tests := []struct {
		name           string
		path           string
		headers        map[string]string
		expectedStatus int
		expectedCalled bool
	}{
		{
			name: "bearer agent 1 success",
			path: "/api/agent_bearer_1",
			headers: map[string]string{
				"Authorization": "Bearer bearer_token_1",
			},
			expectedStatus: http.StatusOK,
			expectedCalled: true,
		},
		{
			name: "basic agent 2 success",
			path: "/sms/agent_basic_2",
			headers: map[string]string{
				"Authorization": "Basic " + basicCredentials,
			},
			expectedStatus: http.StatusOK,
			expectedCalled: true,
		},
		{
			name: "header agent 3 success",
			path: "/webhook/agent_header_3",
			headers: map[string]string{
				"X-Agent-Token": "header_token_3",
			},
			expectedStatus: http.StatusOK,
			expectedCalled: true,
		},
		{
			name: "wrong auth type for bearer agent",
			path: "/api/agent_bearer_1",
			headers: map[string]string{
				"Authorization": "Basic " + basicCredentials,
			},
			expectedStatus: http.StatusUnauthorized,
			expectedCalled: false,
		},
		{
			name: "wrong auth type for basic agent",
			path: "/api/agent_basic_2",
			headers: map[string]string{
				"Authorization": "Bearer bearer_token_1",
			},
			expectedStatus: http.StatusUnauthorized,
			expectedCalled: false,
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

func TestAgentAuthenticationMiddleware_EdgeCases(t *testing.T) {
	agents := []project.AgentConfig{
		{
			ID:   "agent_test_123",
			Name: "Test Agent",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeBearer,
				Fields: map[string]any{
					"token": "test_token",
				},
			},
		},
	}

	secrets, err := BuildAgentSecrets(agents)
	if err != nil {
		t.Fatalf("Failed to build secrets: %v", err)
	}

	lookup := func(key string) (string, bool) {
		val, exists := secrets[key]
		return val, exists
	}

	handlerCalled := false
	testHandler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}

	middleware := AgentAuthenticationMiddleware(agents, lookup, testHandler)

	tests := []struct {
		name           string
		path           string
		expectedStatus int
		expectedCalled bool
	}{
		{
			name:           "unknown agent id - should pass through",
			path:           "/api/agent_unknown_id",
			expectedStatus: http.StatusOK,
			expectedCalled: true,
		},
		{
			name:           "path with agent prefix but not second segment",
			path:           "/agent_test_123",
			expectedStatus: http.StatusOK,
			expectedCalled: true,
		},
		{
			name:           "empty path segments with agent id - should require auth",
			path:           "//agent_test_123",
			expectedStatus: http.StatusUnauthorized, // Should fail auth since agent ID detected
			expectedCalled: false,
		},
		{
			name:           "deep nested path with agent id",
			path:           "/api/v1/agent_test_123/data",
			expectedStatus: http.StatusOK,
			expectedCalled: true,
		},
		{
			name:           "path with query parameters",
			path:           "/api/agent_test_123?param=value",
			expectedStatus: http.StatusUnauthorized, // Should still validate auth
			expectedCalled: false,
		},
		{
			name:           "case sensitive agent id",
			path:           "/api/AGENT_test_123",
			expectedStatus: http.StatusOK, // Not agent_ prefix
			expectedCalled: true,
		},
		{
			name:           "agent prefix without underscore",
			path:           "/api/agent123",
			expectedStatus: http.StatusOK, // Not agent_ prefix
			expectedCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlerCalled = false

			req := httptest.NewRequest("GET", tt.path, nil)
			if tt.expectedStatus == http.StatusUnauthorized {
				// Only set auth header for tests that should fail auth
				req.Header.Set("Authorization", "Bearer wrong_token")
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

func TestAgentAuthenticationMiddleware_HTTPMethods(t *testing.T) {
	agents := []project.AgentConfig{
		{
			ID:   "agent_method_test",
			Name: "Method Test Agent",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeBearer,
				Fields: map[string]any{
					"token": "method_test_token",
				},
			},
		},
	}

	secrets, err := BuildAgentSecrets(agents)
	if err != nil {
		t.Fatalf("Failed to build secrets: %v", err)
	}

	lookup := func(key string) (string, bool) {
		val, exists := secrets[key]
		return val, exists
	}

	handlerCalled := false
	testHandler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}

	middleware := AgentAuthenticationMiddleware(agents, lookup, testHandler)

	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"}

	for _, method := range methods {
		t.Run("method_"+method, func(t *testing.T) {
			handlerCalled = false

			req := httptest.NewRequest(method, "/api/agent_method_test", nil)
			req.Header.Set("Authorization", "Bearer method_test_token")

			w := httptest.NewRecorder()
			middleware(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected status 200 for method %s, got %d", method, w.Code)
			}

			if !handlerCalled {
				t.Errorf("Expected handler to be called for method %s", method)
			}
		})
	}
}

func TestAgentAuthenticationMiddleware_EmptyAgentsList(t *testing.T) {
	agents := []project.AgentConfig{} // Empty list

	lookup := func(key string) (string, bool) {
		return "", false
	}

	handlerCalled := false
	testHandler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}

	middleware := AgentAuthenticationMiddleware(agents, lookup, testHandler)

	tests := []struct {
		name string
		path string
	}{
		{"agent path with empty config", "/api/agent_any_id"},
		{"non-agent path", "/api/some-endpoint"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlerCalled = false

			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()
			middleware(w, req)

			if tt.path == "/api/agent_any_id" {
				// Should pass through since agent not found in config
				if w.Code != http.StatusOK {
					t.Errorf("Expected status 200 for unknown agent, got %d", w.Code)
				}
				if !handlerCalled {
					t.Error("Expected handler to be called for unknown agent")
				}
			} else {
				// Non-agent path should pass through
				if w.Code != http.StatusOK {
					t.Errorf("Expected status 200 for non-agent path, got %d", w.Code)
				}
				if !handlerCalled {
					t.Error("Expected handler to be called for non-agent path")
				}
			}
		})
	}
}

func TestAgentAuthenticationMiddleware_ResponseContent(t *testing.T) {
	agents := []project.AgentConfig{
		{
			ID:   "agent_response_test",
			Name: "Response Test Agent",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeBearer,
				Fields: map[string]any{
					"token": "response_test_token",
				},
			},
		},
	}

	secrets, err := BuildAgentSecrets(agents)
	if err != nil {
		t.Fatalf("Failed to build secrets: %v", err)
	}

	lookup := func(key string) (string, bool) {
		val, exists := secrets[key]
		return val, exists
	}

	testHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"success": true}`))
	}

	middleware := AgentAuthenticationMiddleware(agents, lookup, testHandler)

	t.Run("successful auth preserves response", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/agent_response_test", nil)
		req.Header.Set("Authorization", "Bearer response_test_token")

		w := httptest.NewRecorder()
		middleware(w, req)

		if w.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d", w.Code)
		}

		expectedBody := `{"success": true}`
		if w.Body.String() != expectedBody {
			t.Errorf("Expected body %s, got %s", expectedBody, w.Body.String())
		}

		if w.Header().Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", w.Header().Get("Content-Type"))
		}
	})

	t.Run("failed auth returns unauthorized", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/agent_response_test", nil)
		req.Header.Set("Authorization", "Bearer wrong_token")

		w := httptest.NewRecorder()
		middleware(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", w.Code)
		}

		expectedBody := "Unauthorized\n"
		if w.Body.String() != expectedBody {
			t.Errorf("Expected body %s, got %s", expectedBody, w.Body.String())
		}

		if w.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
			t.Errorf("Expected Content-Type text/plain; charset=utf-8, got %s", w.Header().Get("Content-Type"))
		}
	})
}
