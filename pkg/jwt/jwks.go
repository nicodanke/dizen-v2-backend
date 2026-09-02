package jwt

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// JWKSPath is where the key set is published. Only identity serves it; the other services
// carry the public keys in their configuration and validate with no network call, so a
// request never depends on identity being up.
const JWKSPath = "/.well-known/jwks.json"

// JWK is one key in JWKS form.
//
// Ed25519 is published as an OKP key (RFC 8037): kty OKP, crv Ed25519, and x carrying the
// raw public key in base64url.
type JWK struct {
	KeyType   string `json:"kty"`
	Curve     string `json:"crv"`
	KeyID     string `json:"kid"`
	Use       string `json:"use"`
	Algorithm string `json:"alg"`
	X         string `json:"x"`
}

// JWKS is the published document.
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// JWKS renders the key set. Only public halves are included: the private key never leaves
// the process that holds it.
func (s *KeySet) JWKS() JWKS {
	keys := s.Keys()

	document := JWKS{Keys: make([]JWK, 0, len(keys))}

	for _, key := range keys {
		document.Keys = append(document.Keys, JWK{
			KeyType:   "OKP",
			Curve:     "Ed25519",
			KeyID:     key.ID,
			Use:       "sig",
			Algorithm: "EdDSA",
			X:         base64.RawURLEncoding.EncodeToString(key.Public),
		})
	}

	return document
}

// JWKSHandler serves the key set over HTTP.
//
// It is cacheable for a few minutes: a rotation adds a key rather than replacing one, so a
// slightly stale copy is still correct for every token already in circulation.
func (s *KeySet) JWKSHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/jwk-set+json")
		w.Header().Set("Cache-Control", "public, max-age=300")

		_ = json.NewEncoder(w).Encode(s.JWKS())
	}
}

// LoadKeySet builds the key set from configuration.
//
// The private key, when present, becomes the active signing key; every public key stays
// available for verification. That is the whole rotation procedure: deploy the new private
// key while keeping the previous public one, and tokens already issued remain valid until
// they expire.
func LoadKeySet(cfg Config) (*KeySet, error) {
	keys := NewKeySet()

	if cfg.PrivateKeyPEM != "" {
		key, err := ParsePrivateKeyPEM(normalizePEM(cfg.PrivateKeyPEM))
		if err != nil {
			return nil, err
		}

		if err := keys.Add(key); err != nil {
			return nil, err
		}
	}

	if cfg.PublicKeysPEM != "" {
		if err := addPublicKeys(keys, normalizePEM(cfg.PublicKeysPEM)); err != nil {
			return nil, err
		}

		// A non-empty value that yields no key is a configuration error, and it has to say
		// so. Only the service that issues tokens holds a private key; the other four carry
		// public keys alone, so without this they would start with an empty key set, look
		// healthy, and reject every token they were given -- with nothing in the log to
		// explain it.
		if len(keys.Keys()) == 0 {
			return nil, fmt.Errorf("%w: the public keys are set but contain no PEM block", ErrInvalidKey)
		}
	}

	return keys, nil
}

// normalizePEM accepts a PEM whose newlines arrived escaped as the two characters `\` and
// `n`, and turns them back into real ones.
//
// That form is not a mistake to reject: it is what a .env file needs, because godotenv
// expands the escapes as it reads the file. A secrets manager expands nothing, so the same
// value arrives with literal backslashes and the PEM decoder sees garbage. The two forms
// travel through different transports and both are legitimate, so both are accepted here
// rather than in whichever of them happens to be configured today.
//
// A real PEM never contains a backslash, so the substitution cannot corrupt a valid one.
func normalizePEM(pem string) []byte {
	return []byte(strings.ReplaceAll(pem, `\n`, "\n"))
}

// addPublicKeys parses a concatenation of PEM blocks, so several retired keys fit in one
// environment variable.
func addPublicKeys(keys *KeySet, data []byte) error {
	rest := data

	for {
		key, remaining, err := nextPublicKey(rest)
		if err != nil {
			return err
		}

		if key == nil {
			return nil
		}

		// A key already present as the private half must not be downgraded to public
		// only: adding it again would drop its ability to sign.
		if existing, err := keys.Get(key.ID); err == nil && existing.CanSign() {
			rest = remaining

			continue
		}

		if err := keys.Add(*key); err != nil {
			return err
		}

		rest = remaining
	}
}
