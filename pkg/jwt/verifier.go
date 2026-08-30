package jwt

import (
	"errors"
	"fmt"
	"slices"

	"github.com/golang-jwt/jwt/v5"
)

// Errors the verifier reports. They are distinct because the app reacts differently to
// each: an expired token is refreshed, an invalid one signs the user out.
var (
	// ErrExpired means the token is past its expiry.
	ErrExpired = errors.New("jwt: the token has expired")

	// ErrInvalidToken means the token is malformed, badly signed, or fails a claim check.
	ErrInvalidToken = errors.New("jwt: invalid token")

	// ErrMissingKID means the header carries no kid, so no key can be selected.
	ErrMissingKID = errors.New("jwt: the token carries no kid")

	// ErrWrongAlgorithm means the token is signed with something other than EdDSA.
	ErrWrongAlgorithm = errors.New("jwt: unexpected signing algorithm")
)

// Verifier validates tokens against the key set.
type Verifier struct {
	keys *KeySet
	cfg  Config
}

// NewVerifier builds the verifier. It needs only public keys, which is why a service that
// merely validates never holds the material that could mint a token.
func NewVerifier(keys *KeySet, cfg Config) *Verifier {
	return &Verifier{keys: keys, cfg: cfg}
}

// Verify parses and validates a token, returning its claims.
//
// It checks the signature, the algorithm, the expiry, the issuer and the audience. What it
// does NOT check is session_version: that needs the current value from Redis, and belongs
// to the auth interceptor, which has it.
func (v *Verifier) Verify(token string) (Claims, error) {
	parsed, err := jwt.ParseWithClaims(token, &Claims{}, v.keyFor,
		// Pinning the algorithm is what closes the algorithm confusion attack: without
		// it, a token declaring "none" or HS256 signed with the public key would parse.
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuer(v.cfg.Issuer),
		jwt.WithLeeway(v.cfg.ClockSkew),
	)
	if err != nil {
		return Claims{}, translate(err)
	}

	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return Claims{}, ErrInvalidToken
	}

	if err := v.checkAudience(*claims); err != nil {
		return Claims{}, err
	}

	return *claims, nil
}

// keyFor selects the verification key from the kid in the header.
func (v *Verifier) keyFor(token *jwt.Token) (any, error) {
	if _, ok := token.Method.(*jwt.SigningMethodEd25519); !ok {
		return nil, fmt.Errorf("%w: %s", ErrWrongAlgorithm, token.Method.Alg())
	}

	kid, ok := token.Header["kid"].(string)
	if !ok || kid == "" {
		return nil, ErrMissingKID
	}

	key, err := v.keys.Get(kid)
	if err != nil {
		return nil, err
	}

	return key.Public, nil
}

// checkAudience verifies the token was minted for this service.
//
// It is done here rather than through jwt.WithAudience because an empty configured
// audience must mean "do not check", which the library treats as "require empty".
func (v *Verifier) checkAudience(claims Claims) error {
	if len(v.cfg.Audience) == 0 {
		return nil
	}

	for _, want := range v.cfg.Audience {
		if slices.Contains(claims.Audience, want) {
			return nil
		}
	}

	return fmt.Errorf("%w: the audience does not match", ErrInvalidToken)
}

// translate maps the library's errors onto ours, so callers branch on a stable set rather
// than on the dependency's.
func translate(err error) error {
	switch {
	case errors.Is(err, jwt.ErrTokenExpired):
		return ErrExpired
	case errors.Is(err, ErrUnknownKey), errors.Is(err, ErrMissingKID), errors.Is(err, ErrWrongAlgorithm):
		// Already ours: keyFor returned it and the library wrapped it.
		return err
	default:
		return fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
}
