package authentication

import (
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/agentuity/go-common/project"
)

func TestBuildAgentSecrets_BearerAuthentication(t *testing.T) {
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

	// Mock lookup function
	lookup := func(key string) (string, bool) {
		return "", false // No interpolation needed
	}

	secrets, err := BuildAgentSecrets(agents, lookup)
	if err != nil {
		t.Fatalf("BuildAgentSecrets failed: %v", err)
	}

	expectedKey := "AGENTUITY_BEARER_TEST_BEARER_TOKEN"
	expectedValue := "my_secret_bearer_token"

	if val, exists := secrets[expectedKey]; !exists {
		t.Errorf("Expected key %s not found in secrets", expectedKey)
	} else if val.Text() != expectedValue {
		t.Errorf("Expected value %s, got %s", expectedValue, val.Text())
	}
}

func TestBuildAgentSecrets_BasicAuthentication(t *testing.T) {
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

	lookup := func(key string) (string, bool) {
		return "", false // No interpolation needed
	}

	secrets, err := BuildAgentSecrets(agents, lookup)
	if err != nil {
		t.Fatalf("BuildAgentSecrets failed: %v", err)
	}

	expectedKey := "AGENTUITY_BASIC_TEST_BASIC_TOKEN"
	expectedValue := "secret123"

	if val, exists := secrets[expectedKey]; !exists {
		t.Errorf("Expected key %s not found in secrets", expectedKey)
	} else if val.Text() != expectedValue {
		t.Errorf("Expected value %s, got %s", expectedValue, val.Text())
	}
}

func TestBuildAgentSecrets_HeaderAuthentication(t *testing.T) {
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

	lookup := func(key string) (string, bool) {
		return "", false // No interpolation needed
	}

	secrets, err := BuildAgentSecrets(agents, lookup)
	if err != nil {
		t.Fatalf("BuildAgentSecrets failed: %v", err)
	}

	expectedKey := "AGENTUITY_HEADER_TEST_HEADER_TOKEN"
	expectedValue := "header_secret_value"

	if val, exists := secrets[expectedKey]; !exists {
		t.Errorf("Expected key %s not found in secrets", expectedKey)
	} else if val.Text() != expectedValue {
		t.Errorf("Expected value %s, got %s", expectedValue, val.Text())
	}
}

func TestBuildAgentSecrets_ProjectAuthentication(t *testing.T) {
	agents := []project.AgentConfig{
		{
			ID:   "agent_project_test",
			Name: "Project Test Agent",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeProject,
			},
		},
	}

	lookup := func(key string) (string, bool) {
		return "", false
	}

	secrets, err := BuildAgentSecrets(agents, lookup)
	if err != nil {
		t.Fatalf("BuildAgentSecrets failed: %v", err)
	}

	// Project authentication should not create any secrets (handled by platform)
	if len(secrets) != 0 {
		t.Errorf("Expected no secrets for project authentication, got %d secrets", len(secrets))
	}
}

func TestBuildAgentSecrets_MultipleAgents(t *testing.T) {
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

	lookup := func(key string) (string, bool) {
		return "", false // No interpolation needed
	}

	secrets, err := BuildAgentSecrets(agents, lookup)
	if err != nil {
		t.Fatalf("BuildAgentSecrets failed: %v", err)
	}

	// Should have 3 secrets (bearer, basic, header - not project)
	expectedCount := 3
	if len(secrets) != expectedCount {
		t.Errorf("Expected %d secrets, got %d", expectedCount, len(secrets))
	}

	// Check bearer token
	if val, exists := secrets["AGENTUITY_BEARER_1_BEARER_TOKEN"]; !exists {
		t.Error("Bearer token secret not found")
	} else if val.Text() != "bearer_token_1" {
		t.Errorf("Bearer token value incorrect: %s", val.Text())
	}

	// Check basic token
	expectedBasic := "pass2"
	if val, exists := secrets["AGENTUITY_BASIC_2_BASIC_TOKEN"]; !exists {
		t.Error("Basic token secret not found")
	} else if val.Text() != expectedBasic {
		t.Errorf("Basic token value incorrect: %s", val.Text())
	}

	// Check header token
	if val, exists := secrets["AGENTUITY_HEADER_3_HEADER_TOKEN"]; !exists {
		t.Error("Header token secret not found")
	} else if val.Text() != "header_value_3" {
		t.Errorf("Header token value incorrect: %s", val.Text())
	}
}

