package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/agentuity/go-common/logger"
)

// GetPublicKey fetches an ECDSA public key from the API endpoint
func GetPublicKey(ctx context.Context, logger logger.Logger, apiUrl string) (*ecdsa.PublicKey, error) {
	u, err := url.Parse(apiUrl)
	if err != nil {
		return nil, fmt.Errorf("error parsing api url: %s. %w", apiUrl, err)
	}

	u.Path = "/.well-known/ecdsa.pub.pem"

	logger.Trace("fetching agentuity api public key from %s", u.String())

	// Create HTTP client with reasonable timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("error creating GET request: %w", err)
	}
	req.Header.Set("User-Agent", "gravity-client/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d for %s", resp.StatusCode, u.String())
	}

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}

	keyPem, rest := pem.Decode(buf)
	if keyPem == nil {
		logger.Error("no valid pem data, received: %s", string(rest))
		return nil, fmt.Errorf("%s did not return valid pem data", u.String())
	}

	pubKeyAny, err := x509.ParsePKIXPublicKey(keyPem.Bytes)
	if err != nil {
		return nil, fmt.Errorf("error parsing public key: %w", err)
	}

	pubKey, ok := pubKeyAny.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key is not an ECDSA key, was %T", pubKeyAny)
	}

	return pubKey, nil
}
