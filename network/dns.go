package network

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	cstr "github.com/agentuity/go-common/string"
)

type CloudProvider string

const (
	CloudProviderAWS   CloudProvider = "aws"
	CloudProviderGCP   CloudProvider = "gcp"
	CloudProviderAzure CloudProvider = "az"
	CloudProviderLocal CloudProvider = "local"
)

type cloudMetadata struct {
	provider  CloudProvider
	region    string
	accountId string
}

var (
	cachedMetadata *cloudMetadata
	metadataMu     sync.RWMutex
	cloudDetector  CloudDetector = newDefaultCloudDetector()
	cloudMu        sync.RWMutex
)

type CloudDetector interface {
	Detect(ctx context.Context) (*cloudMetadata, error)
}

type defaultCloudDetector struct {
	httpClient *http.Client
}

func newDefaultCloudDetector() CloudDetector {
	return &defaultCloudDetector{
		httpClient: &http.Client{
			Timeout: 2 * time.Second,
		},
	}
}

func SetCloudDetector(detector CloudDetector) {
	metadataMu.Lock()
	cachedMetadata = nil
	metadataMu.Unlock()

	cloudMu.Lock()
	defer cloudMu.Unlock()
	cloudDetector = detector
}

func (d *defaultCloudDetector) Detect(ctx context.Context) (*cloudMetadata, error) {
	detectors := []func(context.Context) (*cloudMetadata, error){
		d.detectGCP,
		d.detectAWS,
		d.detectAzure,
	}

	for _, detect := range detectors {
		if metadata, err := detect(ctx); err == nil && metadata != nil {
			return metadata, nil
		}
	}

	return &cloudMetadata{
		provider: CloudProviderLocal,
		region:   "",
	}, nil
}

func (d *defaultCloudDetector) detectGCP(ctx context.Context) (*cloudMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://metadata.google.internal/computeMetadata/v1/instance/zone", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Metadata-Flavor", "Google")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	zone := strings.TrimSpace(string(body))
	parts := strings.Split(zone, "/")
	fullZone := parts[len(parts)-1]
	if fullZone == "" {
		return nil, fmt.Errorf("invalid zone format: %q", zone)
	}
	region := extractRegion(fullZone)

	req, err = http.NewRequestWithContext(ctx, "GET", "http://metadata.google.internal/computeMetadata/v1/project/project-id", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Metadata-Flavor", "Google")

	resp, err = d.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var projectID string
	if resp.StatusCode == http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err == nil {
			projectID = strings.TrimSpace(string(body))
		}
	}

	if projectID == "" {
		return nil, fmt.Errorf("error fetching the GCP project id")
	}

	return &cloudMetadata{
		provider:  CloudProviderGCP,
		region:    region,
		accountId: projectID,
	}, nil
}

func (d *defaultCloudDetector) detectAWS(ctx context.Context) (*cloudMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, "PUT", "http://169.254.169.254/latest/api/token", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", "21600")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get token: %d", resp.StatusCode)
	}

	token, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	req, err = http.NewRequestWithContext(ctx, "GET", "http://169.254.169.254/latest/meta-data/placement/region", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-aws-ec2-metadata-token", string(token))

	resp, err = d.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	region := string(body)

	req, err = http.NewRequestWithContext(ctx, "GET", "http://169.254.169.254/latest/dynamic/instance-identity/document", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-aws-ec2-metadata-token", string(token))

	resp, err = d.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	type instanceIdentity struct {
		AccountID string `json:"accountId"`
	}

	var accountID string
	if resp.StatusCode == http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err == nil {
			var doc instanceIdentity
			if json.Unmarshal(body, &doc) == nil {
				accountID = doc.AccountID
			}
		}
	}

	if accountID == "" {
		return nil, fmt.Errorf("error fetching the AWS account id")
	}

	return &cloudMetadata{
		provider:  CloudProviderAWS,
		region:    region,
		accountId: accountID,
	}, nil
}

func (d *defaultCloudDetector) detectAzure(ctx context.Context) (*cloudMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://169.254.169.254/metadata/instance/compute/location?api-version=2021-02-01&format=text", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Metadata", "true")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	region := string(body)

	req, err = http.NewRequestWithContext(ctx, "GET", "http://169.254.169.254/metadata/instance/compute/subscriptionId?api-version=2021-02-01&format=text", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Metadata", "true")

	resp, err = d.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var subscriptionID string
	if resp.StatusCode == http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err == nil {
			subscriptionID = strings.TrimSpace(string(body))
		}
	}

	if subscriptionID == "" {
		return nil, fmt.Errorf("error fetching the Azure subscription id")
	}

	return &cloudMetadata{
		provider:  CloudProviderAzure,
		region:    region,
		accountId: subscriptionID,
	}, nil
}

