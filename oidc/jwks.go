package oidc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// jwkSet represents a JSON Web Key Set document.
type jwkSet struct {
	Keys []jwk `json:"keys"`
}

// jwk represents a single JSON Web Key.
type jwk struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`

	// RSA parameters
	N string `json:"n,omitempty"`
	E string `json:"e,omitempty"`

	// EC parameters
	Crv string `json:"crv,omitempty"`
	X   string `json:"x,omitempty"`
	Y   string `json:"y,omitempty"`
}

// parsedKey holds a parsed public key with its metadata.
type parsedKey struct {
	kid       string
	algorithm string
	publicKey any
}

// jwksCache manages JWKS key fetching and caching.
type jwksCache struct {
	jwksURL    string
	httpClient *http.Client
	ttl        time.Duration

	mu      sync.RWMutex
	keys    []parsedKey
	expires time.Time
}

func newJWKSCache(jwksURL string, httpClient *http.Client, ttl time.Duration) *jwksCache {
	return &jwksCache{
		jwksURL:    jwksURL,
		httpClient: httpClient,
		ttl:        ttl,
	}
}

// getKeys returns the cached keys, fetching fresh ones if the cache is stale.
func (c *jwksCache) getKeys(ctx context.Context) ([]parsedKey, error) {
	c.mu.RLock()
	if len(c.keys) > 0 && time.Now().Before(c.expires) {
		keys := c.keys
		c.mu.RUnlock()
		return keys, nil
	}
	c.mu.RUnlock()

	return c.refresh(ctx)
}

// refresh fetches fresh keys from the JWKS endpoint.
func (c *jwksCache) refresh(ctx context.Context) ([]parsedKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if len(c.keys) > 0 && time.Now().Before(c.expires) {
		return c.keys, nil
	}

	keys, err := c.fetchKeys(ctx)
	if err != nil {
		return nil, err
	}

	c.keys = keys
	c.expires = time.Now().Add(c.ttl)
	return keys, nil
}

// invalidate clears the key cache, forcing a refresh on next access.
func (c *jwksCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys = nil
	c.expires = time.Time{}
}

// fetchKeys retrieves and parses the JWKS document from the provider.
func (c *jwksCache) fetchKeys(ctx context.Context) ([]parsedKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.jwksURL, nil)
	if err != nil {
		return nil, fmt.Errorf("oidc: creating JWKS request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc: fetching JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("oidc: JWKS endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var keySet jwkSet
	if err := json.NewDecoder(resp.Body).Decode(&keySet); err != nil {
		return nil, fmt.Errorf("oidc: decoding JWKS: %w", err)
	}

	keys := make([]parsedKey, 0, len(keySet.Keys))
	for _, k := range keySet.Keys {
		if k.Use != "" && k.Use != "sig" {
			continue // skip encryption keys
		}
		pk, err := parseJWK(k)
		if err != nil {
			continue // skip keys we can't parse
		}
		keys = append(keys, pk)
	}

	if len(keys) == 0 {
		return nil, fmt.Errorf("oidc: no usable signing keys found in JWKS")
	}

	return keys, nil
}

// parseJWK converts a JWK JSON structure into a parsed key.
func parseJWK(k jwk) (parsedKey, error) {
	switch k.Kty {
	case "EC":
		key, err := parseECKey(k)
		if err != nil {
			return parsedKey{}, err
		}
		return parsedKey{kid: k.Kid, algorithm: k.Alg, publicKey: key}, nil
	case "RSA":
		key, err := parseRSAKey(k)
		if err != nil {
			return parsedKey{}, err
		}
		return parsedKey{kid: k.Kid, algorithm: k.Alg, publicKey: key}, nil
	default:
		return parsedKey{}, fmt.Errorf("oidc: unsupported key type: %s", k.Kty)
	}
}

// parseECKey parses an EC public key from JWK parameters.
func parseECKey(k jwk) (*ecdsa.PublicKey, error) {
	var curve elliptic.Curve
	switch k.Crv {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return nil, fmt.Errorf("oidc: unsupported EC curve: %s", k.Crv)
	}

	xBytes, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, fmt.Errorf("oidc: decoding EC X coordinate: %w", err)
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(k.Y)
	if err != nil {
		return nil, fmt.Errorf("oidc: decoding EC Y coordinate: %w", err)
	}

	return &ecdsa.PublicKey{
		Curve: curve,
		X:     new(big.Int).SetBytes(xBytes),
		Y:     new(big.Int).SetBytes(yBytes),
	}, nil
}

// parseRSAKey parses an RSA public key from JWK parameters.
func parseRSAKey(k jwk) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("oidc: decoding RSA N: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("oidc: decoding RSA E: %w", err)
	}

	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(new(big.Int).SetBytes(eBytes).Int64()),
	}, nil
}
