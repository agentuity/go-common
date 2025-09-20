package authentication

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentuity/go-common/project"
	cstr "github.com/agentuity/go-common/string"
	"gopkg.in/yaml.v3"
)

func TestYAMLUnmarshaling_WithInterpolation(t *testing.T) {
	yamlData := `
agents:
  - id: agent_interpolated_bearer
    name: Interpolated Bearer Agent
    authentication:
      type: bearer
      token: "{{BEARER_SECRET}}"
  - id: agent_interpolated_basic
    name: Interpolated Basic Agent
    authentication:
      type: basic
      username: admin
      password: "{{BASIC_PASSWORD}}"
  - id: agent_interpolated_header
    name: Interpolated Header Agent
    authentication:
      type: header
      name: X-API-Key
      value: "{{HEADER_TOKEN}}"
  - id: agent_default_value
    name: Agent with Default Value
    authentication:
      type: bearer
      token: "{{MISSING_TOKEN:-default_token_value}}"
  - id: agent_required_value
    name: Agent with Required Value
    authentication:
      type: bearer
      token: "{{!REQUIRED_TOKEN}}"
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

	// Test bearer agent with interpolation
	bearerAgent := config.Agents[0]
	if bearerAgent.Authentication.Fields["token"] != "{{BEARER_SECRET}}" {
		t.Errorf("Expected bearer token '{{BEARER_SECRET}}', got '%v'", bearerAgent.Authentication.Fields["token"])
	}

	// Test basic agent with interpolation
	basicAgent := config.Agents[1]
	if basicAgent.Authentication.Fields["password"] != "{{BASIC_PASSWORD}}" {
		t.Errorf("Expected basic password '{{BASIC_PASSWORD}}', got '%v'", basicAgent.Authentication.Fields["password"])
	}

	// Test header agent with interpolation
	headerAgent := config.Agents[2]
	if headerAgent.Authentication.Fields["value"] != "{{HEADER_TOKEN}}" {
		t.Errorf("Expected header value '{{HEADER_TOKEN}}', got '%v'", headerAgent.Authentication.Fields["value"])
	}

	// Test default value syntax
	defaultAgent := config.Agents[3]
	if defaultAgent.Authentication.Fields["token"] != "{{MISSING_TOKEN:-default_token_value}}" {
		t.Errorf("Expected default token syntax, got '%v'", defaultAgent.Authentication.Fields["token"])
	}

	// Test required value syntax
	requiredAgent := config.Agents[4]
	if requiredAgent.Authentication.Fields["token"] != "{{!REQUIRED_TOKEN}}" {
		t.Errorf("Expected required token syntax, got '%v'", requiredAgent.Authentication.Fields["token"])
	}
}

func TestJSONUnmarshaling_WithInterpolation(t *testing.T) {
	jsonData := `{
		"agents": [
			{
				"id": "agent_json_bearer",
				"name": "JSON Bearer Agent",
				"authentication": {
					"type": "bearer",
					"token": "{{JSON_BEARER_TOKEN}}"
				}
			},
			{
				"id": "agent_json_basic",
				"name": "JSON Basic Agent",
				"authentication": {
					"type": "basic",
					"username": "user",
					"password": "{{JSON_BASIC_PASSWORD}}"
				}
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

	bearerAgent := config.Agents[0]
	if bearerAgent.Authentication.Fields["token"] != "{{JSON_BEARER_TOKEN}}" {
		t.Errorf("Expected token '{{JSON_BEARER_TOKEN}}', got '%v'", bearerAgent.Authentication.Fields["token"])
	}

	basicAgent := config.Agents[1]
	if basicAgent.Authentication.Fields["password"] != "{{JSON_BASIC_PASSWORD}}" {
		t.Errorf("Expected password '{{JSON_BASIC_PASSWORD}}', got '%v'", basicAgent.Authentication.Fields["password"])
	}
}

func TestBuildAgentSecrets_WithInterpolation(t *testing.T) {
	agents := []project.AgentConfig{
		{
			ID:   "agent_bearer_interpolated",
			Name: "Bearer Interpolated Agent",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeBearer,
				Fields: map[string]any{
					"token": "{{BEARER_SECRET}}",
				},
			},
		},
		{
			ID:   "agent_basic_interpolated",
			Name: "Basic Interpolated Agent",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeBasic,
				Fields: map[string]any{
					"username": "admin",
					"password": "{{BASIC_SECRET}}",
				},
			},
		},
		{
			ID:   "agent_header_interpolated",
			Name: "Header Interpolated Agent",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeHeader,
				Fields: map[string]any{
					"name":  "X-API-Key",
					"value": "{{HEADER_SECRET}}",
				},
			},
		},
	}

	// Mock lookup function
	lookup := func(key string) (string, bool) {
		secrets := map[string]string{
			"BEARER_SECRET": "interpolated_bearer_token",
			"BASIC_SECRET":  "interpolated_basic_password",
			"HEADER_SECRET": "interpolated_header_value",
		}
		val, exists := secrets[key]
		return val, exists
	}

	secrets, err := BuildAgentSecrets(agents, lookup)
	if err != nil {
		t.Fatalf("BuildAgentSecrets failed: %v", err)
	}

	// Test bearer token interpolation
	bearerKey := "AGENTUITY_BEARER_INTERPOLATED_BEARER_TOKEN"
	if val, exists := secrets[bearerKey]; !exists {
		t.Errorf("Expected key %s not found", bearerKey)
	} else if val.Text() != "interpolated_bearer_token" {
		t.Errorf("Expected interpolated value 'interpolated_bearer_token', got '%s'", val.Text())
	}

	// Test basic password interpolation
	basicKey := "AGENTUITY_BASIC_INTERPOLATED_BASIC_TOKEN"
	if val, exists := secrets[basicKey]; !exists {
		t.Errorf("Expected key %s not found", basicKey)
	} else if val.Text() != "interpolated_basic_password" {
		t.Errorf("Expected interpolated value 'interpolated_basic_password', got '%s'", val.Text())
	}

	// Test header value interpolation
	headerKey := "AGENTUITY_HEADER_INTERPOLATED_HEADER_TOKEN"
	if val, exists := secrets[headerKey]; !exists {
		t.Errorf("Expected key %s not found", headerKey)
	} else if val.Text() != "interpolated_header_value" {
		t.Errorf("Expected interpolated value 'interpolated_header_value', got '%s'", val.Text())
	}
}

