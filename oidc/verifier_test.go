package oidc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testKeyPair generates an ECDSA P-256 key pair for testing.
func testKeyPair(t *testing.T) (*ecdsa.PrivateKey, *ecdsa.PublicKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return key, &key.PublicKey
}

// testRSAKeyPair generates an RSA key pair for testing.
func testRSAKeyPair(t *testing.T) (*rsa.PrivateKey, *rsa.PublicKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return key, &key.PublicKey
}

// jwksServer creates an httptest server that serves JWKS and discovery documents.
func jwksServer(t *testing.T, keys []parsedKey) (*httptest.Server, string) {
	t.Helper()

	var jwkKeys []map[string]string
	for _, k := range keys {
		jwkMap := keyToJWK(t, k)
		jwkKeys = append(jwkKeys, jwkMap)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// We need the server URL but don't have it in the closure yet, so we use the Host header
		scheme := "http"
		issuer := fmt.Sprintf("%s://%s", scheme, r.Host)
		json.NewEncoder(w).Encode(map[string]any{
			"issuer":   issuer,
			"jwks_uri": issuer + "/.well-known/jwks.json",
		})
	})
	mux.HandleFunc("GET /.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"keys": jwkKeys,
		})
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, server.URL
}

// keyToJWK converts a parsedKey to a JWK JSON map.
func keyToJWK(t *testing.T, k parsedKey) map[string]string {
	t.Helper()
	switch pub := k.publicKey.(type) {
	case *ecdsa.PublicKey:
		return ecdsaToJWK(pub, k.kid, k.algorithm)
	case *rsa.PublicKey:
		return rsaToJWK(pub, k.kid, k.algorithm)
	default:
		t.Fatalf("unsupported key type: %T", k.publicKey)
		return nil
	}
}

func ecdsaToJWK(pub *ecdsa.PublicKey, kid, alg string) map[string]string {
	byteLen := (pub.Curve.Params().BitSize + 7) / 8
	xBytes := pub.X.Bytes()
	yBytes := pub.Y.Bytes()
	// Pad to fixed length
	xPadded := make([]byte, byteLen)
	yPadded := make([]byte, byteLen)
	copy(xPadded[byteLen-len(xBytes):], xBytes)
	copy(yPadded[byteLen-len(yBytes):], yBytes)

	return map[string]string{
		"kty": "EC",
		"kid": kid,
		"alg": alg,
		"use": "sig",
		"crv": pub.Curve.Params().Name,
		"x":   base64RawURL(xPadded),
		"y":   base64RawURL(yPadded),
	}
}

func rsaToJWK(pub *rsa.PublicKey, kid, alg string) map[string]string {
	return map[string]string{
		"kty": "RSA",
		"kid": kid,
		"alg": alg,
		"use": "sig",
		"n":   base64RawURL(pub.N.Bytes()),
		"e":   base64RawURL(big2Bytes(pub.E)),
	}
}

func big2Bytes(e int) []byte {
	b := make([]byte, 0)
	for e > 0 {
		b = append([]byte{byte(e & 0xff)}, b...)
		e >>= 8
	}
	if len(b) == 0 {
		b = []byte{0}
	}
	return b
}

