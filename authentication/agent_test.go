package authentication

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/agentuity/go-common/project"
	"gopkg.in/yaml.v3"
)

func TestAgentYAMLUnmarshaling(t *testing.T) {
	yamlData := `
agents:
  - id: agent_1ef45f97c229c9f0df0674581dc0d3ce
    name: my agent
    authentication:
      type: basic
      username: admin
      password: password
  - id: agent_292faa3dc376be1d86326a558c0ebce7
    name: testagent2
    authentication:
      type: project
  - id: agent_7c7eb2157b2189af3b332f5951d03972
    name: my-agent
    authentication:
      type: header
      name: X_AUTH_TOKEN
      value: value
  - id: agent_bearer_test
    name: bearer-agent
    authentication:
      type: bearer
  - id: agent_no_auth
    name: no-auth-agent
`

	var config struct {
		Agents []project.AgentConfig `yaml:"agents"`
	}

	err := yaml.Unmarshal([]byte(yamlData), &config)
	if err != nil {
		t.Fatalf("Failed to unmarshal YAML: %v", err)
	}

	if len(config.Agents) != 5 {
		t.Fatalf("Expected 5 agents, got %d", len(config.Agents))
	}

	// Test basic auth agent
	basicAgent := config.Agents[0]
	if basicAgent.ID != "agent_1ef45f97c229c9f0df0674581dc0d3ce" {
		t.Errorf("Expected agent ID 'agent_1ef45f97c229c9f0df0674581dc0d3ce', got '%s'", basicAgent.ID)
	}
	if basicAgent.Authentication.Type != "basic" {
		t.Errorf("Expected auth type 'basic', got '%s'", basicAgent.Authentication.Type)
	}
	if basicAgent.Authentication.Fields["username"] != "admin" {
		t.Errorf("Expected username 'admin', got '%v'", basicAgent.Authentication.Fields["username"])
	}

	// Test project auth agent
	projectAgent := config.Agents[1]
	if projectAgent.Authentication.Type != "project" {
		t.Errorf("Expected auth type 'project', got '%s'", projectAgent.Authentication.Type)
	}

	// Test header auth agent
	headerAgent := config.Agents[2]
	if headerAgent.Authentication.Type != "header" {
		t.Errorf("Expected auth type 'header', got '%s'", headerAgent.Authentication.Type)
	}
	if headerAgent.Authentication.Fields["name"] != "X_AUTH_TOKEN" {
		t.Errorf("Expected header name 'X_AUTH_TOKEN', got '%v'", headerAgent.Authentication.Fields["name"])
	}

	// Test bearer auth agent
	bearerAgent := config.Agents[3]
	if bearerAgent.Authentication.Type != "bearer" {
		t.Errorf("Expected auth type 'bearer', got '%s'", bearerAgent.Authentication.Type)
	}

	// Test no auth agent (should have empty authentication)
	noAuthAgent := config.Agents[4]
	if noAuthAgent.Authentication.Type != "" {
		t.Errorf("Expected empty auth type, got '%s'", noAuthAgent.Authentication.Type)
	}
}