func TestBuildAgentSecrets_WithDefaultValues(t *testing.T) {
	agents := []project.AgentConfig{
		{
			ID:   "agent_default_test",
			Name: "Default Test Agent",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeBearer,
				Fields: map[string]any{
					"token": "{{MISSING_SECRET:-default_fallback_token}}",
				},
			},
		},
	}

	// Lookup function that doesn't have the secret
	lookup := func(key string) (string, bool) {
		return "", false
	}

	secrets, err := BuildAgentSecrets(agents, lookup)
	if err != nil {
		t.Fatalf("BuildAgentSecrets failed: %v", err)
	}

	secretKey := "AGENTUITY_DEFAULT_TEST_BEARER_TOKEN"
	if val, exists := secrets[secretKey]; !exists {
		t.Errorf("Expected key %s not found", secretKey)
	} else if val.Text() != "default_fallback_token" {
		t.Errorf("Expected default value 'default_fallback_token', got '%s'", val.Text())
	}
}

func TestBuildAgentSecrets_WithRequiredValues(t *testing.T) {
	agents := []project.AgentConfig{
		{
			ID:   "agent_required_test",
			Name: "Required Test Agent",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeBearer,
				Fields: map[string]any{
					"token": "{{!REQUIRED_SECRET}}",
				},
			},
		},
	}

	// Lookup function that doesn't have the required secret
	lookup := func(key string) (string, bool) {
		return "", false
	}

	// The current implementation silently ignores interpolation errors,
	// so the original template string is used when interpolation fails
	secrets, err := BuildAgentSecrets(agents, lookup)
	if err != nil {
		t.Fatalf("BuildAgentSecrets failed: %v", err)
	}

	secretKey := "AGENTUITY_REQUIRED_TEST_BEARER_TOKEN"
	if val, exists := secrets[secretKey]; !exists {
		t.Errorf("Expected key %s not found", secretKey)
	} else if val.Text() != "{{!REQUIRED_SECRET}}" {
		t.Errorf("Expected template string '{{!REQUIRED_SECRET}}', got '%s'", val.Text())
	}

	// Test with the required value present - should interpolate correctly
	lookupWithSecret := func(key string) (string, bool) {
		if key == "REQUIRED_SECRET" {
			return "required_secret_value", true
		}
		return "", false
	}

	secrets, err = BuildAgentSecrets(agents, lookupWithSecret)
	if err != nil {
		t.Fatalf("BuildAgentSecrets should succeed with required value: %v", err)
	}

	if val, exists := secrets[secretKey]; !exists {
		t.Errorf("Expected key %s not found", secretKey)
	} else if val.Text() != "required_secret_value" {
		t.Errorf("Expected required value 'required_secret_value', got '%s'", val.Text())
	}
}

