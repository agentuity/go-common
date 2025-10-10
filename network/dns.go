package network

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type CloudProvider string

const (
	CloudProviderAWS   CloudProvider = "aws"
	CloudProviderGCP   CloudProvider = "gcp"
	CloudProviderAzure CloudProvider = "az"
	CloudProviderLocal CloudProvider = "local"
)

type cloudMetadata struct {
	provider CloudProvider
	region   string
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

	zone := string(body)
	parts := strings.Split(zone, "/")
	if len(parts) == 0 {
		return nil, fmt.Errorf("invalid zone format: %s", zone)
	}
	fullZone := parts[len(parts)-1]
	region := extractRegion(fullZone)

	return &cloudMetadata{
		provider: CloudProviderGCP,
		region:   region,
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
	return &cloudMetadata{
		provider: CloudProviderAWS,
		region:   region,
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
	return &cloudMetadata{
		provider: CloudProviderAzure,
		region:   region,
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
		return region
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
	return fmt.Sprintf("%s-%s", metadata.provider, shortened)
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

	return fmt.Sprintf("%s.%s.%s", host, cloudID, suffix), nil
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

	return fmt.Sprintf("%s.%s.%s", host, cloudID, suffix), nil
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