func base64RawURL(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

// signToken creates a signed JWT for testing.
func signToken(t *testing.T, key any, kid string, claims jwt.Claims, method jwt.SigningMethod) string {
	t.Helper()
	token := jwt.NewWithClaims(method, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	require.NoError(t, err)
	return signed
}

func TestVerifyToken_ES256(t *testing.T) {
	privKey, pubKey := testKeyPair(t)

	keys := []parsedKey{
		{kid: "key-1", algorithm: "ES256", publicKey: pubKey},
	}
	server, issuer := jwksServer(t, keys)
	_ = server

	v, err := NewVerifier(
		WithIssuer(issuer),
		WithAudience("test-client"),
		WithCacheTTL(time.Minute),
	)
	require.NoError(t, err)

	tokenStr := signToken(t, privKey, "key-1", &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   "user-123",
			Audience:  jwt.ClaimStrings{"test-client"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		Scope:    "openid profile email",
		ClientID: "test-client",
		Email:    "user@example.com",
		Name:     "Test User",
	}, jwt.SigningMethodES256)

	claims, err := v.VerifyToken(context.Background(), tokenStr)
	require.NoError(t, err)

	assert.Equal(t, "user-123", claims.Subject)
	assert.Equal(t, "user@example.com", claims.Email)
	assert.Equal(t, "Test User", claims.Name)
	assert.Equal(t, "test-client", claims.ClientID)
	assert.Equal(t, "openid profile email", claims.Scope)
	assert.Equal(t, []string{"openid", "profile", "email"}, claims.Scopes())
	assert.True(t, claims.HasScope("openid"))
	assert.True(t, claims.HasScope("email"))
	assert.False(t, claims.HasScope("admin"))
	assert.False(t, claims.IsClientCredentials())
}

func TestVerifyToken_RS256(t *testing.T) {
	privKey, pubKey := testRSAKeyPair(t)

	keys := []parsedKey{
		{kid: "rsa-key-1", algorithm: "RS256", publicKey: pubKey},
	}
	server, issuer := jwksServer(t, keys)
	_ = server

	v, err := NewVerifier(
		WithIssuer(issuer),
		WithAudience("test-client"),
	)
	require.NoError(t, err)

	tokenStr := signToken(t, privKey, "rsa-key-1", &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   "user-456",
			Audience:  jwt.ClaimStrings{"test-client"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		Scope: "openid",
	}, jwt.SigningMethodRS256)

	claims, err := v.VerifyToken(context.Background(), tokenStr)
	require.NoError(t, err)

	assert.Equal(t, "user-456", claims.Subject)
	assert.Equal(t, []string{"openid"}, claims.Scopes())
}

func TestVerifyToken_Expired(t *testing.T) {
	privKey, pubKey := testKeyPair(t)

	keys := []parsedKey{
		{kid: "key-1", algorithm: "ES256", publicKey: pubKey},
	}
	_, issuer := jwksServer(t, keys)

	v, err := NewVerifier(
		WithIssuer(issuer),
		WithClockSkew(0), // No skew tolerance
	)
	require.NoError(t, err)

	tokenStr := signToken(t, privKey, "key-1", &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   "user-123",
			Audience:  jwt.ClaimStrings{"test-client"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	}, jwt.SigningMethodES256)

	_, err = v.VerifyToken(context.Background(), tokenStr)
	assert.ErrorIs(t, err, ErrTokenExpired)
}

func TestVerifyToken_WrongIssuer(t *testing.T) {
	privKey, pubKey := testKeyPair(t)

	keys := []parsedKey{
		{kid: "key-1", algorithm: "ES256", publicKey: pubKey},
	}
	_, issuer := jwksServer(t, keys)

	v, err := NewVerifier(WithIssuer(issuer))
	require.NoError(t, err)

	tokenStr := signToken(t, privKey, "key-1", &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "https://evil.example.com",
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}, jwt.SigningMethodES256)

	_, err = v.VerifyToken(context.Background(), tokenStr)
	assert.ErrorIs(t, err, ErrInvalidIssuer)
}

func TestVerifyToken_WrongAudience(t *testing.T) {
	privKey, pubKey := testKeyPair(t)

	keys := []parsedKey{
		{kid: "key-1", algorithm: "ES256", publicKey: pubKey},
	}
	_, issuer := jwksServer(t, keys)

	v, err := NewVerifier(
		WithIssuer(issuer), WithAudience("expected-client"))
	require.NoError(t, err)

	tokenStr := signToken(t, privKey, "key-1", &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   "user-123",
			Audience:  jwt.ClaimStrings{"wrong-client"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}, jwt.SigningMethodES256)

	_, err = v.VerifyToken(context.Background(), tokenStr)
	assert.ErrorIs(t, err, ErrInvalidAudience)
}

func TestVerifyToken_WrongSigningKey(t *testing.T) {
	// Sign with one key, serve a different one in JWKS
	signingKey, _ := testKeyPair(t)
	_, wrongPubKey := testKeyPair(t)

	keys := []parsedKey{
		{kid: "key-1", algorithm: "ES256", publicKey: wrongPubKey},
	}
	_, issuer := jwksServer(t, keys)

	v, err := NewVerifier(WithIssuer(issuer))
	require.NoError(t, err)

	tokenStr := signToken(t, signingKey, "key-1", &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}, jwt.SigningMethodES256)

	_, err = v.VerifyToken(context.Background(), tokenStr)
	assert.ErrorIs(t, err, ErrInvalidSignature)
}

