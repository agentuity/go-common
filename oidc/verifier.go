package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/agentuity/go-common/logger"
	"github.com/golang-jwt/jwt/v5"
)

// DefaultIssuer is the default Agentuity OIDC provider issuer URL.
const DefaultIssuer = "https://auth.agentuity.cloud"

var (
	// ErrTokenExpired indicates the token's exp claim is in the past.
	ErrTokenExpired = errors.New("oidc: token expired")

	// ErrTokenNotYetValid indicates the token's nbf claim is in the future.
	ErrTokenNotYetValid = errors.New("oidc: token not yet valid")

	// ErrInvalidIssuer indicates the token's iss claim doesn't match the expected issuer.
	ErrInvalidIssuer = errors.New("oidc: invalid issuer")

	// ErrInvalidAudience indicates the token's aud claim doesn't match any expected audience.
	ErrInvalidAudience = errors.New("oidc: invalid audience")

	// ErrInvalidSignature indicates the token's signature could not be verified.
	ErrInvalidSignature = errors.New("oidc: invalid signature")

	// ErrNoMatchingKey indicates no key in the JWKS matched the token's kid header.
	ErrNoMatchingKey = errors.New("oidc: no matching key found in JWKS")

	// ErrMalformedToken indicates the token could not be parsed.
	ErrMalformedToken = errors.New("oidc: malformed token")

	// ErrDiscoveryFailed indicates the OIDC discovery document could not be fetched.
	ErrDiscoveryFailed = errors.New("oidc: discovery failed")
)

// discoveryDocument represents the OpenID Connect Discovery 1.0 document.
type discoveryDocument struct {
	Issuer  string `json:"issuer"`
	JWKSURI string `json:"jwks_uri"`
}

// Verifier validates OIDC tokens issued by an Agentuity auth provider.
//
// It discovers the provider's JWKS endpoint from the OpenID Connect discovery
// document, caches the keys, and validates both the signature and standard claims
// (iss, aud, exp, iat, nbf) of incoming tokens.
//
// Key rotation is handled transparently: if a token's kid doesn't match any
// cached key, the JWKS is refreshed before returning an error.
type Verifier struct {
	issuer     string
	audiences  []string
	jwksURL    string
	httpClient *http.Client
	logger     logger.Logger
	clockSkew  time.Duration
	cacheTTL   time.Duration
	cache      *jwksCache
	discoverMu sync.Mutex
	discovered bool
}

// NewVerifier creates a new OIDC token verifier.
//
// By default it uses DefaultIssuer ("https://auth.agentuity.cloud") as the
// issuer URL. Use WithIssuer to override this for custom or local deployments.
//
// The verifier discovers the JWKS endpoint from {issuer}/.well-known/openid-configuration.
//
// Options can configure issuer, audience validation, HTTP client, logger, and cache settings.
func NewVerifier(opts ...Option) (*Verifier, error) {
	v := &Verifier{
		issuer:     DefaultIssuer,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		cacheTTL:   5 * time.Minute,
		clockSkew:  30 * time.Second,
	}

	for _, opt := range opts {
		opt(v)
	}

	v.issuer = strings.TrimRight(v.issuer, "/")
	if v.issuer == "" {
		return nil, fmt.Errorf("oidc: issuer URL is required")
	}

	// Discover JWKS URL if not explicitly set
	if v.jwksURL == "" {
		v.jwksURL = v.issuer + "/.well-known/openid-configuration"
	}

	v.cache = newJWKSCache(v.jwksURL, v.httpClient, v.cacheTTL)

	return v, nil
}

// VerifyToken parses and validates a JWT access token or ID token.
//
// It verifies the token's signature against the provider's JWKS keys,
// and validates the issuer, audience, and expiry claims. On success, it
// returns the parsed claims.
//
// If the token's key ID (kid) doesn't match any cached key, the JWKS cache
// is automatically refreshed to handle key rotation.
func (v *Verifier) VerifyToken(ctx context.Context, tokenString string) (*Claims, error) {
	// Ensure we have the JWKS URL resolved (discovery)
	if err := v.ensureJWKSURL(ctx); err != nil {
		return nil, err
	}

	claims := &Claims{}

	parserOpts := []jwt.ParserOption{
		jwt.WithIssuer(v.issuer),
		jwt.WithLeeway(v.clockSkew),
		jwt.WithExpirationRequired(),
	}
	if len(v.audiences) == 1 {
		// jwt/v5's WithAudience validates a single expected audience during parsing.
		parserOpts = append(parserOpts, jwt.WithAudience(v.audiences[0]))
	}

	token, err := jwt.ParseWithClaims(tokenString, claims, v.keyFunc(ctx), parserOpts...)
	if err != nil {
		// If no matching key was found, refresh the cache and retry once
		if errors.Is(err, ErrNoMatchingKey) {
			v.cache.invalidate()
			token, err = jwt.ParseWithClaims(tokenString, claims, v.keyFunc(ctx), parserOpts...)
			if err != nil {
				return nil, v.classifyError(err)
			}
		} else {
			return nil, v.classifyError(err)
		}
	}

	if !token.Valid {
		return nil, ErrMalformedToken
	}

	// If multiple audiences were configured, check the rest
	if len(v.audiences) > 1 {
		aud, _ := claims.GetAudience()
		if !audienceMatch(aud, v.audiences) {
			return nil, ErrInvalidAudience
		}
	}

	return claims, nil
}

