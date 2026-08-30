package jwt

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
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
		key, err := ParsePrivateKeyPEM([]byte(cfg.PrivateKeyPEM))
		if err != nil {
			return nil, err
		}

		if err := keys.Add(key); err != nil {
			return nil, err
		}
	}

	if cfg.PublicKeysPEM != "" {
		if err := addPublicKeys(keys, []byte(cfg.PublicKeysPEM)); err != nil {
			return nil, err
		}
	}

	return keys, nil
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
