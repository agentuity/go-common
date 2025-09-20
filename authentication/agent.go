package authentication

import (
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/agentuity/go-common/project"
)

func createAgentSecretPrefix(agentId string, suffix string) string {
	id := strings.TrimPrefix(agentId, "agent_")
	return fmt.Sprintf("AGENTUITY_%s_%s", strings.ToUpper(id), suffix)
}

func getAuthToken(headers http.Header, prefix string) (string, bool) {
	token := headers.Get("Authorization")
	if token == "" {
		return "", false
	}
	if !strings.HasPrefix(token, prefix) || len(token) < len(prefix)+1 {
		return "", false
	}
	return token[len(prefix):], true
}

func getBearerToken(headers http.Header) (string, bool) {
	return getAuthToken(headers, "Bearer ")
}

func getBasicToken(headers http.Header) (string, bool) {
	return getAuthToken(headers, "Basic ")
}

type EnvLookupFunc func(string) (string, bool)

// BuildAgentSecrets will build a map of secrets for the given agents based on their authentication type.
func BuildAgentSecrets(agents []project.AgentConfig) (map[string]string, error) {
	kv := make(map[string]string)
	for _, config := range agents {
		switch config.Authentication.Type {
		case project.AuthenticationTypeProject:
			continue // this will automatically be included by the platform
		case project.AuthenticationTypeBearer:
			val, ok := config.Authentication.Fields["token"].(string)
			if !ok {
				return nil, fmt.Errorf("missing token field for agent %s when using bearer authentication type", config.ID)
			}
			secretName := createAgentSecretPrefix(config.ID, "BEARER_TOKEN")
			kv[secretName] = val
		case project.AuthenticationTypeBasic:
			_, ok := config.Authentication.Fields["username"].(string)
			if !ok {
				return nil, fmt.Errorf("missing username field for agent %s when using basic authentication type", config.ID)
			}
			password, ok := config.Authentication.Fields["password"].(string)
			if !ok {
				return nil, fmt.Errorf("missing password field for agent %s when using basic authentication type", config.ID)
			}
			secretName := createAgentSecretPrefix(config.ID, "BASIC_TOKEN")
			kv[secretName] = password
		case project.AuthenticationTypeHeader:
			_, ok := config.Authentication.Fields["name"].(string)
			if !ok {
				return nil, fmt.Errorf("missing name field for agent %s when using header authentication type", config.ID)
			}
			value, ok := config.Authentication.Fields["value"].(string)
			if !ok {
				return nil, fmt.Errorf("missing value field for agent %s when using header authentication type", config.ID)
			}
			secretName := createAgentSecretPrefix(config.ID, "HEADER_TOKEN")
			kv[secretName] = value
		}
	}
	return kv, nil
}

// ValidateAgentAuthentication validates the authentication of an agent.
func ValidateAgentAuthentication(config project.AgentConfig, lookup EnvLookupFunc, headers http.Header) bool {
	switch config.Authentication.Type {
	case project.AuthenticationTypeProject:
		if val, ok := lookup("AGENTUITY_PROJECT_KEY"); ok {
			if token, ok := getBearerToken(headers); ok {
				return subtle.ConstantTimeCompare([]byte(token), []byte(val)) == 1
			}
		}
		return false
	case project.AuthenticationTypeBearer:
		secretName := createAgentSecretPrefix(config.ID, "BEARER_TOKEN")
		if val, ok := lookup(secretName); ok {
			if token, ok := getBearerToken(headers); ok {
				return subtle.ConstantTimeCompare([]byte(token), []byte(val)) == 1
			}
		}
		return false
	case project.AuthenticationTypeBasic:
		val, ok := getBasicToken(headers)
		if ok {
			secretName := createAgentSecretPrefix(config.ID, "BASIC_TOKEN")
			if passwd, ok := lookup(secretName); ok && config.Authentication.Fields != nil {
				name, ok := config.Authentication.Fields["username"].(string)
				if ok {
					token := base64.StdEncoding.EncodeToString([]byte(name + ":" + passwd))
					return subtle.ConstantTimeCompare([]byte(token), []byte(val)) == 1
				}
			}
		}
		return false
	case project.AuthenticationTypeHeader:
		secretName := createAgentSecretPrefix(config.ID, "HEADER_TOKEN")
		if val, ok := lookup(secretName); ok && config.Authentication.Fields != nil {
			name, ok := config.Authentication.Fields["name"].(string)
			if ok {
				token := headers.Get(name)
				if token != "" {
					return subtle.ConstantTimeCompare([]byte(token), []byte(val)) == 1
				}
			}
		}
		return false
	default:
	}
	return true
}

// OSEnvLookup is a function that looks up environment variables.
var OSEnvLookup = func(key string) (string, bool) {
	return os.LookupEnv(key)
}

// AgentAuthenticationMiddleware returns HTTP middleware that authenticates agents.
func AgentAuthenticationMiddleware(config []project.AgentConfig, lookup EnvLookupFunc, next http.HandlerFunc) http.HandlerFunc {
	agents := make(map[string]project.AgentConfig)
	for _, agent := range config {
		if agent.Authentication.Type != "" {
			agents[agent.ID] = agent
		}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		pathTok := strings.Split(path, "/")
		if len(pathTok) > 1 && strings.HasPrefix(pathTok[1], "agent_") {
			if agent, ok := agents[pathTok[1]]; ok {
				if !ValidateAgentAuthentication(agent, lookup, r.Header) {
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}
			}
		}
		next(w, r)
	}
}
