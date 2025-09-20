package authentication

import (
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/agentuity/go-common/project"
)

func TestBuildSecrets_BearerAuthentication(t *testing.T) {
	agents := []project.AgentConfig{
		{
			ID:   "agent_bearer_test",
			Name: "Bearer Test Agent",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeBearer,
				Fields: map[string]any{
					"token": "my_secret_bearer_token",
				},
			},
		},
	}

	secrets, err := BuildAgentSecrets(agents)
	if err != nil {
		t.Fatalf("BuildSecrets failed: %v", err)
	}

	expectedKey := "AGENTUITY_BEARER_TEST_BEARER_TOKEN"
	expectedValue := "my_secret_bearer_token"

	if val, exists := secrets[expectedKey]; !exists {
		t.Errorf("Expected key %s not found in secrets", expectedKey)
	} else if val != expectedValue {
		t.Errorf("Expected value %s, got %s", expectedValue, val)
	}
}

func TestBuildSecrets_BasicAuthentication(t *testing.T) {
	agents := []project.AgentConfig{
		{
			ID:   "agent_basic_test",
			Name: "Basic Test Agent",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeBasic,
				Fields: map[string]any{
					"username": "admin",
					"password": "secret123",
				},
			},
		},
	}

	secrets, err := BuildAgentSecrets(agents)
	if err != nil {
		t.Fatalf("BuildSecrets failed: %v", err)
	}

	expectedKey := "AGENTUITY_BASIC_TEST_BASIC_TOKEN"
	expectedValue := "secret123"

	if val, exists := secrets[expectedKey]; !exists {
		t.Errorf("Expected key %s not found in secrets", expectedKey)
	} else if val != expectedValue {
		t.Errorf("Expected value %s, got %s", expectedValue, val)
	}
}

func TestBuildSecrets_HeaderAuthentication(t *testing.T) {
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
		t.Fatalf("BuildSecrets failed: %v", err)
	}

	expectedKey := "AGENTUITY_HEADER_TEST_HEADER_TOKEN"
	expectedValue := "header_secret_value"

	if val, exists := secrets[expectedKey]; !exists {
		t.Errorf("Expected key %s not found in secrets", expectedKey)
	} else if val != expectedValue {
		t.Errorf("Expected value %s, got %s", expectedValue, val)
	}
}

func TestBuildSecrets_ProjectAuthentication(t *testing.T) {
	agents := []project.AgentConfig{
		{
			ID:   "agent_project_test",
			Name: "Project Test Agent",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeProject,
			},
		},
	}

	secrets, err := BuildAgentSecrets(agents)
	if err != nil {
		t.Fatalf("BuildSecrets failed: %v", err)
	}

	// Project authentication should not create any secrets (handled by platform)
	if len(secrets) != 0 {
		t.Errorf("Expected no secrets for project authentication, got %d secrets", len(secrets))
	}
}

func TestBuildSecrets_MultipleAgents(t *testing.T) {
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
					"name":  "X-Token",
					"value": "header_value_3",
				},
			},
		},
		{
			ID:   "agent_project_4",
			Name: "Project Agent 4",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeProject,
			},
		},
	}

	secrets, err := BuildAgentSecrets(agents)
	if err != nil {
		t.Fatalf("BuildSecrets failed: %v", err)
	}

	// Should have 3 secrets (bearer, basic, header - not project)
	expectedCount := 3
	if len(secrets) != expectedCount {
		t.Errorf("Expected %d secrets, got %d", expectedCount, len(secrets))
	}

	// Check bearer token
	if val, exists := secrets["AGENTUITY_BEARER_1_BEARER_TOKEN"]; !exists {
		t.Error("Bearer token secret not found")
	} else if val != "bearer_token_1" {
		t.Errorf("Bearer token value incorrect: %s", val)
	}

	// Check basic token
	expectedBasic := "pass2"
	if val, exists := secrets["AGENTUITY_BASIC_2_BASIC_TOKEN"]; !exists {
		t.Error("Basic token secret not found")
	} else if val != expectedBasic {
		t.Errorf("Basic token value incorrect: %s", val)
	}

	// Check header token
	if val, exists := secrets["AGENTUITY_HEADER_3_HEADER_TOKEN"]; !exists {
		t.Error("Header token secret not found")
	} else if val != "header_value_3" {
		t.Errorf("Header token value incorrect: %s", val)
	}
}

