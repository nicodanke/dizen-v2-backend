package jwt

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/google/uuid"
)

// DefaultAccessTokenTTL is the access token lifetime (03 section 2: 15 minutes).
//
// It is short because revocation is checked against session_version, and the window
// between a revocation and the last valid token expiring is exactly this.
const DefaultAccessTokenTTL = 15 * time.Minute

// Config describes issuance and validation.
type Config struct {
	// Issuer is the iss claim, and what a verifier requires.
	Issuer string `env:"JWT_ISSUER" envDefault:"dizen" validate:"required"`

	// Audience is the default aud claim.
	Audience []string `env:"JWT_AUDIENCE" envSeparator:"," envDefault:"dizen-app"`

	// AccessTokenTTL is the access token lifetime.
	AccessTokenTTL time.Duration `env:"JWT_ACCESS_TOKEN_TTL" envDefault:"15m"`

	// PrivateKeyPEM is the active signing key, in PKCS#8 PEM. Only the service that
	// issues tokens sets it.
	PrivateKeyPEM string `env:"JWT_PRIVATE_KEY_PEM"`

	// PublicKeysPEM are the verification keys, PKIX PEM, concatenated. Retired keys stay
	// here until every token they signed has expired.
	PublicKeysPEM string `env:"JWT_PUBLIC_KEYS_PEM"`

	// ClockSkew is how much clock drift is tolerated when checking exp and iat. Without
	// it, a server a second behind rejects tokens that are perfectly valid.
	ClockSkew time.Duration `env:"JWT_CLOCK_SKEW" envDefault:"30s"`
}

// Issuer mints access tokens.
type Issuer struct {
	keys *KeySet
	cfg  Config

	// now is injectable so expiry can be tested without waiting.
	now func() time.Time
}

// NewIssuer builds the issuer over a key set that must contain an active signing key.
func NewIssuer(keys *KeySet, cfg Config) (*Issuer, error) {
	if _, err := keys.Active(); err != nil {
		return nil, err
	}

	if cfg.AccessTokenTTL <= 0 {
		cfg.AccessTokenTTL = DefaultAccessTokenTTL
	}

	return &Issuer{keys: keys, cfg: cfg, now: time.Now}, nil
}

// Issue mints an access token.
//
// The kid goes in the header so a verifier can pick the key without trying each one, which
// is what makes rotation cheap. The jti is a fresh uuid, so a single token can be revoked
// without touching the session.
func (i *Issuer) Issue(req TokenRequest) (string, Claims, error) {
	key, err := i.keys.Active()
	if err != nil {
		return "", Claims{}, err
	}

	now := i.now()

	audience := req.Audience
	if len(audience) == 0 {
		audience = i.cfg.Audience
	}

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   req.UserID,
			Issuer:    i.cfg.Issuer,
			Audience:  audience,
			ID:        uuid.NewString(),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(i.cfg.AccessTokenTTL)),
		},
		SessionID:      req.SessionID,
		SessionVersion: req.SessionVersion,
		Scope:          req.Scope,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = key.ID

	signed, err := token.SignedString(key.Private)
	if err != nil {
		return "", Claims{}, fmt.Errorf("signing the token: %w", err)
	}

	return signed, claims, nil
}

// TTL returns the access token lifetime, which the response reports as expires_in.
func (i *Issuer) TTL() time.Duration {
	return i.cfg.AccessTokenTTL
}