func TestBuildAgentSecrets_EmptyAgents(t *testing.T) {
	agents := []project.AgentConfig{}

	lookup := func(key string) (string, bool) {
		return "", false
	}

	secrets, err := BuildAgentSecrets(agents, lookup)
	if err != nil {
		t.Fatalf("BuildAgentSecrets failed: %v", err)
	}

	if len(secrets) != 0 {
		t.Errorf("Expected empty secrets map, got %d entries", len(secrets))
	}
}

// Integration tests that combine BuildAgentSecrets with ValidateAgentAuthentication
func TestBuildAgentSecrets_Integration_Bearer(t *testing.T) {
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

	lookup := func(key string) (string, bool) {
		return "", false // No interpolation needed
	}

	// Build secrets
	secrets, err := BuildAgentSecrets([]project.AgentConfig{agent}, lookup)
	if err != nil {
		t.Fatalf("BuildAgentSecrets failed: %v", err)
	}

	// Create lookup function using the secrets
	secretLookup := func(key string) (string, bool) {
		if val, exists := secrets[key]; exists {
			return val.Text(), true
		}
		return "", false
	}

	// Test valid authentication
	headers := http.Header{
		"Authorization": []string{"Bearer integration_bearer_token"},
	}
	if !ValidateAgentAuthentication(agent, secretLookup, headers) {
		t.Error("Expected authentication to succeed")
	}

	// Test invalid token
	invalidHeaders := http.Header{
		"Authorization": []string{"Bearer wrong_token"},
	}
	if ValidateAgentAuthentication(agent, secretLookup, invalidHeaders) {
		t.Error("Expected authentication to fail with wrong token")
	}
}

func TestBuildAgentSecrets_Integration_Basic(t *testing.T) {
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

	lookup := func(key string) (string, bool) {
		return "", false // No interpolation needed
	}

	// Build secrets
	secrets, err := BuildAgentSecrets([]project.AgentConfig{agent}, lookup)
	if err != nil {
		t.Fatalf("BuildAgentSecrets failed: %v", err)
	}

	// Create lookup function using the secrets
	secretLookup := func(key string) (string, bool) {
		if val, exists := secrets[key]; exists {
			return val.Text(), true
		}
		return "", false
	}

	// Test valid authentication
	credentials := base64.StdEncoding.EncodeToString([]byte("testuser:testpass"))
	headers := http.Header{
		"Authorization": []string{"Basic " + credentials},
	}
	if !ValidateAgentAuthentication(agent, secretLookup, headers) {
		t.Error("Expected authentication to succeed")
	}

	// Test invalid credentials
	wrongCredentials := base64.StdEncoding.EncodeToString([]byte("testuser:wrongpass"))
	invalidHeaders := http.Header{
		"Authorization": []string{"Basic " + wrongCredentials},
	}
	if ValidateAgentAuthentication(agent, secretLookup, invalidHeaders) {
		t.Error("Expected authentication to fail with wrong credentials")
	}
}