// keyFunc returns a jwt.Keyfunc that resolves signing keys from the JWKS cache.
func (v *Verifier) keyFunc(ctx context.Context) jwt.Keyfunc {
	return func(token *jwt.Token) (any, error) {
		keys, err := v.cache.getKeys(ctx)
		if err != nil {
			return nil, fmt.Errorf("oidc: fetching JWKS: %w", err)
		}

		kid, _ := token.Header["kid"].(string)

		// Match by kid first
		if kid != "" {
			for _, key := range keys {
				if key.kid == kid {
					return key.publicKey, nil
				}
			}
		}

		// Fallback: match by algorithm if no kid match and exactly one key matches
		alg, _ := token.Header["alg"].(string)
		if kid == "" && alg != "" {
			var matches []parsedKey
			for _, key := range keys {
				if key.algorithm == alg {
					matches = append(matches, key)
				}
			}
			if len(matches) == 1 {
				return matches[0].publicKey, nil
			}
		}

		return nil, ErrNoMatchingKey
	}
}

// maxDiscoveryBodySize limits the size of the OIDC discovery document response.
const maxDiscoveryBodySize = 1 << 20 // 1 MB

// ensureJWKSURL resolves the JWKS URL from the discovery document if needed.
// Only successful discovery is cached; transient failures allow retry on
// subsequent calls so that a temporary network error does not permanently
// disable the verifier.
func (v *Verifier) ensureJWKSURL(ctx context.Context) error {
	v.discoverMu.Lock()
	defer v.discoverMu.Unlock()

	if v.discovered {
		return nil
	}

	// If the JWKS URL is already a direct jwks_uri, we're done.
	// If it ends with .well-known/openid-configuration, we need to fetch it.
	if !strings.HasSuffix(v.jwksURL, "/.well-known/openid-configuration") {
		v.discovered = true
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDiscoveryFailed, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDiscoveryFailed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("%w: status %d: %s", ErrDiscoveryFailed, resp.StatusCode, string(body))
	}

	var doc discoveryDocument
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxDiscoveryBodySize)).Decode(&doc); err != nil {
		return fmt.Errorf("%w: decoding: %v", ErrDiscoveryFailed, err)
	}

	if doc.JWKSURI == "" {
		return fmt.Errorf("%w: no jwks_uri in discovery document", ErrDiscoveryFailed)
	}

	// Update JWKS URL and cache to use the resolved URI
	v.jwksURL = doc.JWKSURI
	v.cache = newJWKSCache(doc.JWKSURI, v.httpClient, v.cacheTTL)

	if v.logger != nil {
		v.logger.Debug("oidc: discovered JWKS endpoint: %s", doc.JWKSURI)
	}

	v.discovered = true
	return nil
}

// classifyError maps jwt library errors to our sentinel errors.
func (v *Verifier) classifyError(err error) error {
	if errors.Is(err, jwt.ErrTokenExpired) {
		return ErrTokenExpired
	}
	if errors.Is(err, jwt.ErrTokenNotValidYet) {
		return ErrTokenNotYetValid
	}
	if errors.Is(err, jwt.ErrTokenSignatureInvalid) {
		return ErrInvalidSignature
	}
	if errors.Is(err, jwt.ErrTokenMalformed) {
		return ErrMalformedToken
	}
	if errors.Is(err, ErrNoMatchingKey) {
		return ErrNoMatchingKey
	}

	// Check for issuer/audience validation errors using typed checks
	if errors.Is(err, jwt.ErrTokenInvalidIssuer) {
		return fmt.Errorf("%w: %v", ErrInvalidIssuer, err)
	}
	if errors.Is(err, jwt.ErrTokenInvalidAudience) {
		return fmt.Errorf("%w: %v", ErrInvalidAudience, err)
	}

	return fmt.Errorf("oidc: token verification failed: %w", err)
}

// audienceMatch returns true if at least one token audience matches an expected audience.
func audienceMatch(tokenAud, expected []string) bool {
	for _, e := range expected {
		for _, a := range tokenAud {
			if a == e {
				return true
			}
		}
	}
	return false
}