func TestAgentJSONUnmarshaling(t *testing.T) {
	jsonData := `{
		"agents": [
			{
				"id": "agent_1ef45f97c229c9f0df0674581dc0d3ce",
				"name": "my agent",
				"authentication": {
					"type": "basic",
					"username": "admin",
					"password": "password"
				}
			},
			{
				"id": "agent_292faa3dc376be1d86326a558c0ebce7",
				"name": "testagent2",
				"authentication": {
					"type": "project"
				}
			},
			{
				"id": "agent_7c7eb2157b2189af3b332f5951d03972",
				"name": "my-agent",
				"authentication": {
					"type": "header",
					"name": "X_AUTH_TOKEN",
					"value": "value"
				}
			},
			{
				"id": "agent_bearer_test",
				"name": "bearer-agent",
				"authentication": {
					"type": "bearer"
				}
			},
			{
				"id": "agent_no_auth",
				"name": "no-auth-agent"
			}
		]
	}`

	var config struct {
		Agents []project.AgentConfig `json:"agents"`
	}

	err := json.Unmarshal([]byte(jsonData), &config)
	if err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	if len(config.Agents) != 5 {
		t.Fatalf("Expected 5 agents, got %d", len(config.Agents))
	}

	// Test basic auth agent
	basicAgent := config.Agents[0]
	if basicAgent.Authentication.Type != "basic" {
		t.Errorf("Expected auth type 'basic', got '%s'", basicAgent.Authentication.Type)
	}
	if basicAgent.Authentication.Fields["username"] != "admin" {
		t.Errorf("Expected username 'admin', got '%v'", basicAgent.Authentication.Fields["username"])
	}

	// Test project auth agent
	projectAgent := config.Agents[1]
	if projectAgent.Authentication.Type != "project" {
		t.Errorf("Expected auth type 'project', got '%s'", projectAgent.Authentication.Type)
	}

	// Test header auth agent
	headerAgent := config.Agents[2]
	if headerAgent.Authentication.Type != "header" {
		t.Errorf("Expected auth type 'header', got '%s'", headerAgent.Authentication.Type)
	}
	if headerAgent.Authentication.Fields["name"] != "X_AUTH_TOKEN" {
		t.Errorf("Expected header name 'X_AUTH_TOKEN', got '%v'", headerAgent.Authentication.Fields["name"])
	}
}

func TestValidateAgentAuthentication_Project(t *testing.T) {
	agent := project.AgentConfig{
		ID: "test_agent",
		Authentication: project.Authentication{
			Type: "project",
		},
	}

	tests := []struct {
		name           string
		envVars        map[string]string
		headers        http.Header
		expectedResult bool
	}{
		{
			name: "valid project authentication",
			envVars: map[string]string{
				"AGENTUITY_PROJECT_KEY": "valid_project_key",
			},
			headers: http.Header{
				"Authorization": []string{"Bearer valid_project_key"},
			},
			expectedResult: true,
		},
		{
			name: "invalid project token",
			envVars: map[string]string{
				"AGENTUITY_PROJECT_KEY": "valid_project_key",
			},
			headers: http.Header{
				"Authorization": []string{"Bearer wrong_key"},
			},
			expectedResult: false,
		},
		{
			name:    "missing project key env var",
			envVars: map[string]string{},
			headers: http.Header{
				"Authorization": []string{"Bearer some_key"},
			},
			expectedResult: false,
		},
		{
			name: "missing authorization header",
			envVars: map[string]string{
				"AGENTUITY_PROJECT_KEY": "valid_project_key",
			},
			headers:        http.Header{},
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup := func(key string) (string, bool) {
				val, ok := tt.envVars[key]
				return val, ok
			}

			result := ValidateAgentAuthentication(agent, lookup, tt.headers)
			if result != tt.expectedResult {
				t.Errorf("Expected %v, got %v", tt.expectedResult, result)
			}
		})
	}
}

func TestValidateAgentAuthentication_Bearer(t *testing.T) {
	agent := project.AgentConfig{
		ID: "agent_test123",
		Authentication: project.Authentication{
			Type: "bearer",
		},
	}

	tests := []struct {
		name           string
		envVars        map[string]string
		headers        http.Header
		expectedResult bool
	}{
		{
			name: "valid bearer authentication",
			envVars: map[string]string{
				"AGENTUITY_TEST123_BEARER_TOKEN": "valid_bearer_token",
			},
			headers: http.Header{
				"Authorization": []string{"Bearer valid_bearer_token"},
			},
			expectedResult: true,
		},
		{
			name: "invalid bearer token",
			envVars: map[string]string{
				"AGENTUITY_TEST123_BEARER_TOKEN": "valid_bearer_token",
			},
			headers: http.Header{
				"Authorization": []string{"Bearer wrong_token"},
			},
			expectedResult: false,
		},
		{
			name:    "missing bearer token env var",
			envVars: map[string]string{},
			headers: http.Header{
				"Authorization": []string{"Bearer some_token"},
			},
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup := func(key string) (string, bool) {
				val, ok := tt.envVars[key]
				return val, ok
			}

			result := ValidateAgentAuthentication(agent, lookup, tt.headers)
			if result != tt.expectedResult {
				t.Errorf("Expected %v, got %v", tt.expectedResult, result)
			}
		})
	}
}

