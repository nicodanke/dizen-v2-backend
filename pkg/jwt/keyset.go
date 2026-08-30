package jwt

import (
	"crypto"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"sync"
)

// Errors the key set reports.
var (
	// ErrNoSigningKey means no active key was configured, so nothing can be issued.
	ErrNoSigningKey = errors.New("jwt: no active signing key")

	// ErrUnknownKey means the token names a kid the service does not know. It is what a
	// token signed with a retired key produces.
	ErrUnknownKey = errors.New("jwt: unknown kid")

	// ErrInvalidKey means the configured material is not a usable Ed25519 key.
	ErrInvalidKey = errors.New("jwt: invalid key")
)

// Key is one key pair in the set.
type Key struct {
	// ID is the kid carried in the token header, which is what lets a verifier pick the
	// right key without trying them all.
	ID string

	// Public is always present: a verifying service holds only this half.
	Public ed25519.PublicKey

	// Private is present only in the service that issues tokens.
	Private ed25519.PrivateKey
}

// CanSign reports whether the key can issue tokens.
func (k Key) CanSign() bool {
	return len(k.Private) > 0
}

// KeySet holds the keys and which one is active.
//
// Rotation works by adding the new key as active while keeping the previous ones for
// verification: tokens already issued stay valid until they expire, and no user is signed
// out by a key change.
type KeySet struct {
	mu sync.RWMutex

	keys      map[string]Key
	activeKID string
}

// NewKeySet builds an empty set.
func NewKeySet() *KeySet {
	return &KeySet{keys: make(map[string]Key)}
}

// Add registers a key. The first key that can sign becomes the active one unless SetActive
// says otherwise.
func (s *KeySet) Add(key Key) error {
	if len(key.Public) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: the public key of %q is not Ed25519", ErrInvalidKey, key.ID)
	}

	if key.ID == "" {
		key.ID = KeyID(key.Public)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.keys[key.ID] = key

	if s.activeKID == "" && key.CanSign() {
		s.activeKID = key.ID
	}

	return nil
}

// SetActive chooses the key used to sign new tokens.
func (s *KeySet) SetActive(kid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key, ok := s.keys[kid]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownKey, kid)
	}

	if !key.CanSign() {
		return fmt.Errorf("%w: %s has no private half", ErrNoSigningKey, kid)
	}

	s.activeKID = kid

	return nil
}

// Active returns the signing key.
func (s *KeySet) Active() (Key, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.activeKID == "" {
		return Key{}, ErrNoSigningKey
	}

	return s.keys[s.activeKID], nil
}

// Get returns the key with the given kid.
func (s *KeySet) Get(kid string) (Key, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key, ok := s.keys[kid]
	if !ok {
		return Key{}, fmt.Errorf("%w: %s", ErrUnknownKey, kid)
	}

	return key, nil
}

// Keys returns every key in the set, which is what the JWKS is built from.
func (s *KeySet) Keys() []Key {
	s.mu.RLock()
	defer s.mu.RUnlock()

	keys := make([]Key, 0, len(s.keys))
	for _, key := range s.keys {
		keys = append(keys, key)
	}

	return keys
}

// KeyID derives a stable kid from a public key.
//
// Deriving it rather than configuring it means two services given the same key always
// agree on its name, so a rotation cannot leave the issuer and the verifier disagreeing
// about which key is which.
func KeyID(public ed25519.PublicKey) string {
	sum := sha256.Sum256(public)

	return base64.RawURLEncoding.EncodeToString(sum[:8])
}

// GenerateKey creates a new pair, used in development and in the tests.
func GenerateKey() (Key, error) {
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		return Key{}, fmt.Errorf("generating the Ed25519 key: %w", err)
	}

	return Key{ID: KeyID(public), Public: public, Private: private}, nil
}

// ParsePrivateKeyPEM reads a PKCS#8 PEM private key, the format the environment variable
// carries.
func ParsePrivateKeyPEM(data []byte) (Key, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return Key{}, fmt.Errorf("%w: the private key is not valid PEM", ErrInvalidKey)
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return Key{}, fmt.Errorf("%w: %w", ErrInvalidKey, err)
	}

	private, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return Key{}, fmt.Errorf("%w: the key is %T, not Ed25519", ErrInvalidKey, parsed)
	}

	public, ok := private.Public().(ed25519.PublicKey)
	if !ok {
		return Key{}, fmt.Errorf("%w: the public half is not Ed25519", ErrInvalidKey)
	}

	return Key{ID: KeyID(public), Public: public, Private: private}, nil
}

// ParsePublicKeyPEM reads a PKIX PEM public key, which is all a verifying service needs.
func ParsePublicKeyPEM(data []byte) (Key, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return Key{}, fmt.Errorf("%w: the public key is not valid PEM", ErrInvalidKey)
	}

	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return Key{}, fmt.Errorf("%w: %w", ErrInvalidKey, err)
	}

	public, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return Key{}, fmt.Errorf("%w: the key is %T, not Ed25519", ErrInvalidKey, parsed)
	}

	return Key{ID: KeyID(public), Public: public}, nil
}

// EncodePrivateKeyPEM renders a private key as PKCS#8 PEM, used to seed a development
// environment.
func EncodePrivateKeyPEM(key Key) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key.Private)
	if err != nil {
		return nil, fmt.Errorf("encoding the private key: %w", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

// EncodePublicKeyPEM renders a public key as PKIX PEM.
func EncodePublicKeyPEM(key Key) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(key.Public)
	if err != nil {
		return nil, fmt.Errorf("encoding the public key: %w", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

// ensure the Ed25519 signer interface is satisfied at compile time.
var _ crypto.Signer = ed25519.PrivateKey(nil)
