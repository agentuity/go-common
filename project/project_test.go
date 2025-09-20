package project

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestResourcesUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name        string
		jsonData    string
		expected    Resources
		expectError bool
	}{
		{
			name:     "valid resources",
			jsonData: `{"memory":"1Gi","cpu":"500m","disk":"10Gi"}`,
			expected: Resources{
				Memory: "1Gi",
				CPU:    "500m",
				Disk:   "10Gi",
			},
			expectError: false,
		},
		{
			name:        "empty resources",
			jsonData:    `{}`,
			expected:    Resources{},
			expectError: false,
		},
		{
			name:        "invalid cpu",
			jsonData:    `{"cpu":"invalid","memory":"1Gi","disk":"10Gi"}`,
			expectError: true,
		},
		{
			name:        "invalid memory",
			jsonData:    `{"cpu":"500m","memory":"invalid","disk":"10Gi"}`,
			expectError: true,
		},
		{
			name:        "invalid disk",
			jsonData:    `{"cpu":"500m","memory":"1Gi","disk":"invalid"}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var r Resources
			err := json.Unmarshal([]byte(tt.jsonData), &r)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if r.CPU != tt.expected.CPU {
				t.Errorf("expected CPU %q, got %q", tt.expected.CPU, r.CPU)
			}
			if r.Memory != tt.expected.Memory {
				t.Errorf("expected Memory %q, got %q", tt.expected.Memory, r.Memory)
			}
			if r.Disk != tt.expected.Disk {
				t.Errorf("expected Disk %q, got %q", tt.expected.Disk, r.Disk)
			}

			// Test that quantities are parsed correctly when fields are not empty
			if r.CPU != "" {
				expectedCPU := resource.MustParse(tt.expected.CPU)
				if !r.CPUQuantity.Equal(expectedCPU) {
					t.Errorf("expected CPUQuantity %v, got %v", expectedCPU, r.CPUQuantity)
				}
			}
			if r.Memory != "" {
				expectedMemory := resource.MustParse(tt.expected.Memory)
				if !r.MemoryQuantity.Equal(expectedMemory) {
					t.Errorf("expected MemoryQuantity %v, got %v", expectedMemory, r.MemoryQuantity)
				}
			}
			if r.Disk != "" {
				expectedDisk := resource.MustParse(tt.expected.Disk)
				if !r.DiskQuantity.Equal(expectedDisk) {
					t.Errorf("expected DiskQuantity %v, got %v", expectedDisk, r.DiskQuantity)
				}
			}
		})
	}
}

func TestResourcesUnmarshalYAML(t *testing.T) {
	tests := []struct {
		name        string
		yamlData    string
		expected    Resources
		expectError bool
	}{
		{
			name: "valid resources",
			yamlData: `memory: 1Gi
cpu: 500m
disk: 10Gi`,
			expected: Resources{
				Memory: "1Gi",
				CPU:    "500m",
				Disk:   "10Gi",
			},
			expectError: false,
		},
		{
			name:        "empty resources",
			yamlData:    `{}`,
			expected:    Resources{},
			expectError: false,
		},
		{
			name: "invalid cpu",
			yamlData: `cpu: invalid
memory: 1Gi
disk: 10Gi`,
			expectError: true,
		},
		{
			name: "invalid memory",
			yamlData: `cpu: 500m
memory: invalid
disk: 10Gi`,
			expectError: true,
		},
		{
			name: "invalid disk",
			yamlData: `cpu: 500m
memory: 1Gi
disk: invalid`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var r Resources
			err := yaml.Unmarshal([]byte(tt.yamlData), &r)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if r.CPU != tt.expected.CPU {
				t.Errorf("expected CPU %q, got %q", tt.expected.CPU, r.CPU)
			}
			if r.Memory != tt.expected.Memory {
				t.Errorf("expected Memory %q, got %q", tt.expected.Memory, r.Memory)
			}
			if r.Disk != tt.expected.Disk {
				t.Errorf("expected Disk %q, got %q", tt.expected.Disk, r.Disk)
			}

			// Test that quantities are parsed correctly when fields are not empty
			if r.CPU != "" {
				expectedCPU := resource.MustParse(tt.expected.CPU)
				if !r.CPUQuantity.Equal(expectedCPU) {
					t.Errorf("expected CPUQuantity %v, got %v", expectedCPU, r.CPUQuantity)
				}
			}
			if r.Memory != "" {
				expectedMemory := resource.MustParse(tt.expected.Memory)
				if !r.MemoryQuantity.Equal(expectedMemory) {
					t.Errorf("expected MemoryQuantity %v, got %v", expectedMemory, r.MemoryQuantity)
				}
			}
			if r.Disk != "" {
				expectedDisk := resource.MustParse(tt.expected.Disk)
				if !r.DiskQuantity.Equal(expectedDisk) {
					t.Errorf("expected DiskQuantity %v, got %v", expectedDisk, r.DiskQuantity)
				}
			}
		})
	}
}

func TestResourcesPartialUnmarshaling(t *testing.T) {
	t.Run("json with only cpu", func(t *testing.T) {
		jsonData := `{"cpu":"2"}`
		var r Resources
		err := json.Unmarshal([]byte(jsonData), &r)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if r.CPU != "2" {
			t.Errorf("expected CPU '2', got %q", r.CPU)
		}
		if r.Memory != "" {
			t.Errorf("expected empty Memory, got %q", r.Memory)
		}
		if r.Disk != "" {
			t.Errorf("expected empty Disk, got %q", r.Disk)
		}

		expectedCPU := resource.MustParse("2")
		if !r.CPUQuantity.Equal(expectedCPU) {
			t.Errorf("expected CPUQuantity %v, got %v", expectedCPU, r.CPUQuantity)
		}
	})

	t.Run("yaml with only memory", func(t *testing.T) {
		yamlData := `memory: 2Gi`
		var r Resources
		err := yaml.Unmarshal([]byte(yamlData), &r)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if r.Memory != "2Gi" {
			t.Errorf("expected Memory '2Gi', got %q", r.Memory)
		}
		if r.CPU != "" {
			t.Errorf("expected empty CPU, got %q", r.CPU)
		}
		if r.Disk != "" {
			t.Errorf("expected empty Disk, got %q", r.Disk)
		}

		expectedMemory := resource.MustParse("2Gi")
		if !r.MemoryQuantity.Equal(expectedMemory) {
			t.Errorf("expected MemoryQuantity %v, got %v", expectedMemory, r.MemoryQuantity)
		}
	})
}

func TestResourcesNegativeQuantityValidation(t *testing.T) {
	t.Run("negative CPU JSON", func(t *testing.T) {
		jsonData := `{"cpu":"-500m","memory":"1Gi","disk":"10Gi"}`
		var r Resources
		err := json.Unmarshal([]byte(jsonData), &r)
		if err == nil {
			t.Errorf("expected error for negative CPU quantity, got none")
		}
		if err != nil && err.Error() != "resource CPU must be >= 0, got '-500m'" {
			t.Errorf("expected specific error message, got: %v", err)
		}
	})

	t.Run("negative Memory JSON", func(t *testing.T) {
		jsonData := `{"cpu":"500m","memory":"-1Gi","disk":"10Gi"}`
		var r Resources
		err := json.Unmarshal([]byte(jsonData), &r)
		if err == nil {
			t.Errorf("expected error for negative Memory quantity, got none")
		}
		if err != nil && err.Error() != "resource Memory must be >= 0, got '-1Gi'" {
			t.Errorf("expected specific error message, got: %v", err)
		}
	})

	t.Run("negative Disk JSON", func(t *testing.T) {
		jsonData := `{"cpu":"500m","memory":"1Gi","disk":"-10Gi"}`
		var r Resources
		err := json.Unmarshal([]byte(jsonData), &r)
		if err == nil {
			t.Errorf("expected error for negative Disk quantity, got none")
		}
		if err != nil && err.Error() != "resource Disk must be >= 0, got '-10Gi'" {
			t.Errorf("expected specific error message, got: %v", err)
		}
	})

	t.Run("negative CPU YAML", func(t *testing.T) {
		yamlData := `cpu: -500m
memory: 1Gi
disk: 10Gi`
		var r Resources
		err := yaml.Unmarshal([]byte(yamlData), &r)
		if err == nil {
			t.Errorf("expected error for negative CPU quantity, got none")
		}
		if err != nil && err.Error() != "resource CPU must be >= 0, got '-500m'" {
			t.Errorf("expected specific error message, got: %v", err)
		}
	})

	t.Run("negative Memory YAML", func(t *testing.T) {
		yamlData := `cpu: 500m
memory: -1Gi
disk: 10Gi`
		var r Resources
		err := yaml.Unmarshal([]byte(yamlData), &r)
		if err == nil {
			t.Errorf("expected error for negative Memory quantity, got none")
		}
		if err != nil && err.Error() != "resource Memory must be >= 0, got '-1Gi'" {
			t.Errorf("expected specific error message, got: %v", err)
		}
	})

	t.Run("negative Disk YAML", func(t *testing.T) {
		yamlData := `cpu: 500m
memory: 1Gi
disk: -10Gi`
		var r Resources
		err := yaml.Unmarshal([]byte(yamlData), &r)
		if err == nil {
			t.Errorf("expected error for negative Disk quantity, got none")
		}
		if err != nil && err.Error() != "resource Disk must be >= 0, got '-10Gi'" {
			t.Errorf("expected specific error message, got: %v", err)
		}
	})

	t.Run("zero quantities are valid", func(t *testing.T) {
		jsonData := `{"cpu":"0","memory":"0","disk":"0"}`
		var r Resources
		err := json.Unmarshal([]byte(jsonData), &r)
		if err != nil {
			t.Errorf("unexpected error for zero quantities: %v", err)
		}
	})
}

func TestTriggerMarshalUnmarshal(t *testing.T) {
	t.Run("JSON round trip", func(t *testing.T) {
		original := Trigger{
			Type:        TriggerTypeAPI,
			Destination: TriggerDirectionSource,
			Fields: map[string]any{
				"url":    "https://example.com",
				"method": "POST",
			},
		}

		// Marshal to JSON
		data, err := json.Marshal(original)
		if err != nil {
			t.Errorf("unexpected marshal error: %v", err)
		}

		// Unmarshal from JSON
		var unmarshaled Trigger
		err = json.Unmarshal(data, &unmarshaled)
		if err != nil {
			t.Errorf("unexpected unmarshal error: %v", err)
		}

		// Verify round trip
		if unmarshaled.Type != original.Type {
			t.Errorf("expected Type %v, got %v", original.Type, unmarshaled.Type)
		}
		if unmarshaled.Destination != original.Destination {
			t.Errorf("expected Destination %v, got %v", original.Destination, unmarshaled.Destination)
		}
		if len(unmarshaled.Fields) != len(original.Fields) {
			t.Errorf("expected %d fields, got %d", len(original.Fields), len(unmarshaled.Fields))
		}
		for k, v := range original.Fields {
			if unmarshaled.Fields[k] != v {
				t.Errorf("expected field %s=%v, got %v", k, v, unmarshaled.Fields[k])
			}
		}
	})

	t.Run("YAML round trip", func(t *testing.T) {
		original := Trigger{
			Type:        TriggerTypeWebhook,
			Destination: TriggerDirectionDestination,
			Fields: map[string]any{
				"secret": "my-secret",
				"port":   "8080",
			},
		}

		// Marshal to YAML
		data, err := yaml.Marshal(original)
		if err != nil {
			t.Errorf("unexpected marshal error: %v", err)
		}

		// Unmarshal from YAML
		var unmarshaled Trigger
		err = yaml.Unmarshal(data, &unmarshaled)
		if err != nil {
			t.Errorf("unexpected unmarshal error: %v", err)
		}

		// Verify round trip
		if unmarshaled.Type != original.Type {
			t.Errorf("expected Type %v, got %v", original.Type, unmarshaled.Type)
		}
		if unmarshaled.Destination != original.Destination {
			t.Errorf("expected Destination %v, got %v", original.Destination, unmarshaled.Destination)
		}
		if len(unmarshaled.Fields) != len(original.Fields) {
			t.Errorf("expected %d fields, got %d", len(original.Fields), len(unmarshaled.Fields))
		}
		for k, v := range original.Fields {
			if unmarshaled.Fields[k] != v {
				t.Errorf("expected field %s=%v, got %v", k, v, unmarshaled.Fields[k])
			}
		}
	})

	t.Run("missing type field JSON", func(t *testing.T) {
		jsonData := `{"destination":"source","url":"https://example.com"}`
		var trigger Trigger
		err := json.Unmarshal([]byte(jsonData), &trigger)
		if err == nil {
			t.Errorf("expected error for missing type field, got none")
		}
		if err != nil && err.Error() != "trigger type is required" {
			t.Errorf("expected specific error message, got: %v", err)
		}
	})

	t.Run("non-string type field JSON", func(t *testing.T) {
		jsonData := `{"type":123,"destination":"source"}`
		var trigger Trigger
		err := json.Unmarshal([]byte(jsonData), &trigger)
		if err == nil {
			t.Errorf("expected error for non-string type field, got none")
		}
		if err != nil && err.Error() != "trigger type must be a string, got float64" {
			t.Errorf("expected specific error message, got: %v", err)
		}
	})

	t.Run("unknown type value JSON", func(t *testing.T) {
		jsonData := `{"type":"unknown","destination":"source"}`
		var trigger Trigger
		err := json.Unmarshal([]byte(jsonData), &trigger)
		if err == nil {
			t.Errorf("expected error for unknown type value, got none")
		}
		if err != nil && err.Error() != "unknown trigger type: unknown. should be one of api, webhook, cron, manual, agent, sms, or email" {
			t.Errorf("expected specific error message, got: %v", err)
		}
	})

	t.Run("non-string destination field JSON", func(t *testing.T) {
		jsonData := `{"type":"api","destination":456}`
		var trigger Trigger
		err := json.Unmarshal([]byte(jsonData), &trigger)
		if err == nil {
			t.Errorf("expected error for non-string destination field, got none")
		}
		if err != nil && err.Error() != "trigger destination must be a string, got float64" {
			t.Errorf("expected specific error message, got: %v", err)
		}
	})

	t.Run("unknown destination value JSON", func(t *testing.T) {
		jsonData := `{"type":"api","destination":"invalid"}`
		var trigger Trigger
		err := json.Unmarshal([]byte(jsonData), &trigger)
		if err == nil {
			t.Errorf("expected error for unknown destination value, got none")
		}
		if err != nil && err.Error() != "unknown trigger destination: invalid. should be one of source or destination" {
			t.Errorf("expected specific error message, got: %v", err)
		}
	})

	t.Run("optional destination field", func(t *testing.T) {
		jsonData := `{"type":"api","url":"https://example.com"}`
		var trigger Trigger
		err := json.Unmarshal([]byte(jsonData), &trigger)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if trigger.Type != TriggerTypeAPI {
			t.Errorf("expected Type %v, got %v", TriggerTypeAPI, trigger.Type)
		}
		if trigger.Destination != "" {
			t.Errorf("expected empty Destination, got %v", trigger.Destination)
		}
		if trigger.Fields["url"] != "https://example.com" {
			t.Errorf("expected url field, got %v", trigger.Fields["url"])
		}
	})
}

func TestAuthenticationMarshalUnmarshal(t *testing.T) {
	t.Run("JSON round trip", func(t *testing.T) {
		original := Authentication{
			Type: AuthenticationTypeBearer,
			Fields: map[string]any{
				"token": "secret-token",
				"ttl":   "3600",
			},
		}

		// Marshal to JSON
		data, err := json.Marshal(original)
		if err != nil {
			t.Errorf("unexpected marshal error: %v", err)
		}

		// Unmarshal from JSON
		var unmarshaled Authentication
		err = json.Unmarshal(data, &unmarshaled)
		if err != nil {
			t.Errorf("unexpected unmarshal error: %v", err)
		}

		// Verify round trip
		if unmarshaled.Type != original.Type {
			t.Errorf("expected Type %v, got %v", original.Type, unmarshaled.Type)
		}
		if len(unmarshaled.Fields) != len(original.Fields) {
			t.Errorf("expected %d fields, got %d", len(original.Fields), len(unmarshaled.Fields))
		}
		for k, v := range original.Fields {
			if unmarshaled.Fields[k] != v {
				t.Errorf("expected field %s=%v, got %v", k, v, unmarshaled.Fields[k])
			}
		}
	})

	t.Run("YAML round trip", func(t *testing.T) {
		original := Authentication{
			Type: AuthenticationTypeBasic,
			Fields: map[string]any{
				"username": "admin",
				"password": "secret",
			},
		}

		// Marshal to YAML
		data, err := yaml.Marshal(original)
		if err != nil {
			t.Errorf("unexpected marshal error: %v", err)
		}

		// Unmarshal from YAML
		var unmarshaled Authentication
		err = yaml.Unmarshal(data, &unmarshaled)
		if err != nil {
			t.Errorf("unexpected unmarshal error: %v", err)
		}

		// Verify round trip
		if unmarshaled.Type != original.Type {
			t.Errorf("expected Type %v, got %v", original.Type, unmarshaled.Type)
		}
		if len(unmarshaled.Fields) != len(original.Fields) {
			t.Errorf("expected %d fields, got %d", len(original.Fields), len(unmarshaled.Fields))
		}
		for k, v := range original.Fields {
			if unmarshaled.Fields[k] != v {
				t.Errorf("expected field %s=%v, got %v", k, v, unmarshaled.Fields[k])
			}
		}
	})

	t.Run("nil Fields marshal", func(t *testing.T) {
		original := Authentication{
			Type:   AuthenticationTypeProject,
			Fields: nil,
		}

		// Marshal to JSON
		data, err := json.Marshal(original)
		if err != nil {
			t.Errorf("unexpected marshal error: %v", err)
		}

		// Should contain only the type field
		var result map[string]any
		err = json.Unmarshal(data, &result)
		if err != nil {
			t.Errorf("unexpected unmarshal error: %v", err)
		}

		if result["type"] != "project" {
			t.Errorf("expected type='project', got %v", result["type"])
		}
		if len(result) != 1 {
			t.Errorf("expected only 1 field, got %d: %v", len(result), result)
		}
	})

	t.Run("missing type field JSON", func(t *testing.T) {
		jsonData := `{"username":"admin","password":"secret"}`
		var auth Authentication
		err := json.Unmarshal([]byte(jsonData), &auth)
		if err == nil {
			t.Errorf("expected error for missing type field, got none")
		}
		if err != nil && err.Error() != "authentication type is required" {
			t.Errorf("expected specific error message, got: %v", err)
		}
	})

	t.Run("non-string type field JSON", func(t *testing.T) {
		jsonData := `{"type":123,"username":"admin"}`
		var auth Authentication
		err := json.Unmarshal([]byte(jsonData), &auth)
		if err == nil {
			t.Errorf("expected error for non-string type field, got none")
		}
		if err != nil && err.Error() != "authentication type must be a string, got float64" {
			t.Errorf("expected specific error message, got: %v", err)
		}
	})

	t.Run("unknown type value JSON", func(t *testing.T) {
		jsonData := `{"type":"unknown","username":"admin"}`
		var auth Authentication
		err := json.Unmarshal([]byte(jsonData), &auth)
		if err == nil {
			t.Errorf("expected error for unknown type value, got none")
		}
		if err != nil && err.Error() != "unknown authentication type: unknown. should be one of project, bearer, basic, or header" {
			t.Errorf("expected specific error message, got: %v", err)
		}
	})

	t.Run("YAML missing type field", func(t *testing.T) {
		yamlData := `username: admin
password: secret`
		var auth Authentication
		err := yaml.Unmarshal([]byte(yamlData), &auth)
		if err == nil {
			t.Errorf("expected error for missing type field, got none")
		}
		if err != nil && err.Error() != "authentication type is required" {
			t.Errorf("expected specific error message, got: %v", err)
		}
	})

	t.Run("all authentication types", func(t *testing.T) {
		types := []struct {
			name     string
			authType AuthenticationType
			jsonType string
		}{
			{"project", AuthenticationTypeProject, "project"},
			{"bearer", AuthenticationTypeBearer, "bearer"},
			{"basic", AuthenticationTypeBasic, "basic"},
			{"header", AuthenticationTypeHeader, "header"},
		}

		for _, tt := range types {
			t.Run(tt.name, func(t *testing.T) {
				jsonData := `{"type":"` + tt.jsonType + `","field1":"value1"}`
				var auth Authentication
				err := json.Unmarshal([]byte(jsonData), &auth)
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if auth.Type != tt.authType {
					t.Errorf("expected Type %v, got %v", tt.authType, auth.Type)
				}
				if auth.Fields["field1"] != "value1" {
					t.Errorf("expected field1='value1', got %v", auth.Fields["field1"])
				}
			})
		}
	})
}
