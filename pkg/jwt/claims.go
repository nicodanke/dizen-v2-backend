// Package jwt issues and validates the access tokens, signed with EdDSA (Ed25519), with
// key rotation through kid and an internal JWKS (RF-14).
//
// Ed25519 rather than RSA or HMAC: signatures are 64 bytes and verification is fast, which
// matters when every request carries one; and it is asymmetric, so a service that only
// verifies never holds the key that could mint tokens.
package jwt

import (
	"slices"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims are the ones RF-14 fixes.
//
// The names are short because they travel on every request: a JWT is sent in a header, and
// three characters saved per claim is real bytes on a mobile connection.
type Claims struct {
	jwt.RegisteredClaims

	// SessionVersion is the user's session_version at issuance time. If the stored value
	// has moved on, every token issued before is invalid: this is what makes revocation
	// take effect in under a second without a lookup per request (01 section 6).
	SessionVersion int32 `json:"sv"`

	// SessionID identifies the session the token belongs to, so one device can be revoked
	// without touching the others.
	SessionID string `json:"sid"`

	// Scope is the permission set. Empty for an app user, populated for an administrator
	// (PRD-14).
	Scope []string `json:"scope,omitempty"`
}

// Subject returns the user identifier.
func (c Claims) Subject() string {
	return c.RegisteredClaims.Subject
}

// ID returns the unique token identifier (jti), used to revoke a single token.
func (c Claims) ID() string {
	return c.RegisteredClaims.ID
}

// ExpiresAt returns the expiry.
func (c Claims) ExpiresAt() time.Time {
	if c.RegisteredClaims.ExpiresAt == nil {
		return time.Time{}
	}

	return c.RegisteredClaims.ExpiresAt.Time
}

// IssuedAt returns the issuance time.
func (c Claims) IssuedAt() time.Time {
	if c.RegisteredClaims.IssuedAt == nil {
		return time.Time{}
	}

	return c.RegisteredClaims.IssuedAt.Time
}

// HasScope reports whether the token carries a permission.
func (c Claims) HasScope(scope string) bool {
	return slices.Contains(c.Scope, scope)
}

// TokenRequest is what a caller supplies to mint a token.
type TokenRequest struct {
	// UserID becomes the sub claim.
	UserID string

	// SessionID becomes the sid claim.
	SessionID string

	// SessionVersion becomes the sv claim.
	SessionVersion int32

	// Scope becomes the scope claim.
	Scope []string

	// Audience overrides the issuer's default audience.
	Audience []string
}