func TestBuildSecrets_MissingFields(t *testing.T) {
	tests := []struct {
		name        string
		agent       project.AgentConfig
		expectedErr string
	}{
		{
			name: "bearer missing token",
			agent: project.AgentConfig{
				ID:   "agent_bearer_no_token",
				Name: "Bearer No Token",
				Authentication: project.Authentication{
					Type:   project.AuthenticationTypeBearer,
					Fields: map[string]any{},
				},
			},
			expectedErr: "missing token field for agent agent_bearer_no_token when using bearer authentication type",
		},
		{
			name: "bearer wrong token type",
			agent: project.AgentConfig{
				ID:   "agent_bearer_wrong_type",
				Name: "Bearer Wrong Type",
				Authentication: project.Authentication{
					Type: project.AuthenticationTypeBearer,
					Fields: map[string]any{
						"token": 123, // should be string
					},
				},
			},
			expectedErr: "missing token field for agent agent_bearer_wrong_type when using bearer authentication type",
		},
		{
			name: "basic missing username",
			agent: project.AgentConfig{
				ID:   "agent_basic_no_user",
				Name: "Basic No User",
				Authentication: project.Authentication{
					Type: project.AuthenticationTypeBasic,
					Fields: map[string]any{
						"password": "secret",
					},
				},
			},
			expectedErr: "missing username field for agent agent_basic_no_user when using basic authentication type",
		},
		{
			name: "basic missing password",
			agent: project.AgentConfig{
				ID:   "agent_basic_no_pass",
				Name: "Basic No Pass",
				Authentication: project.Authentication{
					Type: project.AuthenticationTypeBasic,
					Fields: map[string]any{
						"username": "admin",
					},
				},
			},
			expectedErr: "missing password field for agent agent_basic_no_pass when using basic authentication type",
		},
		{
			name: "header missing name",
			agent: project.AgentConfig{
				ID:   "agent_header_no_name",
				Name: "Header No Name",
				Authentication: project.Authentication{
					Type: project.AuthenticationTypeHeader,
					Fields: map[string]any{
						"value": "token_value",
					},
				},
			},
			expectedErr: "missing name field for agent agent_header_no_name when using header authentication type",
		},
		{
			name: "header missing value",
			agent: project.AgentConfig{
				ID:   "agent_header_no_value",
				Name: "Header No Value",
				Authentication: project.Authentication{
					Type: project.AuthenticationTypeHeader,
					Fields: map[string]any{
						"name": "X-Token",
					},
				},
			},
			expectedErr: "missing value field for agent agent_header_no_value when using header authentication type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agents := []project.AgentConfig{tt.agent}
			_, err := BuildAgentSecrets(agents)
			if err == nil {
				t.Errorf("Expected error but got none")
			} else if err.Error() != tt.expectedErr {
				t.Errorf("Expected error '%s', got '%s'", tt.expectedErr, err.Error())
			}
		})
	}
}

func TestBuildSecrets_EmptyAgents(t *testing.T) {
	agents := []project.AgentConfig{}
	secrets, err := BuildAgentSecrets(agents)
	if err != nil {
		t.Fatalf("BuildSecrets failed: %v", err)
	}

	if len(secrets) != 0 {
		t.Errorf("Expected empty secrets map, got %d entries", len(secrets))
	}
}

// Integration tests that combine BuildSecrets with ValidateAgentAuthentication
func TestBuildSecrets_Integration_Bearer(t *testing.T) {
	agent := project.AgentConfig{
		ID:   "agent_integration_bearer",
		Name: "Integration Bearer Test",
		Authentication: project.Authentication{
			Type: project.AuthenticationTypeBearer,
			Fields: map[string]any{
				"token": "integration_bearer_token",
			},
		},
	}

	// Build secrets
	secrets, err := BuildAgentSecrets([]project.AgentConfig{agent})
	if err != nil {
		t.Fatalf("BuildSecrets failed: %v", err)
	}

	// Create lookup function using the secrets
	lookup := func(key string) (string, bool) {
		val, exists := secrets[key]
		return val, exists
	}

	// Test valid authentication
	headers := http.Header{
		"Authorization": []string{"Bearer integration_bearer_token"},
	}
	if !ValidateAgentAuthentication(agent, lookup, headers) {
		t.Error("Expected authentication to succeed")
	}

	// Test invalid token
	invalidHeaders := http.Header{
		"Authorization": []string{"Bearer wrong_token"},
	}
	if ValidateAgentAuthentication(agent, lookup, invalidHeaders) {
		t.Error("Expected authentication to fail with wrong token")
	}
}

func TestBuildSecrets_Integration_Basic(t *testing.T) {
	agent := project.AgentConfig{
		ID:   "agent_integration_basic",
		Name: "Integration Basic Test",
		Authentication: project.Authentication{
			Type: project.AuthenticationTypeBasic,
			Fields: map[string]any{
				"username": "testuser",
				"password": "testpass",
			},
		},
	}

	// Build secrets
	secrets, err := BuildAgentSecrets([]project.AgentConfig{agent})
	if err != nil {
		t.Fatalf("BuildSecrets failed: %v", err)
	}

	// Create lookup function using the secrets
	lookup := func(key string) (string, bool) {
		val, exists := secrets[key]
		return val, exists
	}

	// Test valid authentication
	credentials := base64.StdEncoding.EncodeToString([]byte("testuser:testpass"))
	headers := http.Header{
		"Authorization": []string{"Basic " + credentials},
	}
	if !ValidateAgentAuthentication(agent, lookup, headers) {
		t.Error("Expected authentication to succeed")
	}

	// Test invalid credentials
	wrongCredentials := base64.StdEncoding.EncodeToString([]byte("testuser:wrongpass"))
	invalidHeaders := http.Header{
		"Authorization": []string{"Basic " + wrongCredentials},
	}
	if ValidateAgentAuthentication(agent, lookup, invalidHeaders) {
		t.Error("Expected authentication to fail with wrong credentials")
	}
}