func extractRegion(zone string) string {
	if zone == "" {
		return zone
	}

	lastChar := zone[len(zone)-1]
	if len(zone) > 1 && lastChar >= 'a' && lastChar <= 'z' {
		beforeLast := zone[len(zone)-2]
		if beforeLast >= '0' && beforeLast <= '9' {
			return zone[:len(zone)-1]
		} else if beforeLast == '-' {
			return zone[:len(zone)-2]
		}
	}

	return zone
}

func shortenRegion(region string) string {
	parts := strings.Split(region, "-")
	if len(parts) < 2 {
		// No dashes - likely Azure region, apply intelligent shortening
		return shortenAzureRegion(region)
	}

	var allParts []string
	for _, part := range parts {
		splitPart := splitAlphaNumeric(part)
		allParts = append(allParts, splitPart...)
	}

	var result strings.Builder
	for i, part := range allParts {
		if i == len(allParts)-1 {
			result.WriteString(part)
		} else if len(part) <= 2 {
			result.WriteString(part)
		} else {
			result.WriteString(string(part[0]))
		}
	}

	return result.String()
}

// Azure region lookup table - maps full region names to short codes (max 5 chars)
var azureRegionShortCodes = map[string]string{
	// US regions
	"eastus":         "eus",
	"eastus2":        "eus2",
	"westus":         "wus",
	"westus2":        "wus2",
	"westus3":        "wus3",
	"centralus":      "cus",
	"northcentralus": "ncus",
	"southcentralus": "scus",
	"westcentralus":  "wcus",

	// Europe regions
	"northeurope":        "neu",
	"westeurope":         "weu",
	"francecentral":      "frc",
	"francesouth":        "frs",
	"germanywestcentral": "dewc",
	"germanynorth":       "den",
	"norwayeast":         "noe",
	"norwaywest":         "now",
	"swedencentral":      "sec",
	"switzerlandnorth":   "chn",
	"switzerlandwest":    "chw",
	"uksouth":            "uks",
	"ukwest":             "ukw",

	// Asia Pacific regions
	"eastasia":           "eas",
	"southeastasia":      "seas",
	"australiaeast":      "aue",
	"australiacentral":   "auc",
	"australiacentral2":  "auc2",
	"australiasoutheast": "ause",
	"centralindia":       "indc",
	"southindia":         "inds",
	"westindia":          "indw",
	"japaneast":          "jpe",
	"japanwest":          "jpw",
	"koreacentral":       "krc",
	"koreasouth":         "krs",

	// Middle East & Africa
	"uaenorth":         "aen",
	"uaecentral":       "aec",
	"southafricanorth": "zan",
	"southafricawest":  "zaw",
	"qatarcentral":     "qac",

	// Americas (non-US)
	"brazilsouth":     "brs",
	"brazilsoutheast": "brse",
	"canadacentral":   "cac",
	"canadaeast":      "cae",

	// Special regions
	"jioindiawest":    "jiow",
	"jioindiacentral": "jioc",

	// Stage/test environments
	"centralusstage":      "cuss",
	"eastusstage":         "euss",
	"eastus2stage":        "eus2s",
	"northcentralusstage": "ncuss",
	"southcentralusstage": "scuss",
	"westusstage":         "wuss",
	"westus2stage":        "wus2s",
	"eastusstg":           "eust",
	"southcentralusstg":   "scust",
	"eastasiastage":       "eass",
	"southeastasiastage":  "seass",

	// Geo regions (typically kept as-is or lightly shortened)
	"asia":             "asia",
	"asiapacific":      "apac",
	"australia":        "au",
	"brazil":           "br",
	"canada":           "ca",
	"europe":           "eu",
	"france":           "fr",
	"germany":          "de",
	"global":           "glbl",
	"india":            "ind",
	"japan":            "jp",
	"korea":            "kr",
	"norway":           "no",
	"singapore":        "sg",
	"southafrica":      "za",
	"switzerland":      "ch",
	"uae":              "ae",
	"uk":               "uk",
	"unitedstates":     "us",
	"unitedstateseuap": "usea",
}

func shortenAzureRegion(region string) string {
	lower := strings.ToLower(region)

	// Check lookup table first
	if short, ok := azureRegionShortCodes[lower]; ok {
		return short
	}

	// If not in table, return as-is
	return region
}

