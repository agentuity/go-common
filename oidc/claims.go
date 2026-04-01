// Package oidc provides OIDC token verification for Agentuity services.
//
// It validates JWT access tokens and ID tokens issued by the Agentuity OIDC
// provider (auth.agentuity.cloud), handling JWKS discovery, key caching,
// and standard claim validation.
//
// By default, the verifier uses "https://auth.agentuity.cloud" as the issuer.
// Use WithIssuer to override for custom or local deployments.
//
// Usage:
//
//	// Default issuer (auth.agentuity.cloud):
//	verifier, err := oidc.NewVerifier(
//		oidc.WithAudience("my-client-id"),
//	)
//
//	// Custom issuer:
//	verifier, err := oidc.NewVerifier(
//		oidc.WithIssuer("https://auth.custom.example.com"),
//		oidc.WithAudience("my-client-id"),
//	)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	claims, err := verifier.VerifyToken(ctx, tokenString)
//	if err != nil {
//		// handle invalid token
//	}
//	fmt.Println("User:", claims.Subject)
package oidc

import (
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// Claims represents the claims extracted from a verified OIDC token.
// It includes both standard OIDC claims and Agentuity-specific claims.
type Claims struct {
	jwt.RegisteredClaims

	// Scope is the space-separated scopes string from the token.
	// Use Scopes() to get them as a slice.
	Scope string `json:"scope,omitempty"`

	// ClientID is the OAuth2 client that requested the token.
	ClientID string `json:"client_id,omitempty"`

	// AuthorizedParty is the party to which the token was issued.
	AuthorizedParty string `json:"azp,omitempty"`

	// Email is the user's email address (requires "email" scope).
	Email string `json:"email,omitempty"`

	// EmailVerified indicates whether the email has been verified.
	EmailVerified *bool `json:"email_verified,omitempty"`

	// Name is the user's full name (requires "profile" scope).
	Name string `json:"name,omitempty"`

	// GivenName is the user's first name (requires "profile" scope).
	GivenName string `json:"given_name,omitempty"`

	// FamilyName is the user's last name (requires "profile" scope).
	FamilyName string `json:"family_name,omitempty"`

	// Picture is the URL of the user's profile picture (requires "profile" scope).
	Picture string `json:"picture,omitempty"`

	// OrgID is the organization this token is scoped to (Agentuity-specific).
	OrgID string `json:"org_id,omitempty"`

	// Nonce is the nonce from the original auth request (ID tokens only).
	Nonce string `json:"nonce,omitempty"`

	// AtHash is the access token hash (ID tokens only).
	AtHash string `json:"at_hash,omitempty"`
}

// Scopes returns the token's scopes as a string slice.
func (c *Claims) Scopes() []string {
	if c.Scope == "" {
		return nil
	}
	return strings.Fields(c.Scope)
}

// HasScope returns true if the token includes the given scope.
func (c *Claims) HasScope(scope string) bool {
	for _, s := range c.Scopes() {
		if s == scope {
			return true
		}
	}
	return false
}

// IsClientCredentials returns true if this token was issued via the
// client_credentials grant (subject equals client ID, no user identity).
func (c *Claims) IsClientCredentials() bool {
	sub, _ := c.GetSubject()
	return sub != "" && sub == c.ClientID
}
