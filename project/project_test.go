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
