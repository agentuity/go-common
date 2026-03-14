package oidc

import (
	"net/http"
	"time"

	"github.com/agentuity/go-common/logger"
)

// Option configures a Verifier.
type Option func(*Verifier)

// WithIssuer overrides the default issuer URL (DefaultIssuer).
// Use this for custom or local OIDC provider deployments.
func WithIssuer(issuerURL string) Option {
	return func(v *Verifier) {
		v.issuer = issuerURL
	}
}

// WithAudience sets the expected audience(s) for token validation.
// If multiple audiences are provided, the token must contain at least one.
// If no audience is set, audience validation is skipped.
func WithAudience(audiences ...string) Option {
	return func(v *Verifier) {
		v.audiences = audiences
	}
}

// WithHTTPClient sets a custom HTTP client for JWKS and discovery requests.
func WithHTTPClient(client *http.Client) Option {
	return func(v *Verifier) {
		v.httpClient = client
	}
}

// WithLogger sets the logger for the verifier.
func WithLogger(log logger.Logger) Option {
	return func(v *Verifier) {
		v.logger = log
	}
}

// WithCacheTTL sets how long JWKS keys are cached before being refreshed.
// Default is 5 minutes.
func WithCacheTTL(ttl time.Duration) Option {
	return func(v *Verifier) {
		v.cacheTTL = ttl
	}
}

// WithClockSkew sets the allowed clock skew for token time validation.
// Default is 30 seconds.
func WithClockSkew(d time.Duration) Option {
	return func(v *Verifier) {
		v.clockSkew = d
	}
}

// WithJWKSURL overrides the JWKS URL instead of discovering it from
// the issuer's .well-known/openid-configuration. Useful for testing
// or non-standard setups.
func WithJWKSURL(url string) Option {
	return func(v *Verifier) {
		v.jwksURL = url
	}
}
