package authentication

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/xhit/go-str2duration/v2"
)

var (
	ErrTokenExpired = errors.New("token expired")
	ErrInvalidToken = errors.New("invalid token")
)

type TokenOpt func(*tokenOpts) error

type tokenOpts struct {
	nonce      string
	expiration time.Time
}

func WithExpiration(expiration time.Time) TokenOpt {
	exp := time.Until(expiration)
	if exp < 0 {
		return func(opts *tokenOpts) error {
			return fmt.Errorf("expiration time is in the past")
		}
	}
	if exp > 365*24*time.Hour {
		return func(opts *tokenOpts) error {
			return fmt.Errorf("expiration time exceeds maximum of 1 year")
		}
	}
	roundedExp := exp.Round(time.Minute)
	if roundedExp < time.Minute {
		roundedExp = time.Minute
	}
	return WithNonce(str2duration.String(roundedExp) + "." + strconv.FormatInt(time.Now().Unix(), 10))
}

// WithNonce is a TokenOpt that sets the nonce for the token
//
// Deprecated: it is better to pass no token and let the function generate a random token
func WithNonce(nonce string) TokenOpt {
	return func(opts *tokenOpts) error {
		opts.nonce = nonce
		return nil
	}
}

// NewBearerToken generates a new bearer token
func NewBearerToken(sharedSecret string, opts ...TokenOpt) (string, error) {
	hash := sha256.New()

	tokenOpts := tokenOpts{}
	for _, opt := range opts {
		if err := opt(&tokenOpts); err != nil {
			return "", err
		}
	}
	nonce := tokenOpts.nonce
	if nonce == "" {
		// use a random string
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		nonce = fmt.Sprintf("%x", buf)
	}
	if _, err := hash.Write([]byte(sharedSecret + "." + nonce)); err != nil {
		return "", fmt.Errorf("error hashing token: %w", err)
	}
	secret := hash.Sum(nil)
	tok2 := base64.StdEncoding.EncodeToString(secret)
	return nonce + "." + tok2, nil
}

func ValidateToken(sharedSecret string, auth string) error {
	if len(auth) < 32 {
		return ErrInvalidToken
	}
	tok := strings.Split(auth, ".")

	var expiration time.Time
	var token string
	var encoded string

	switch len(tok) {
	case 2:
		// this is the non expiring token format
		token = tok[0] // this is the value we're going to hash with our shared secret
		expiration = time.Now().Add(time.Hour)
		encoded = tok[1]
	case 3:
		// this is the case for the new token format
		tokenExpr := tok[0]
		if tokenExpr == "" {
			return ErrInvalidToken
		}
		dur, err := str2duration.ParseDuration(tokenExpr)
		if err != nil {
			return ErrInvalidToken
		}
		token = tok[0] + "." + tok[1]
		tv, err := strconv.ParseInt(tok[1], 10, 64)
		if err != nil {
			return ErrInvalidToken
		}
		issueTime := time.Unix(tv, 0)
		expiration = issueTime.Add(dur)
		encoded = tok[2]
	default:
		return ErrInvalidToken
	}

	encodeValue, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return ErrInvalidToken
	}

	// see if we can hash the token with our shared secret to get the same value as the second token
	hash := sha256.New()
	hash.Write([]byte(sharedSecret + "." + token))
	secret := hash.Sum(nil)

	// if the two values are not the same, return an error
	if subtle.ConstantTimeCompare(secret, []byte(encodeValue)) == 0 {
		return ErrInvalidToken
	}

	if expiration.Before(time.Now()) {
		return ErrTokenExpired
	}

	return nil
}