func splitAlphaNumeric(s string) []string {
	if len(s) == 0 {
		return nil
	}

	var parts []string
	var current strings.Builder
	var lastWasDigit bool

	for i, c := range s {
		isDigit := c >= '0' && c <= '9'

		if i == 0 {
			current.WriteRune(c)
			lastWasDigit = isDigit
		} else if isDigit != lastWasDigit {
			parts = append(parts, current.String())
			current.Reset()
			current.WriteRune(c)
			lastWasDigit = isDigit
		} else {
			current.WriteRune(c)
		}
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

// GetCloudIdentifier will return the cloud identifier for the current environment which is detected automatically at runtime.
func GetCloudIdentifier(ctx context.Context) (string, error) {
	metadataMu.RLock()
	if cachedMetadata != nil {
		metadata := cachedMetadata
		metadataMu.RUnlock()
		return formatCloudIdentifier(metadata), nil
	}
	metadataMu.RUnlock()

	metadataMu.Lock()
	defer metadataMu.Unlock()

	if cachedMetadata != nil {
		return formatCloudIdentifier(cachedMetadata), nil
	}

	cloudMu.RLock()
	detector := cloudDetector
	cloudMu.RUnlock()

	metadata, err := detector.Detect(ctx)
	if err != nil {
		return "", err
	}

	cachedMetadata = metadata
	return formatCloudIdentifier(metadata), nil
}

func formatCloudIdentifier(metadata *cloudMetadata) string {
	if metadata.provider == CloudProviderLocal || metadata.region == "" {
		return "local"
	}
	shortened := shortenRegion(metadata.region)
	account := cstr.CrockfordHash(metadata.accountId, 4)
	return fmt.Sprintf("%s%s-%s", metadata.provider, shortened, account)
}

// GenerateHostnameWithCloudRegion generates a hostname with cloud region information if you already have it.
func GenerateHostnameWithCloudRegion(ctx context.Context, host string, suffix string, provider CloudProvider, region string) (string, error) {
	if host == "" {
		return "", fmt.Errorf("host cannot be empty")
	}
	if suffix == "" {
		return "", fmt.Errorf("suffix cannot be empty")
	}
	if provider == "" {
		return "", fmt.Errorf("provider cannot be empty")
	}
	if region == "" {
		return "", fmt.Errorf("region cannot be empty")
	}

	cloudID := formatCloudIdentifier(&cloudMetadata{
		provider: provider,
		region:   region,
	})

	return fmt.Sprintf("%s-%s.%s", host, cloudID, suffix), nil
}

// GenerateHostname generates a hostname dynamically based on the cloud provider and region detected at runtime.
func GenerateHostname(ctx context.Context, host string, suffix string) (string, error) {
	if host == "" {
		return "", fmt.Errorf("host cannot be empty")
	}
	if suffix == "" {
		return "", fmt.Errorf("suffix cannot be empty")
	}

	cloudID, err := GetCloudIdentifier(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get cloud identifier: %w", err)
	}

	return fmt.Sprintf("%s-%s.%s", host, cloudID, suffix), nil
}

// findRegionStringForProvider finds a canonical region string for the given Region constant and CloudProvider.
func findRegionStringForProvider(region Region, provider CloudProvider) string {
	// Define provider-specific region string patterns to prefer
	var preferredPrefixes []string
	switch provider {
	case CloudProviderGCP:
		preferredPrefixes = []string{"us-central", "us-west", "us-east"}
	case CloudProviderAWS:
		preferredPrefixes = []string{"us-east-", "us-west-"}
	case CloudProviderAzure:
		preferredPrefixes = []string{"eastus", "westus", "centralus", "northcentralus", "southcentralus"}
	default:
		return ""
	}

	// First, try to find an exact match with preferred prefixes
	for regionStr, r := range Regions {
		if r == region && regionStr != "global" {
			for _, prefix := range preferredPrefixes {
				if strings.HasPrefix(regionStr, prefix) {
					return regionStr
				}
			}
		}
	}

	// If no match found, return the first region string that matches the Region constant
	// This handles cases where a provider doesn't have a region with the exact naming pattern
	for regionStr, r := range Regions {
		if r == region && regionStr != "global" {
			return regionStr
		}
	}

	return ""
}

// GenerateHostnamesForCloudRegions generates hostnames for all cloud provider and region combinations defined in ProductionRegions.
func GenerateHostnamesForCloudRegions(ctx context.Context, host string, suffix string) ([]string, error) {
	if host == "" {
		return nil, fmt.Errorf("host cannot be empty")
	}
	if suffix == "" {
		return nil, fmt.Errorf("suffix cannot be empty")
	}

	var hostnames []string

	for provider, regions := range ProductionRegions {
		for _, region := range regions {
			// Find the canonical region string for this Region constant and provider
			regionStr := findRegionStringForProvider(region, provider)
			if regionStr == "" {
				// Skip if we can't find a matching region string
				continue
			}

			hostname, err := GenerateHostnameWithCloudRegion(ctx, host, suffix, provider, regionStr)
			if err != nil {
				return nil, fmt.Errorf("failed to generate hostname for %s/%s: %w", provider, regionStr, err)
			}
			hostnames = append(hostnames, hostname)
		}
	}

	return hostnames, nil
}