func TestBuildSecrets_Integration_Header(t *testing.T) {
	agent := project.AgentConfig{
		ID:   "agent_integration_header",
		Name: "Integration Header Test",
		Authentication: project.Authentication{
			Type: project.AuthenticationTypeHeader,
			Fields: map[string]any{
				"name":  "X-Custom-Auth",
				"value": "integration_header_value",
			},
		},
	}

	// Build secrets
	secrets, err := BuildAgentSecrets([]project.AgentConfig{agent})
	if err != nil {
		t.Fatalf("BuildSecrets failed: %v", err)
	}

	// Create lookup function using the secrets
	lookup := func(key string) (string, bool) {
		val, exists := secrets[key]
		return val, exists
	}

	// Test valid authentication
	headers := http.Header{
		"X-Custom-Auth": []string{"integration_header_value"},
	}
	if !ValidateAgentAuthentication(agent, lookup, headers) {
		t.Error("Expected authentication to succeed")
	}

	// Test invalid header value
	invalidHeaders := http.Header{
		"X-Custom-Auth": []string{"wrong_header_value"},
	}
	if ValidateAgentAuthentication(agent, lookup, invalidHeaders) {
		t.Error("Expected authentication to fail with wrong header value")
	}

	// Test missing header
	emptyHeaders := http.Header{}
	if ValidateAgentAuthentication(agent, lookup, emptyHeaders) {
		t.Error("Expected authentication to fail with missing header")
	}
}

func TestBuildSecrets_Integration_Project(t *testing.T) {
	agent := project.AgentConfig{
		ID:   "agent_integration_project",
		Name: "Integration Project Test",
		Authentication: project.Authentication{
			Type: project.AuthenticationTypeProject,
		},
	}

	// Build secrets (should be empty for project type)
	secrets, err := BuildAgentSecrets([]project.AgentConfig{agent})
	if err != nil {
		t.Fatalf("BuildSecrets failed: %v", err)
	}

	// Create lookup function that includes the project key
	projectKey := "global_project_secret_key"
	lookup := func(key string) (string, bool) {
		if key == "AGENTUITY_PROJECT_KEY" {
			return projectKey, true
		}
		val, exists := secrets[key]
		return val, exists
	}

	// Test valid project authentication
	headers := http.Header{
		"Authorization": []string{"Bearer " + projectKey},
	}
	if !ValidateAgentAuthentication(agent, lookup, headers) {
		t.Error("Expected project authentication to succeed")
	}

	// Test invalid project key
	invalidHeaders := http.Header{
		"Authorization": []string{"Bearer wrong_project_key"},
	}
	if ValidateAgentAuthentication(agent, lookup, invalidHeaders) {
		t.Error("Expected project authentication to fail with wrong key")
	}
}

func TestBuildSecrets_Integration_MultipleAgents(t *testing.T) {
	agents := []project.AgentConfig{
		{
			ID:   "agent_multi_bearer",
			Name: "Multi Bearer",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeBearer,
				Fields: map[string]any{
					"token": "multi_bearer_token",
				},
			},
		},
		{
			ID:   "agent_multi_basic",
			Name: "Multi Basic",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeBasic,
				Fields: map[string]any{
					"username": "multiuser",
					"password": "multipass",
				},
			},
		},
	}

	// Build secrets
	secrets, err := BuildAgentSecrets(agents)
	if err != nil {
		t.Fatalf("BuildSecrets failed: %v", err)
	}

	// Create lookup function using the secrets
	lookup := func(key string) (string, bool) {
		val, exists := secrets[key]
		return val, exists
	}

	// Test bearer agent authentication
	bearerHeaders := http.Header{
		"Authorization": []string{"Bearer multi_bearer_token"},
	}
	if !ValidateAgentAuthentication(agents[0], lookup, bearerHeaders) {
		t.Error("Expected bearer agent authentication to succeed")
	}

	// Test basic agent authentication
	basicCredentials := base64.StdEncoding.EncodeToString([]byte("multiuser:multipass"))
	basicHeaders := http.Header{
		"Authorization": []string{"Basic " + basicCredentials},
	}
	if !ValidateAgentAuthentication(agents[1], lookup, basicHeaders) {
		t.Error("Expected basic agent authentication to succeed")
	}

	// Test wrong agent with wrong authentication type
	if ValidateAgentAuthentication(agents[0], lookup, basicHeaders) {
		t.Error("Expected bearer agent to fail with basic auth header")
	}

	if ValidateAgentAuthentication(agents[1], lookup, bearerHeaders) {
		t.Error("Expected basic agent to fail with bearer auth header")
	}
}