func TestBuildAgentSecrets_NoInterpolationNeeded(t *testing.T) {
	agents := []project.AgentConfig{
		{
			ID:   "agent_plain_text",
			Name: "Plain Text Agent",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeBearer,
				Fields: map[string]any{
					"token": "plain_text_token",
				},
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

	secretKey := "AGENTUITY_PLAIN_TEXT_BEARER_TOKEN"
	if val, exists := secrets[secretKey]; !exists {
		t.Errorf("Expected key %s not found", secretKey)
	} else if val.Text() != "plain_text_token" {
		t.Errorf("Expected plain text value 'plain_text_token', got '%s'", val.Text())
	}
}

func TestAgentAuthenticationMiddleware_WithInterpolation(t *testing.T) {
	agents := []project.AgentConfig{
		{
			ID:   "agent_middleware_bearer",
			Name: "Middleware Bearer Test",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeBearer,
				Fields: map[string]any{
					"token": "{{MIDDLEWARE_BEARER_TOKEN}}",
				},
			},
		},
		{
			ID:   "agent_middleware_basic",
			Name: "Middleware Basic Test",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeBasic,
				Fields: map[string]any{
					"username": "{{MIDDLEWARE_USERNAME}}",
					"password": "{{MIDDLEWARE_PASSWORD}}",
				},
			},
		},
		{
			ID:   "agent_middleware_header",
			Name: "Middleware Header Test",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeHeader,
				Fields: map[string]any{
					"name":  "{{MIDDLEWARE_HEADER_NAME}}",
					"value": "{{MIDDLEWARE_HEADER_VALUE}}",
				},
			},
		},
	}

	// Mock secrets from BuildAgentSecrets
	secrets := map[string]cstr.MaskedString{
		"AGENTUITY_MIDDLEWARE_BEARER_BEARER_TOKEN": cstr.NewMaskedString("interpolated_bearer_secret"),
		"AGENTUITY_MIDDLEWARE_BASIC_BASIC_TOKEN":   cstr.NewMaskedString("interpolated_basic_secret"),
		"AGENTUITY_MIDDLEWARE_HEADER_HEADER_TOKEN": cstr.NewMaskedString("interpolated_header_secret"),
	}

	// Combined lookup function for both interpolation and secret lookup
	lookup := func(key string) (string, bool) {
		// First check for interpolation values
		interpolationMap := map[string]string{
			"MIDDLEWARE_BEARER_TOKEN": "interpolated_bearer_secret",
			"MIDDLEWARE_USERNAME":     "interpolated_user",
			"MIDDLEWARE_PASSWORD":     "interpolated_basic_secret",
			"MIDDLEWARE_HEADER_NAME":  "X-Custom-Auth",
			"MIDDLEWARE_HEADER_VALUE": "interpolated_header_secret",
		}
		if val, exists := interpolationMap[key]; exists {
			return val, true
		}

		// Then check for agent secrets
		if secret, exists := secrets[key]; exists {
			return secret.Text(), true
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
		headers        map[string]string
		expectedStatus int
		expectedCalled bool
	}{
		{
			name: "bearer auth with interpolated token",
			path: "/api/agent_middleware_bearer",
			headers: map[string]string{
				"Authorization": "Bearer interpolated_bearer_secret",
			},
			expectedStatus: http.StatusOK,
			expectedCalled: true,
		},
		{
			name: "basic auth with interpolated credentials",
			path: "/api/agent_middleware_basic",
			headers: map[string]string{
				"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte("interpolated_user:interpolated_basic_secret")),
			},
			expectedStatus: http.StatusOK,
			expectedCalled: true,
		},
		{
			name: "header auth with interpolated header name and value",
			path: "/api/agent_middleware_header",
			headers: map[string]string{
				"X-Custom-Auth": "interpolated_header_secret",
			},
			expectedStatus: http.StatusOK,
			expectedCalled: true,
		},
		{
			name: "bearer auth with wrong token",
			path: "/api/agent_middleware_bearer",
			headers: map[string]string{
				"Authorization": "Bearer wrong_token",
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

func TestAgentAuthenticationMiddleware_InterpolationWithDefaults(t *testing.T) {
	agents := []project.AgentConfig{
		{
			ID:   "agent_default_middleware",
			Name: "Default Middleware Test",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeBearer,
				Fields: map[string]any{
					"token": "{{MISSING_MIDDLEWARE_TOKEN:-default_middleware_token}}",
				},
			},
		},
	}

	// Mock secrets that would be generated from BuildAgentSecrets
	secrets := map[string]cstr.MaskedString{
		"AGENTUITY_DEFAULT_MIDDLEWARE_BEARER_TOKEN": cstr.NewMaskedString("default_middleware_token"),
	}

	lookup := func(key string) (string, bool) {
		// Don't return the MISSING_MIDDLEWARE_TOKEN to test default fallback
		if secret, exists := secrets[key]; exists {
			return secret.Text(), true
		}
		return "", false
	}

	handlerCalled := false
	testHandler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}

	middleware := AgentAuthenticationMiddleware(agents, lookup, testHandler)

	req := httptest.NewRequest("POST", "/api/agent_default_middleware", nil)
	req.Header.Set("Authorization", "Bearer default_middleware_token")

	w := httptest.NewRecorder()
	middleware(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if !handlerCalled {
		t.Error("Expected handler to be called")
	}
}

func TestBuildAgentSecrets_MaskedStringBehavior(t *testing.T) {
	agents := []project.AgentConfig{
		{
			ID:   "agent_mask_test",
			Name: "Mask Test Agent",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeBearer,
				Fields: map[string]any{
					"token": "{{SECRET_TOKEN}}",
				},
			},
		},
	}

	lookup := func(key string) (string, bool) {
		if key == "SECRET_TOKEN" {
			return "very_secret_token_value", true
		}
		return "", false
	}

	secrets, err := BuildAgentSecrets(agents, lookup)
	if err != nil {
		t.Fatalf("BuildAgentSecrets failed: %v", err)
	}

	secretKey := "AGENTUITY_MASK_TEST_BEARER_TOKEN"
	maskedSecret, exists := secrets[secretKey]
	if !exists {
		t.Fatalf("Expected key %s not found", secretKey)
	}

	// Test that the masked string hides the actual value when printed
	maskedStr := maskedSecret.String()
	if maskedStr == "very_secret_token_value" {
		t.Error("MaskedString should not expose the actual value in String()")
	}
	if !strings.Contains(maskedStr, "*") {
		t.Error("MaskedString should contain asterisks when masked")
	}

	// Test that we can still get the actual value
	actualValue := maskedSecret.Text()
	if actualValue != "very_secret_token_value" {
		t.Errorf("Expected actual value 'very_secret_token_value', got '%s'", actualValue)
	}
}

func TestComplexInterpolationScenarios(t *testing.T) {
	agents := []project.AgentConfig{
		{
			ID:   "agent_complex",
			Name: "Complex Interpolation Agent",
			Authentication: project.Authentication{
				Type: project.AuthenticationTypeBasic,
				Fields: map[string]any{
					"username": "service-{{SERVICE_ENV}}",
					"password": "{{!DB_PASSWORD}}",
				},
			},
		},
	}

	lookup := func(key string) (string, bool) {
		secrets := map[string]string{
			"SERVICE_ENV": "production",
			"DB_PASSWORD": "super_secure_db_password",
		}
		val, exists := secrets[key]
		return val, exists
	}

	secrets, err := BuildAgentSecrets(agents, lookup)
	if err != nil {
		t.Fatalf("BuildAgentSecrets failed: %v", err)
	}

	secretKey := "AGENTUITY_COMPLEX_BASIC_TOKEN"
	if val, exists := secrets[secretKey]; !exists {
		t.Errorf("Expected key %s not found", secretKey)
	} else if val.Text() != "super_secure_db_password" {
		t.Errorf("Expected password 'super_secure_db_password', got '%s'", val.Text())
	}

	// Test that the middleware also interpolates the username correctly
	middlewareLookup := func(key string) (string, bool) {
		secrets := map[string]string{
			"SERVICE_ENV": "production",
			"DB_PASSWORD": "super_secure_db_password",
		}
		if val, exists := secrets[key]; exists {
			return val, true
		}
		// Also provide the generated secret
		if key == "AGENTUITY_COMPLEX_BASIC_TOKEN" {
			return "super_secure_db_password", true
		}
		return "", false
	}

	handlerCalled := false
	testHandler := func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}

	middleware := AgentAuthenticationMiddleware(agents, middlewareLookup, testHandler)

	// Test with the interpolated username
	credentials := base64.StdEncoding.EncodeToString([]byte("service-production:super_secure_db_password"))
	req := httptest.NewRequest("POST", "/api/agent_complex", nil)
	req.Header.Set("Authorization", "Basic "+credentials)

	w := httptest.NewRecorder()
	middleware(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if !handlerCalled {
		t.Error("Expected handler to be called")
	}
}