func TestVerifyToken_NoMatchingKid(t *testing.T) {
	privKey, pubKey := testKeyPair(t)

	keys := []parsedKey{
		{kid: "key-1", algorithm: "ES256", publicKey: pubKey},
	}
	_, issuer := jwksServer(t, keys)

	v, err := NewVerifier(WithIssuer(issuer))
	require.NoError(t, err)

	tokenStr := signToken(t, privKey, "key-999", &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}, jwt.SigningMethodES256)

	// Should fail even after cache refresh since the kid doesn't match
	_, err = v.VerifyToken(context.Background(), tokenStr)
	assert.Error(t, err)
}

func TestVerifyToken_ClientCredentials(t *testing.T) {
	privKey, pubKey := testKeyPair(t)

	keys := []parsedKey{
		{kid: "key-1", algorithm: "ES256", publicKey: pubKey},
	}
	_, issuer := jwksServer(t, keys)

	v, err := NewVerifier(
		WithIssuer(issuer), WithAudience("my-service"))
	require.NoError(t, err)

	tokenStr := signToken(t, privKey, "key-1", &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   "my-service",
			Audience:  jwt.ClaimStrings{"my-service"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		Scope:    "openid",
		ClientID: "my-service",
	}, jwt.SigningMethodES256)

	claims, err := v.VerifyToken(context.Background(), tokenStr)
	require.NoError(t, err)
	assert.True(t, claims.IsClientCredentials())
}

func TestVerifyToken_MalformedToken(t *testing.T) {
	_, pubKey := testKeyPair(t)

	keys := []parsedKey{
		{kid: "key-1", algorithm: "ES256", publicKey: pubKey},
	}
	_, issuer := jwksServer(t, keys)

	v, err := NewVerifier(WithIssuer(issuer))
	require.NoError(t, err)

	_, err = v.VerifyToken(context.Background(), "not-a-jwt")
	assert.ErrorIs(t, err, ErrMalformedToken)
}

func TestVerifyToken_MultipleKeys(t *testing.T) {
	// Test that the verifier picks the right key when multiple are in JWKS
	privKey1, pubKey1 := testKeyPair(t)
	_, pubKey2 := testKeyPair(t)

	keys := []parsedKey{
		{kid: "key-1", algorithm: "ES256", publicKey: pubKey1},
		{kid: "key-2", algorithm: "ES256", publicKey: pubKey2},
	}
	_, issuer := jwksServer(t, keys)

	v, err := NewVerifier(WithIssuer(issuer))
	require.NoError(t, err)

	tokenStr := signToken(t, privKey1, "key-1", &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}, jwt.SigningMethodES256)

	claims, err := v.VerifyToken(context.Background(), tokenStr)
	require.NoError(t, err)
	assert.Equal(t, "user-123", claims.Subject)
}

func TestVerifyToken_WithJWKSURL(t *testing.T) {
	privKey, pubKey := testKeyPair(t)

	// Serve only the JWKS endpoint, no discovery
	mux := http.NewServeMux()
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		jwkMap := ecdsaToJWK(pubKey, "key-1", "ES256")
		json.NewEncoder(w).Encode(map[string]any{"keys": []any{jwkMap}})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	issuer := "https://auth.example.com"

	v, err := NewVerifier(
		WithIssuer(issuer),
		WithJWKSURL(server.URL+"/jwks"),
	)
	require.NoError(t, err)

	tokenStr := signToken(t, privKey, "key-1", &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}, jwt.SigningMethodES256)

	claims, err := v.VerifyToken(context.Background(), tokenStr)
	require.NoError(t, err)
	assert.Equal(t, "user-123", claims.Subject)
}

func TestClaims_Scopes(t *testing.T) {
	tests := []struct {
		name     string
		scope    string
		expected []string
	}{
		{"empty", "", nil},
		{"single", "openid", []string{"openid"}},
		{"multiple", "openid profile email", []string{"openid", "profile", "email"}},
		{"trailing space", "openid ", []string{"openid"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Claims{Scope: tt.scope}
			assert.Equal(t, tt.expected, c.Scopes())
		})
	}
}

func TestNewVerifier_EmptyIssuer(t *testing.T) {
	_, err := NewVerifier(WithIssuer(""))
	assert.Error(t, err)
}

func TestNewVerifier_TrailingSlash(t *testing.T) {
	v, err := NewVerifier(WithIssuer("https://auth.example.com/"))
	require.NoError(t, err)
	assert.Equal(t, "https://auth.example.com", v.issuer)
}

func TestNewVerifier_DefaultIssuer(t *testing.T) {
	v, err := NewVerifier()
	require.NoError(t, err)
	assert.Equal(t, DefaultIssuer, v.issuer)
}