func TestValidateAgentAuthentication_Basic(t *testing.T) {
	agent := project.AgentConfig{
		ID: "agent_test123",
		Authentication: project.Authentication{
			Type: "basic",
			Fields: map[string]any{
				"username": "admin",
			},
		},
	}

	validCredentials := base64.StdEncoding.EncodeToString([]byte("admin:secret_password"))

	tests := []struct {
		name           string
		envVars        map[string]string
		headers        http.Header
		expectedResult bool
	}{
		{
			name: "valid basic authentication",
			envVars: map[string]string{
				"AGENTUITY_TEST123_BASIC_TOKEN": "secret_password",
			},
			headers: http.Header{
				"Authorization": []string{"Basic " + validCredentials},
			},
			expectedResult: true,
		},
		{
			name: "invalid basic credentials",
			envVars: map[string]string{
				"AGENTUITY_TEST123_BASIC_TOKEN": "secret_password",
			},
			headers: http.Header{
				"Authorization": []string{"Basic " + base64.StdEncoding.EncodeToString([]byte("admin:wrong_password"))},
			},
			expectedResult: false,
		},
		{
			name:    "missing basic token env var",
			envVars: map[string]string{},
			headers: http.Header{
				"Authorization": []string{"Basic " + validCredentials},
			},
			expectedResult: false,
		},
		{
			name: "missing username in config",
			envVars: map[string]string{
				"AGENTUITY_TEST123_BASIC_TOKEN": "secret_password",
			},
			headers: http.Header{
				"Authorization": []string{"Basic " + validCredentials},
			},
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup := func(key string) (string, bool) {
				val, ok := tt.envVars[key]
				return val, ok
			}

			testAgent := agent
			if tt.name == "missing username in config" {
				testAgent.Authentication.Fields = nil
			}

			result := ValidateAgentAuthentication(testAgent, lookup, tt.headers)
			if result != tt.expectedResult {
				t.Errorf("Expected %v, got %v", tt.expectedResult, result)
			}
		})
	}
}

func TestValidateAgentAuthentication_Header(t *testing.T) {
	agent := project.AgentConfig{
		ID: "agent_test123",
		Authentication: project.Authentication{
			Type: "header",
			Fields: map[string]any{
				"name": "X-API-Key",
			},
		},
	}

	tests := []struct {
		name           string
		envVars        map[string]string
		headers        http.Header
		expectedResult bool
	}{
		{
			name: "valid header authentication",
			envVars: map[string]string{
				"AGENTUITY_TEST123_HEADER_TOKEN": "secret_api_key",
			},
			headers: http.Header{
				"X-Api-Key": []string{"secret_api_key"},
			},
			expectedResult: true,
		},
		{
			name: "invalid header token",
			envVars: map[string]string{
				"AGENTUITY_TEST123_HEADER_TOKEN": "secret_api_key",
			},
			headers: http.Header{
				"X-Api-Key": []string{"wrong_api_key"},
			},
			expectedResult: false,
		},
		{
			name:    "missing header token env var",
			envVars: map[string]string{},
			headers: http.Header{
				"X-Api-Key": []string{"secret_api_key"},
			},
			expectedResult: false,
		},
		{
			name: "missing header in request",
			envVars: map[string]string{
				"AGENTUITY_TEST123_HEADER_TOKEN": "secret_api_key",
			},
			headers:        http.Header{},
			expectedResult: false,
		},
		{
			name: "missing header name in config",
			envVars: map[string]string{
				"AGENTUITY_TEST123_HEADER_TOKEN": "secret_api_key",
			},
			headers: http.Header{
				"X-Api-Key": []string{"secret_api_key"},
			},
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup := func(key string) (string, bool) {
				val, ok := tt.envVars[key]
				return val, ok
			}

			testAgent := agent
			if tt.name == "missing header name in config" {
				testAgent.Authentication.Fields = nil
			}

			result := ValidateAgentAuthentication(testAgent, lookup, tt.headers)
			if result != tt.expectedResult {
				t.Errorf("Expected %v, got %v", tt.expectedResult, result)
			}
		})
	}
}

