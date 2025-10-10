package network

import (
	"context"
	"strings"
	"testing"
)

type mockCloudDetector struct {
	metadata *cloudMetadata
	err      error
}

func (m *mockCloudDetector) Detect(ctx context.Context) (*cloudMetadata, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.metadata, nil
}

func TestGenerateHostname(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		suffix   string
		metadata *cloudMetadata
		expected string
		wantErr  bool
	}{
		{
			name:   "GCP us-central1",
			host:   "project-123",
			suffix: "agentuity.cloud",
			metadata: &cloudMetadata{
				provider: CloudProviderGCP,
				region:   "us-central1",
			},
			expected: "project-123.gcp-usc1.agentuity.cloud",
			wantErr:  false,
		},
		{
			name:   "GCP us-central2",
			host:   "project-123",
			suffix: "agentuity.cloud",
			metadata: &cloudMetadata{
				provider: CloudProviderGCP,
				region:   "us-central2",
			},
			expected: "project-123.gcp-usc2.agentuity.cloud",
			wantErr:  false,
		},
		{
			name:   "AWS us-east2",
			host:   "api-server",
			suffix: "example.com",
			metadata: &cloudMetadata{
				provider: CloudProviderAWS,
				region:   "us-east-2",
			},
			expected: "api-server.aws-use2.example.com",
			wantErr:  false,
		},
		{
			name:   "AWS us-west3",
			host:   "web-app",
			suffix: "domain.io",
			metadata: &cloudMetadata{
				provider: CloudProviderAWS,
				region:   "us-west-3",
			},
			expected: "web-app.aws-usw3.domain.io",
			wantErr:  false,
		},
		{
			name:   "Azure eastus",
			host:   "service-01",
			suffix: "cloud.net",
			metadata: &cloudMetadata{
				provider: CloudProviderAzure,
				region:   "eastus",
			},
			expected: "service-01.az-eastus.cloud.net",
			wantErr:  false,
		},
		{
			name:   "Azure westus2",
			host:   "db-primary",
			suffix: "internal.net",
			metadata: &cloudMetadata{
				provider: CloudProviderAzure,
				region:   "westus2",
			},
			expected: "db-primary.az-westus2.internal.net",
			wantErr:  false,
		},
		{
			name:   "Local environment",
			host:   "dev-server",
			suffix: "local.dev",
			metadata: &cloudMetadata{
				provider: CloudProviderLocal,
				region:   "",
			},
			expected: "dev-server.local.local.dev",
			wantErr:  false,
		},
		{
			name:     "Empty host",
			host:     "",
			suffix:   "example.com",
			metadata: &cloudMetadata{provider: CloudProviderLocal},
			wantErr:  true,
		},
		{
			name:     "Empty suffix",
			host:     "host-1",
			suffix:   "",
			metadata: &cloudMetadata{provider: CloudProviderLocal},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cachedMetadata = nil

			mock := &mockCloudDetector{
				metadata: tt.metadata,
			}
			SetCloudDetector(mock)

			result, err := GenerateHostname(context.Background(), tt.host, tt.suffix)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestShortenRegion(t *testing.T) {
	tests := []struct {
		name     string
		region   string
		expected string
	}{
		{
			name:     "us-central1",
			region:   "us-central1",
			expected: "usc1",
		},
		{
			name:     "us-central2",
			region:   "us-central2",
			expected: "usc2",
		},
		{
			name:     "us-east-2",
			region:   "us-east-2",
			expected: "use2",
		},
		{
			name:     "us-west-3",
			region:   "us-west-3",
			expected: "usw3",
		},
		{
			name:     "eu-west-1",
			region:   "eu-west-1",
			expected: "euw1",
		},
		{
			name:     "ap-southeast-2",
			region:   "ap-southeast-2",
			expected: "aps2",
		},
		{
			name:     "eastus",
			region:   "eastus",
			expected: "eastus",
		},
		{
			name:     "westus2",
			region:   "westus2",
			expected: "westus2",
		},
		{
			name:     "northeurope",
			region:   "northeurope",
			expected: "northeurope",
		},
		{
			name:     "single word",
			region:   "local",
			expected: "local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shortenRegion(tt.region)
			if result != tt.expected {
				t.Errorf("shortenRegion(%q) = %q, expected %q", tt.region, result, tt.expected)
			}
		})
	}
}

func TestExtractRegion(t *testing.T) {
	tests := []struct {
		name     string
		zone     string
		expected string
	}{
		{
			name:     "us-central1-a",
			zone:     "us-central1-a",
			expected: "us-central1",
		},
		{
			name:     "us-east-2b",
			zone:     "us-east-2b",
			expected: "us-east-2",
		},
		{
			name:     "eu-west-1c",
			zone:     "eu-west-1c",
			expected: "eu-west-1",
		},
		{
			name:     "no zone suffix",
			zone:     "us-central1",
			expected: "us-central1",
		},
		{
			name:     "no dashes",
			zone:     "local",
			expected: "local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractRegion(tt.zone)
			if result != tt.expected {
				t.Errorf("extractRegion(%q) = %q, expected %q", tt.zone, result, tt.expected)
			}
		})
	}
}