func TestBuildAgentSecrets_Integration_Header(t *testing.T) {
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

	lookup := func(key string) (string, bool) {
		return "", false // No interpolation needed
	}

	// Build secrets
	secrets, err := BuildAgentSecrets([]project.AgentConfig{agent}, lookup)
	if err != nil {
		t.Fatalf("BuildAgentSecrets failed: %v", err)
	}

	// Create lookup function using the secrets
	secretLookup := func(key string) (string, bool) {
		if val, exists := secrets[key]; exists {
			return val.Text(), true
		}
		return "", false
	}

	// Test valid authentication
	headers := http.Header{
		"X-Custom-Auth": []string{"integration_header_value"},
	}
	if !ValidateAgentAuthentication(agent, secretLookup, headers) {
		t.Error("Expected authentication to succeed")
	}

	// Test invalid header value
	invalidHeaders := http.Header{
		"X-Custom-Auth": []string{"wrong_header_value"},
	}
	if ValidateAgentAuthentication(agent, secretLookup, invalidHeaders) {
		t.Error("Expected authentication to fail with wrong header value")
	}

	// Test missing header
	emptyHeaders := http.Header{}
	if ValidateAgentAuthentication(agent, secretLookup, emptyHeaders) {
		t.Error("Expected authentication to fail with missing header")
	}
}

func TestBuildAgentSecrets_Integration_Project(t *testing.T) {
	agent := project.AgentConfig{
		ID:   "agent_integration_project",
		Name: "Integration Project Test",
		Authentication: project.Authentication{
			Type: project.AuthenticationTypeProject,
		},
	}

	lookup := func(key string) (string, bool) {
		return "", false
	}

	// Build secrets (should be empty for project type)
	secrets, err := BuildAgentSecrets([]project.AgentConfig{agent}, lookup)
	if err != nil {
		t.Fatalf("BuildAgentSecrets failed: %v", err)
	}

	// Create lookup function that includes the project key
	projectKey := "global_project_secret_key"
	secretLookup := func(key string) (string, bool) {
		if key == "AGENTUITY_PROJECT_KEY" {
			return projectKey, true
		}
		if val, exists := secrets[key]; exists {
			return val.Text(), true
		}
		return "", false
	}

	// Test valid project authentication
	headers := http.Header{
		"Authorization": []string{"Bearer " + projectKey},
	}
	if !ValidateAgentAuthentication(agent, secretLookup, headers) {
		t.Error("Expected project authentication to succeed")
	}

	// Test invalid project key
	invalidHeaders := http.Header{
		"Authorization": []string{"Bearer wrong_project_key"},
	}
	if ValidateAgentAuthentication(agent, secretLookup, invalidHeaders) {
		t.Error("Expected project authentication to fail with wrong key")
	}
}

func TestBuildAgentSecrets_Integration_MultipleAgents(t *testing.T) {
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

	lookup := func(key string) (string, bool) {
		return "", false // No interpolation needed
	}

	// Build secrets
	secrets, err := BuildAgentSecrets(agents, lookup)
	if err != nil {
		t.Fatalf("BuildAgentSecrets failed: %v", err)
	}

	// Create lookup function using the secrets
	secretLookup := func(key string) (string, bool) {
		if val, exists := secrets[key]; exists {
			return val.Text(), true
		}
		return "", false
	}

	// Test bearer agent authentication
	bearerHeaders := http.Header{
		"Authorization": []string{"Bearer multi_bearer_token"},
	}
	if !ValidateAgentAuthentication(agents[0], secretLookup, bearerHeaders) {
		t.Error("Expected bearer agent authentication to succeed")
	}

	// Test basic agent authentication
	basicCredentials := base64.StdEncoding.EncodeToString([]byte("multiuser:multipass"))
	basicHeaders := http.Header{
		"Authorization": []string{"Basic " + basicCredentials},
	}
	if !ValidateAgentAuthentication(agents[1], secretLookup, basicHeaders) {
		t.Error("Expected basic agent authentication to succeed")
	}

	// Test wrong agent with wrong authentication type
	if ValidateAgentAuthentication(agents[0], secretLookup, basicHeaders) {
		t.Error("Expected bearer agent to fail with basic auth header")
	}

	if ValidateAgentAuthentication(agents[1], secretLookup, bearerHeaders) {
		t.Error("Expected basic agent to fail with bearer auth header")
	}
}
