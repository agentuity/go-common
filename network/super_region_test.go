package network

import (
	"context"
	"testing"
)

func TestGetSuperRegion(t *testing.T) {
	tests := []struct {
		name          string
		region        Region
		expectedSuper SuperRegion
		shouldError   bool
	}{
		{"USCentral1", RegionUSCentral1, "usc", false},
		{"USCentral2", RegionUSCentral2, "usc", false},
		{"USWest1", RegionUSWest1, "usw", false},
		{"USWest2", RegionUSWest2, "usw", false},
		{"USEast1", RegionUSEast1, "use", false},
		{"USEast2", RegionUSEast2, "use", false},
		{"GlobalShouldError", RegionGlobal, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GetSuperRegion(tt.region)
			if tt.shouldError {
				if err == nil {
					t.Errorf("GetSuperRegion(%v) expected error, got nil", tt.region)
				}
			} else {
				if err != nil {
					t.Errorf("GetSuperRegion(%v) unexpected error: %v", tt.region, err)
				}
				if result != tt.expectedSuper {
					t.Errorf("GetSuperRegion(%v) = %v, want %v", tt.region, result, tt.expectedSuper)
				}
			}
		})
	}
}

func TestGenerateSuperRegionHostname(t *testing.T) {
	tests := []struct {
		name             string
		host             string
		suffix           string
		region           Region
		expectedHostname string
		shouldError      bool
	}{
		{
			name:             "USCentral1",
			host:             "project-123",
			suffix:           "agentuity.cloud",
			region:           RegionUSCentral1,
			expectedHostname: "project-123-usc.agentuity.cloud",
			shouldError:      false,
		},
		{
			name:             "USWest2",
			host:             "project-456",
			suffix:           "agentuity.cloud",
			region:           RegionUSWest2,
			expectedHostname: "project-456-usw.agentuity.cloud",
			shouldError:      false,
		},
		{
			name:             "USEast1",
			host:             "project-789",
			suffix:           "agentuity.cloud",
			region:           RegionUSEast1,
			expectedHostname: "project-789-use.agentuity.cloud",
			shouldError:      false,
		},
		{
			name:        "EmptyHost",
			host:        "",
			suffix:      "agentuity.cloud",
			region:      RegionUSCentral1,
			shouldError: true,
		},
		{
			name:        "EmptySuffix",
			host:        "project-123",
			suffix:      "",
			region:      RegionUSCentral1,
			shouldError: true,
		},
		{
			name:        "GlobalShouldError",
			host:        "project-123",
			suffix:      "agentuity.cloud",
			region:      RegionGlobal,
			shouldError: true,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GenerateSuperRegionHostname(ctx, tt.host, tt.suffix, tt.region)
			if tt.shouldError {
				if err == nil {
					t.Errorf("GenerateSuperRegionHostname() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("GenerateSuperRegionHostname() unexpected error: %v", err)
				}
				if result != tt.expectedHostname {
					t.Errorf("GenerateSuperRegionHostname() = %v, want %v", result, tt.expectedHostname)
				}
			}
		})
	}
}

func TestGenerateSuperRegionHostnamesForAllRegions(t *testing.T) {
	ctx := context.Background()
	hostnames, err := GenerateSuperRegionHostnamesForAllRegions(ctx, "project-123", "agentuity.cloud")
	if err != nil {
		t.Fatalf("GenerateSuperRegionHostnamesForAllRegions() error: %v", err)
	}

	// Should have one hostname per super-region (excluding global)
	expectedCount := 5 // usc, usw, use, euw, eue
	if len(hostnames) != expectedCount {
		t.Errorf("GenerateSuperRegionHostnamesForAllRegions() returned %d hostnames, want %d", len(hostnames), expectedCount)
	}

	// Verify the hostnames contain the expected super-region codes
	expectedCodes := map[string]bool{
		"project-123-usc.agentuity.cloud": true,
		"project-123-usw.agentuity.cloud": true,
		"project-123-use.agentuity.cloud": true,
		"project-123-euw.agentuity.cloud": true,
		"project-123-eue.agentuity.cloud": true,
	}

	for _, hostname := range hostnames {
		if !expectedCodes[hostname] {
			t.Errorf("GenerateSuperRegionHostnamesForAllRegions() returned unexpected hostname: %v", hostname)
		}
	}
}

func TestIsSuperRegion(t *testing.T) {
	tests := []struct {
		val      string
		expected bool
	}{
		{"usc", true},
		{"usw", true},
		{"use", true},
		{"euw", true},
		{"eue", true},
		{"l", false},
		{"", false},
		{"invalid", false},
		{"USC", false},
		{"us-central", false},
	}

	for _, tt := range tests {
		t.Run(tt.val, func(t *testing.T) {
			result := IsSuperRegion(tt.val)
			if result != tt.expected {
				t.Errorf("IsSuperRegion(%q) = %v, want %v", tt.val, result, tt.expected)
			}
		})
	}
}

func TestIsSuperRegionLocal(t *testing.T) {
	tests := []struct {
		val      string
		expected bool
	}{
		{"l", true},
		{"L", false},
		{"local", false},
		{"", false},
		{"usc", false},
		{"usw", false},
	}

	for _, tt := range tests {
		t.Run(tt.val, func(t *testing.T) {
			result := IsSuperRegionLocal(tt.val)
			if result != tt.expected {
				t.Errorf("IsSuperRegionLocal(%q) = %v, want %v", tt.val, result, tt.expected)
			}
		})
	}
}