func TestValidateAgentAuthentication_NoAuth(t *testing.T) {
	agent := project.AgentConfig{
		ID: "agent_no_auth",
		Authentication: project.Authentication{
			Type: "", // empty type should default to allowing access
		},
	}

	lookup := func(key string) (string, bool) {
		return "", false
	}

	result := ValidateAgentAuthentication(agent, lookup, http.Header{})
	if !result {
		t.Errorf("Expected true for agent with no authentication, got false")
	}
}

func TestValidateAgentAuthentication_UnknownType(t *testing.T) {
	agent := project.AgentConfig{
		ID: "agent_unknown",
		Authentication: project.Authentication{
			Type: "unknown_type",
		},
	}

	lookup := func(key string) (string, bool) {
		return "", false
	}

	result := ValidateAgentAuthentication(agent, lookup, http.Header{})
	if !result {
		t.Errorf("Expected true for agent with unknown authentication type, got false")
	}
}

func TestGetBearerToken(t *testing.T) {
	tests := []struct {
		name           string
		authHeader     string
		expectedToken  string
		expectedExists bool
	}{
		{
			name:           "valid bearer token",
			authHeader:     "Bearer mytoken123",
			expectedToken:  "mytoken123",
			expectedExists: true,
		},
		{
			name:           "empty authorization header",
			authHeader:     "",
			expectedToken:  "",
			expectedExists: false,
		},
		{
			name:           "too short token",
			authHeader:     "Bearer ",
			expectedToken:  "",
			expectedExists: false,
		},
		{
			name:           "malformed bearer header",
			authHeader:     "Basic token123",
			expectedToken:  "",
			expectedExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := http.Header{}
			if tt.authHeader != "" {
				headers.Set("Authorization", tt.authHeader)
			}

			token, exists := getBearerToken(headers)
			if token != tt.expectedToken {
				t.Errorf("Expected token '%s', got '%s'", tt.expectedToken, token)
			}
			if exists != tt.expectedExists {
				t.Errorf("Expected exists %v, got %v", tt.expectedExists, exists)
			}
		})
	}
}

func TestGetBasicToken(t *testing.T) {
	tests := []struct {
		name           string
		authHeader     string
		expectedToken  string
		expectedExists bool
	}{
		{
			name:           "valid basic token",
			authHeader:     "Basic dGVzdDp0ZXN0",
			expectedToken:  "dGVzdDp0ZXN0",
			expectedExists: true,
		},
		{
			name:           "empty authorization header",
			authHeader:     "",
			expectedToken:  "",
			expectedExists: false,
		},
		{
			name:           "too short token",
			authHeader:     "Basic ",
			expectedToken:  "",
			expectedExists: false,
		},
		{
			name:           "malformed basic header",
			authHeader:     "Bearer token123",
			expectedToken:  "",
			expectedExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := http.Header{}
			if tt.authHeader != "" {
				headers.Set("Authorization", tt.authHeader)
			}

			token, exists := getBasicToken(headers)
			if token != tt.expectedToken {
				t.Errorf("Expected token '%s', got '%s'", tt.expectedToken, token)
			}
			if exists != tt.expectedExists {
				t.Errorf("Expected exists %v, got %v", tt.expectedExists, exists)
			}
		})
	}
}

func TestCreateAgentSecretPrefix(t *testing.T) {
	tests := []struct {
		name           string
		agentId        string
		suffix         string
		expectedPrefix string
	}{
		{
			name:           "agent with prefix",
			agentId:        "agent_test123",
			suffix:         "BEARER_TOKEN",
			expectedPrefix: "AGENTUITY_TEST123_BEARER_TOKEN",
		},
		{
			name:           "agent without prefix",
			agentId:        "test456",
			suffix:         "BASIC_TOKEN",
			expectedPrefix: "AGENTUITY_TEST456_BASIC_TOKEN",
		},
		{
			name:           "empty suffix",
			agentId:        "agent_xyz",
			suffix:         "",
			expectedPrefix: "AGENTUITY_XYZ_",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := createAgentSecretPrefix(tt.agentId, tt.suffix)
			if result != tt.expectedPrefix {
				t.Errorf("Expected prefix '%s', got '%s'", tt.expectedPrefix, result)
			}
		})
	}
}