func TestFormatCloudIdentifier(t *testing.T) {
	tests := []struct {
		name     string
		metadata *cloudMetadata
		expected string
	}{
		{
			name: "GCP with region",
			metadata: &cloudMetadata{
				provider: CloudProviderGCP,
				region:   "us-central1",
			},
			expected: "gcp-usc1",
		},
		{
			name: "AWS with region",
			metadata: &cloudMetadata{
				provider: CloudProviderAWS,
				region:   "us-east-2",
			},
			expected: "aws-use2",
		},
		{
			name: "Azure with region",
			metadata: &cloudMetadata{
				provider: CloudProviderAzure,
				region:   "eastus",
			},
			expected: "az-eastus",
		},
		{
			name: "Local provider",
			metadata: &cloudMetadata{
				provider: CloudProviderLocal,
				region:   "",
			},
			expected: "local",
		},
		{
			name: "Empty region",
			metadata: &cloudMetadata{
				provider: CloudProviderGCP,
				region:   "",
			},
			expected: "local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatCloudIdentifier(tt.metadata)
			if result != tt.expected {
				t.Errorf("formatCloudIdentifier() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

func TestCachedMetadata(t *testing.T) {
	cachedMetadata = nil

	mock := &mockCloudDetector{
		metadata: &cloudMetadata{
			provider: CloudProviderGCP,
			region:   "us-central1",
		},
	}
	SetCloudDetector(mock)

	result1, err := GenerateHostname(context.Background(), "test", "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result1 != "test.gcp-usc1.example.com" {
		t.Errorf("expected test.gcp-usc1.example.com, got %q", result1)
	}

	mock.metadata = &cloudMetadata{
		provider: CloudProviderAWS,
		region:   "us-east-1",
	}

	result2, err := GenerateHostname(context.Background(), "test", "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result2 != "test.gcp-usc1.example.com" {
		t.Errorf("expected cached result test.gcp-usc1.example.com, got %q", result2)
	}
}

func TestGenerateHostnameWithCloudRegion(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		suffix   string
		provider CloudProvider
		region   string
		expected string
		wantErr  bool
	}{
		{
			name:     "GCP us-central1",
			host:     "api-server",
			suffix:   "example.com",
			provider: CloudProviderGCP,
			region:   "us-central1",
			expected: "api-server.gcp-usc1.example.com",
			wantErr:  false,
		},
		{
			name:     "AWS us-east-1",
			host:     "web-app",
			suffix:   "domain.io",
			provider: CloudProviderAWS,
			region:   "us-east-1",
			expected: "web-app.aws-use1.domain.io",
			wantErr:  false,
		},
		{
			name:     "Azure eastus",
			host:     "db-primary",
			suffix:   "cloud.net",
			provider: CloudProviderAzure,
			region:   "eastus",
			expected: "db-primary.az-eastus.cloud.net",
			wantErr:  false,
		},
		{
			name:     "Local provider",
			host:     "dev-server",
			suffix:   "local.dev",
			provider: CloudProviderLocal,
			region:   "test",
			expected: "dev-server.local.local.dev",
			wantErr:  false,
		},
		{
			name:     "GCP without zone suffix",
			host:     "worker-01",
			suffix:   "internal.net",
			provider: CloudProviderGCP,
			region:   "us-west1",
			expected: "worker-01.gcp-usw1.internal.net",
			wantErr:  false,
		},
		{
			name:     "AWS multi-part region",
			host:     "api",
			suffix:   "test.com",
			provider: CloudProviderAWS,
			region:   "ap-southeast-2",
			expected: "api.aws-aps2.test.com",
			wantErr:  false,
		},
		{
			name:     "Empty host",
			host:     "",
			suffix:   "example.com",
			provider: CloudProviderGCP,
			region:   "us-central1",
			wantErr:  true,
		},
		{
			name:     "Empty suffix",
			host:     "host-1",
			suffix:   "",
			provider: CloudProviderGCP,
			region:   "us-central1",
			wantErr:  true,
		},
		{
			name:     "Empty provider",
			host:     "host-1",
			suffix:   "example.com",
			provider: "",
			region:   "us-central1",
			wantErr:  true,
		},
		{
			name:     "Empty region",
			host:     "host-1",
			suffix:   "example.com",
			provider: CloudProviderGCP,
			region:   "",
			wantErr:  true,
		},
		{
			name:     "Complex GCP region",
			host:     "service",
			suffix:   "prod.io",
			provider: CloudProviderGCP,
			region:   "europe-west12",
			expected: "service.gcp-ew12.prod.io",
			wantErr:  false,
		},
		{
			name:     "Azure with numbers",
			host:     "cache",
			suffix:   "azure.net",
			provider: CloudProviderAzure,
			region:   "westus2",
			expected: "cache.az-westus2.azure.net",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GenerateHostnameWithCloudRegion(context.Background(), tt.host, tt.suffix, tt.provider, tt.region)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestGenerateHostnameWithCloudRegionConsistency(t *testing.T) {
	cachedMetadata = nil

	mock := &mockCloudDetector{
		metadata: &cloudMetadata{
			provider: CloudProviderGCP,
			region:   "us-central1",
		},
	}
	SetCloudDetector(mock)

	autoResult, err := GenerateHostname(context.Background(), "test", "example.com")
	if err != nil {
		t.Fatalf("GenerateHostname error: %v", err)
	}

	manualResult, err := GenerateHostnameWithCloudRegion(context.Background(), "test", "example.com", CloudProviderGCP, "us-central1")
	if err != nil {
		t.Fatalf("GenerateHostnameWithCloudRegion error: %v", err)
	}

	if autoResult != manualResult {
		t.Errorf("results should match: auto=%q, manual=%q", autoResult, manualResult)
	}

	if autoResult != "test.gcp-usc1.example.com" {
		t.Errorf("expected test.gcp-usc1.example.com, got %q", autoResult)
	}
}

func TestGenerateHostnamesForCloudRegions(t *testing.T) {
	testCases := []struct {
		name     string
		host     string
		suffix   string
		wantErr  bool
		validate func(t *testing.T, hostnames []string)
	}{
		{
			name:    "Valid host and suffix",
			host:    "api",
			suffix:  "example.com",
			wantErr: false,
			validate: func(t *testing.T, hostnames []string) {
				if len(hostnames) == 0 {
					t.Error("expected at least one hostname")
				}

				// Check that we have one hostname per provider in ProductionRegions
				expectedCount := 0
				for _, regions := range ProductionRegions {
					expectedCount += len(regions)
				}

				if len(hostnames) != expectedCount {
					t.Errorf("expected %d hostnames, got %d", expectedCount, len(hostnames))
				}

				// Verify all hostnames follow the pattern
				for _, hostname := range hostnames {
					if !strings.Contains(hostname, "api.") {
						t.Errorf("hostname %s should contain 'api.'", hostname)
					}
					if !strings.HasSuffix(hostname, ".example.com") {
						t.Errorf("hostname %s should end with '.example.com'", hostname)
					}
				}
			},
		},
		{
			name:    "Empty host",
			host:    "",
			suffix:  "example.com",
			wantErr: true,
		},
		{
			name:    "Empty suffix",
			host:    "api",
			suffix:  "",
			wantErr: true,
		},
		{
			name:    "Different host and suffix",
			host:    "db-primary",
			suffix:  "internal.net",
			wantErr: false,
			validate: func(t *testing.T, hostnames []string) {
				for _, hostname := range hostnames {
					if !strings.Contains(hostname, "db-primary.") {
						t.Errorf("hostname %s should contain 'db-primary.'", hostname)
					}
					if !strings.HasSuffix(hostname, ".internal.net") {
						t.Errorf("hostname %s should end with '.internal.net'", hostname)
					}
				}
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			hostnames, err := GenerateHostnamesForCloudRegions(context.Background(), tt.host, tt.suffix)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tt.validate != nil {
				tt.validate(t, hostnames)
			}
		})
	}
}

func TestGenerateHostnamesForCloudRegionsContent(t *testing.T) {
	hostnames, err := GenerateHostnamesForCloudRegions(context.Background(), "service", "test.io")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Track which providers we've seen
	seenProviders := make(map[CloudProvider]bool)

	for _, hostname := range hostnames {
		// Check that hostname contains one of the expected cloud prefixes
		hasValidPrefix := false
		if strings.Contains(hostname, ".gcp-") {
			seenProviders[CloudProviderGCP] = true
			hasValidPrefix = true
		} else if strings.Contains(hostname, ".aws-") {
			seenProviders[CloudProviderAWS] = true
			hasValidPrefix = true
		} else if strings.Contains(hostname, ".az-") {
			seenProviders[CloudProviderAzure] = true
			hasValidPrefix = true
		}

		if !hasValidPrefix {
			t.Errorf("hostname %s doesn't contain a valid cloud prefix (gcp-, aws-, az-)", hostname)
		}
	}

	// Verify we generated hostnames for all providers in ProductionRegions
	for provider := range ProductionRegions {
		if !seenProviders[provider] {
			t.Errorf("missing hostnames for provider %s", provider)
		}
	}
}

func TestGenerateHostnamesForCloudRegionsUniqueness(t *testing.T) {
	hostnames, err := GenerateHostnamesForCloudRegions(context.Background(), "test", "domain.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check for duplicates
	seen := make(map[string]bool)
	for _, hostname := range hostnames {
		if seen[hostname] {
			t.Errorf("duplicate hostname found: %s", hostname)
		}
		seen[hostname] = true
	}
}

func TestFindRegionStringForProvider(t *testing.T) {
	testCases := []struct {
		name             string
		region           Region
		provider         CloudProvider
		expectedContains string // What the result should contain
		shouldBeEmpty    bool
	}{
		{
			name:             "GCP RegionUSCentral1",
			region:           RegionUSCentral1,
			provider:         CloudProviderGCP,
			expectedContains: "us-central",
		},
		{
			name:             "AWS RegionUSEast1",
			region:           RegionUSEast1,
			provider:         CloudProviderAWS,
			expectedContains: "us-east-",
		},
		{
			name:             "Azure RegionUSCentral1",
			region:           RegionUSCentral1,
			provider:         CloudProviderAzure,
			expectedContains: "centralus",
		},
		{
			name:             "GCP RegionUSWest2",
			region:           RegionUSWest2,
			provider:         CloudProviderGCP,
			expectedContains: "us-west",
		},
		{
			name:             "AWS RegionUSWest2",
			region:           RegionUSWest2,
			provider:         CloudProviderAWS,
			expectedContains: "us-west-",
		},
		{
			name:          "Unknown provider",
			region:        RegionUSCentral1,
			provider:      "unknown",
			shouldBeEmpty: true,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			result := findRegionStringForProvider(tt.region, tt.provider)

			if tt.shouldBeEmpty {
				if result != "" {
					t.Errorf("expected empty string, got %q", result)
				}
				return
			}

			if result == "" {
				t.Error("expected non-empty region string")
				return
			}

			if !strings.Contains(result, tt.expectedContains) {
				t.Errorf("expected result to contain %q, got %q", tt.expectedContains, result)
			}
		})
	}
}

func TestAllAWSRegions(t *testing.T) {
	regions := map[string]string{
		"us-east-1":      "use1",
		"us-east-2":      "use2",
		"us-west-1":      "usw1",
		"us-west-2":      "usw2",
		"af-south-1":     "afs1",
		"ap-east-1":      "ape1",
		"ap-south-1":     "aps1",
		"ap-south-2":     "aps2",
		"ap-northeast-1": "apn1",
		"ap-northeast-2": "apn2",
		"ap-northeast-3": "apn3",
		"ap-southeast-1": "aps1",
		"ap-southeast-2": "aps2",
		"ap-southeast-3": "aps3",
		"ap-southeast-4": "aps4",
		"ca-central-1":   "cac1",
		"ca-west-1":      "caw1",
		"eu-central-1":   "euc1",
		"eu-central-2":   "euc2",
		"eu-west-1":      "euw1",
		"eu-west-2":      "euw2",
		"eu-west-3":      "euw3",
		"eu-south-1":     "eus1",
		"eu-south-2":     "eus2",
		"eu-north-1":     "eun1",
		"il-central-1":   "ilc1",
		"me-south-1":     "mes1",
		"me-central-1":   "mec1",
		"sa-east-1":      "sae1",
	}

	for region, expected := range regions {
		t.Run(region, func(t *testing.T) {
			cachedMetadata = nil
			mock := &mockCloudDetector{
				metadata: &cloudMetadata{
					provider: CloudProviderAWS,
					region:   region,
				},
			}
			SetCloudDetector(mock)

			result, err := GenerateHostname(context.Background(), "test", "example.com")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			expectedHostname := "test.aws-" + expected + ".example.com"
			if result != expectedHostname {
				t.Errorf("region %s: expected %s, got %s", region, expectedHostname, result)
			}
		})
	}
}

func TestAllGCPRegions(t *testing.T) {
	regions := map[string]string{
		"us-central1":             "usc1",
		"us-central2":             "usc2",
		"us-east1":                "use1",
		"us-east4":                "use4",
		"us-east5":                "use5",
		"us-south1":               "uss1",
		"us-west1":                "usw1",
		"us-west2":                "usw2",
		"us-west3":                "usw3",
		"us-west4":                "usw4",
		"northamerica-northeast1": "nn1",
		"northamerica-northeast2": "nn2",
		"southamerica-east1":      "se1",
		"southamerica-west1":      "sw1",
		"europe-central2":         "ec2",
		"europe-north1":           "en1",
		"europe-southwest1":       "es1",
		"europe-west1":            "ew1",
		"europe-west2":            "ew2",
		"europe-west3":            "ew3",
		"europe-west4":            "ew4",
		"europe-west6":            "ew6",
		"europe-west8":            "ew8",
		"europe-west9":            "ew9",
		"europe-west10":           "ew10",
		"europe-west12":           "ew12",
		"asia-east1":              "ae1",
		"asia-east2":              "ae2",
		"asia-northeast1":         "an1",
		"asia-northeast2":         "an2",
		"asia-northeast3":         "an3",
		"asia-south1":             "as1",
		"asia-south2":             "as2",
		"asia-southeast1":         "as1",
		"asia-southeast2":         "as2",
		"australia-southeast1":    "as1",
		"australia-southeast2":    "as2",
		"me-central1":             "mec1",
		"me-central2":             "mec2",
		"me-west1":                "mew1",
		"africa-south1":           "as1",
	}

	for region, expected := range regions {
		t.Run(region, func(t *testing.T) {
			cachedMetadata = nil
			mock := &mockCloudDetector{
				metadata: &cloudMetadata{
					provider: CloudProviderGCP,
					region:   region,
				},
			}
			SetCloudDetector(mock)

			result, err := GenerateHostname(context.Background(), "test", "example.com")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			expectedHostname := "test.gcp-" + expected + ".example.com"
			if result != expectedHostname {
				t.Errorf("region %s: expected %s, got %s", region, expectedHostname, result)
			}
		})
	}
}

func TestAllAzureRegions(t *testing.T) {
	regions := map[string]string{
		"eastus":              "eastus",
		"eastus2":             "eastus2",
		"southcentralus":      "southcentralus",
		"westus2":             "westus2",
		"westus3":             "westus3",
		"australiaeast":       "australiaeast",
		"southeastasia":       "southeastasia",
		"northeurope":         "northeurope",
		"swedencentral":       "swedencentral",
		"uksouth":             "uksouth",
		"westeurope":          "westeurope",
		"centralus":           "centralus",
		"southafricanorth":    "southafricanorth",
		"centralindia":        "centralindia",
		"eastasia":            "eastasia",
		"japaneast":           "japaneast",
		"koreacentral":        "koreacentral",
		"canadacentral":       "canadacentral",
		"francecentral":       "francecentral",
		"germanywestcentral":  "germanywestcentral",
		"norwayeast":          "norwayeast",
		"switzerlandnorth":    "switzerlandnorth",
		"uaenorth":            "uaenorth",
		"brazilsouth":         "brazilsouth",
		"qatarcentral":        "qatarcentral",
		"centralusstage":      "centralusstage",
		"eastusstage":         "eastusstage",
		"eastus2stage":        "eastus2stage",
		"northcentralusstage": "northcentralusstage",
		"southcentralusstage": "southcentralusstage",
		"westusstage":         "westusstage",
		"westus2stage":        "westus2stage",
		"asia":                "asia",
		"asiapacific":         "asiapacific",
		"australia":           "australia",
		"brazil":              "brazil",
		"canada":              "canada",
		"europe":              "europe",
		"france":              "france",
		"germany":             "germany",
		"global":              "global",
		"india":               "india",
		"japan":               "japan",
		"korea":               "korea",
		"norway":              "norway",
		"singapore":           "singapore",
		"southafrica":         "southafrica",
		"switzerland":         "switzerland",
		"uae":                 "uae",
		"uk":                  "uk",
		"unitedstates":        "unitedstates",
		"unitedstateseuap":    "unitedstateseuap",
		"eastasiastage":       "eastasiastage",
		"southeastasiastage":  "southeastasiastage",
		"eastusstg":           "eastusstg",
		"southcentralusstg":   "southcentralusstg",
		"northcentralus":      "northcentralus",
		"westus":              "westus",
		"jioindiawest":        "jioindiawest",
		"westcentralus":       "westcentralus",
		"southafricawest":     "southafricawest",
		"australiacentral":    "australiacentral",
		"australiacentral2":   "australiacentral2",
		"australiasoutheast":  "australiasoutheast",
		"japanwest":           "japanwest",
		"jioindiacentral":     "jioindiacentral",
		"koreasouth":          "koreasouth",
		"southindia":          "southindia",
		"westindia":           "westindia",
		"canadaeast":          "canadaeast",
		"francesouth":         "francesouth",
		"germanynorth":        "germanynorth",
		"norwaywest":          "norwaywest",
		"switzerlandwest":     "switzerlandwest",
		"ukwest":              "ukwest",
		"uaecentral":          "uaecentral",
		"brazilsoutheast":     "brazilsoutheast",
	}

	for region, expected := range regions {
		t.Run(region, func(t *testing.T) {
			cachedMetadata = nil
			mock := &mockCloudDetector{
				metadata: &cloudMetadata{
					provider: CloudProviderAzure,
					region:   region,
				},
			}
			SetCloudDetector(mock)

			result, err := GenerateHostname(context.Background(), "test", "example.com")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			expectedHostname := "test.az-" + expected + ".example.com"
			if result != expectedHostname {
				t.Errorf("region %s: expected %s, got %s", region, expectedHostname, result)
			}
		})
	}
}
