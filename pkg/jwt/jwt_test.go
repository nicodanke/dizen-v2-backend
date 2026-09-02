package jwt_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"

	"github.com/nicodanke/dizen-v2-backend/pkg/jwt"
)

// testConfig is the configuration the tests issue and verify with.
func testConfig() jwt.Config {
	return jwt.Config{
		Issuer:         "dizen",
		Audience:       []string{"dizen-app"},
		AccessTokenTTL: 15 * time.Minute,
		ClockSkew:      30 * time.Second,
	}
}

// newIssuer builds an issuer over a freshly generated key.
func newIssuer(t *testing.T) (*jwt.Issuer, *jwt.KeySet, jwt.Key) {
	t.Helper()

	key, err := jwt.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	keys := jwt.NewKeySet()
	if err := keys.Add(key); err != nil {
		t.Fatalf("Add: %v", err)
	}

	issuer, err := jwt.NewIssuer(keys, testConfig())
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}

	return issuer, keys, key
}

// sampleRequest is a typical token request.
func sampleRequest() jwt.TokenRequest {
	return jwt.TokenRequest{
		UserID:         "0193f0a0-0000-7000-8000-000000000001",
		SessionID:      "0193f0a0-0000-7000-8000-000000000002",
		SessionVersion: 3,
		Scope:          []string{"tours:read"},
	}
}

// The claims RF-14 fixes have to survive a round trip, because every one of them is load
// bearing: sv drives revocation, sid drives per-device logout, jti drives single-token
// revocation.
func TestEveryClaimSurvivesARoundTrip(t *testing.T) {
	t.Parallel()

	issuer, keys, _ := newIssuer(t)

	token, issued, err := issuer.Issue(sampleRequest())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	claims, err := jwt.NewVerifier(keys, testConfig()).Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if claims.Subject() != sampleRequest().UserID {
		t.Errorf("sub = %q", claims.Subject())
	}

	if claims.SessionID != sampleRequest().SessionID {
		t.Errorf("sid = %q", claims.SessionID)
	}

	if claims.SessionVersion != 3 {
		t.Errorf("sv = %d, want 3", claims.SessionVersion)
	}

	if !claims.HasScope("tours:read") {
		t.Errorf("scope = %v", claims.Scope)
	}

	if claims.Issuer != "dizen" {
		t.Errorf("iss = %q", claims.Issuer)
	}

	if len(claims.Audience) == 0 || claims.Audience[0] != "dizen-app" {
		t.Errorf("aud = %v", claims.Audience)
	}

	if claims.ID() == "" {
		t.Error("jti is empty: a single token could not be revoked")
	}

	if claims.ID() != issued.ID() {
		t.Error("the returned claims do not match the token")
	}

	if claims.ExpiresAt().IsZero() || claims.IssuedAt().IsZero() {
		t.Error("exp or iat is missing")
	}
}

// Two tokens must never share a jti, or revoking one would revoke another.
func TestEachTokenGetsItsOwnJTI(t *testing.T) {
	t.Parallel()

	issuer, _, _ := newIssuer(t)

	_, first, _ := issuer.Issue(sampleRequest())
	_, second, _ := issuer.Issue(sampleRequest())

	if first.ID() == second.ID() {
		t.Error("two tokens share a jti")
	}
}

func TestAnExpiredTokenIsReportedAsExpired(t *testing.T) {
	t.Parallel()

	_, keys, key := newIssuer(t)

	// The token is built directly rather than through the issuer, which floors the
	// lifetime at its default: what is under test here is the verifier.
	expired := gojwt.NewWithClaims(gojwt.SigningMethodEdDSA, gojwt.RegisteredClaims{
		Subject:   "u1",
		Issuer:    "dizen",
		Audience:  []string{"dizen-app"},
		IssuedAt:  gojwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		ExpiresAt: gojwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
	})
	expired.Header["kid"] = key.ID

	token, err := expired.SignedString(key.Private)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	_, err = jwt.NewVerifier(keys, testConfig()).Verify(token)

	// The distinction matters: the app refreshes on expiry and signs out on anything else.
	if !errors.Is(err, jwt.ErrExpired) {
		t.Errorf("the error is not ErrExpired: %v", err)
	}
}

// Clock skew has to be tolerated: without it a server a second behind rejects tokens that
// are perfectly valid.
func TestATokenJustWithinTheClockSkewIsAccepted(t *testing.T) {
	t.Parallel()

	_, keys, key := newIssuer(t)

	justExpired := gojwt.NewWithClaims(gojwt.SigningMethodEdDSA, gojwt.RegisteredClaims{
		Subject:   "u1",
		Issuer:    "dizen",
		Audience:  []string{"dizen-app"},
		IssuedAt:  gojwt.NewNumericDate(time.Now().Add(-time.Hour)),
		ExpiresAt: gojwt.NewNumericDate(time.Now().Add(-5 * time.Second)),
	})
	justExpired.Header["kid"] = key.ID

	token, err := justExpired.SignedString(key.Private)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	// Five seconds past expiry, with a thirty second allowance.
	if _, err := jwt.NewVerifier(keys, testConfig()).Verify(token); err != nil {
		t.Errorf("a token within the clock skew was rejected: %v", err)
	}
}

// Rotation is the point of the kid: a new active key is added while the previous one stays
// for verification, so nobody is signed out by a key change.
func TestRotationKeepsTokensIssuedWithTheOldKeyValid(t *testing.T) {
	t.Parallel()

	oldKey, _ := jwt.GenerateKey()
	newKey, _ := jwt.GenerateKey()

	keys := jwt.NewKeySet()
	_ = keys.Add(oldKey)

	issuer, _ := jwt.NewIssuer(keys, testConfig())

	oldToken, _, err := issuer.Issue(sampleRequest())
	if err != nil {
		t.Fatalf("Issue with the old key: %v", err)
	}

	// The rotation: add the new key and make it active. The old one stays in the set.
	if err := keys.Add(newKey); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := keys.SetActive(newKey.ID); err != nil {
		t.Fatalf("SetActive: %v", err)
	}

	newToken, _, err := issuer.Issue(sampleRequest())
	if err != nil {
		t.Fatalf("Issue with the new key: %v", err)
	}

	verifier := jwt.NewVerifier(keys, testConfig())

	if _, err := verifier.Verify(oldToken); err != nil {
		t.Errorf("a token issued before the rotation was rejected: %v", err)
	}

	if _, err := verifier.Verify(newToken); err != nil {
		t.Errorf("a token issued after the rotation was rejected: %v", err)
	}
}

// Once the old key is retired, its tokens stop verifying. That is what makes retirement
// real rather than cosmetic.
func TestATokenSignedWithARetiredKeyIsRejected(t *testing.T) {
	t.Parallel()

	oldKey, _ := jwt.GenerateKey()

	issuingSet := jwt.NewKeySet()
	_ = issuingSet.Add(oldKey)

	issuer, _ := jwt.NewIssuer(issuingSet, testConfig())
	token, _, _ := issuer.Issue(sampleRequest())

	// A verifier that no longer knows the key.
	newKey, _ := jwt.GenerateKey()
	verifyingSet := jwt.NewKeySet()
	_ = verifyingSet.Add(jwt.Key{ID: newKey.ID, Public: newKey.Public})

	_, err := jwt.NewVerifier(verifyingSet, testConfig()).Verify(token)
	if !errors.Is(err, jwt.ErrUnknownKey) {
		t.Errorf("the error is not ErrUnknownKey: %v", err)
	}
}

// Pinning the algorithm is what closes algorithm confusion: a token declaring "none" must
// not parse, no matter how well formed it is.
func TestATokenWithAlgorithmNoneIsRejected(t *testing.T) {
	t.Parallel()

	_, keys, key := newIssuer(t)

	unsigned := gojwt.NewWithClaims(gojwt.SigningMethodNone, gojwt.RegisteredClaims{
		Subject:   "attacker",
		Issuer:    "dizen",
		ExpiresAt: gojwt.NewNumericDate(time.Now().Add(time.Hour)),
	})
	unsigned.Header["kid"] = key.ID

	token, err := unsigned.SignedString(gojwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("building the unsigned token: %v", err)
	}

	if _, err := jwt.NewVerifier(keys, testConfig()).Verify(token); err == nil {
		t.Fatal("a token with alg=none was accepted")
	}
}

// The same attack through HMAC: signing with the public key as an HMAC secret must not
// verify either.
func TestATokenSignedWithHMACIsRejected(t *testing.T) {
	t.Parallel()

	_, keys, key := newIssuer(t)

	hmacToken := gojwt.NewWithClaims(gojwt.SigningMethodHS256, gojwt.RegisteredClaims{
		Subject:   "attacker",
		Issuer:    "dizen",
		ExpiresAt: gojwt.NewNumericDate(time.Now().Add(time.Hour)),
	})
	hmacToken.Header["kid"] = key.ID

	// The attacker uses the public key, which is public by definition, as the secret.
	token, err := hmacToken.SignedString([]byte(key.Public))
	if err != nil {
		t.Fatalf("building the HMAC token: %v", err)
	}

	if _, err := jwt.NewVerifier(keys, testConfig()).Verify(token); err == nil {
		t.Fatal("a token signed with HMAC was accepted")
	}
}

func TestATamperedTokenIsRejected(t *testing.T) {
	t.Parallel()

	issuer, keys, _ := newIssuer(t)

	token, _, _ := issuer.Issue(sampleRequest())

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("the token does not have three parts: %d", len(parts))
	}

	// Flip a character of the payload.
	tampered := parts[0] + "." + parts[1][:len(parts[1])-2] + "XY." + parts[2]

	if _, err := jwt.NewVerifier(keys, testConfig()).Verify(tampered); err == nil {
		t.Fatal("a tampered token was accepted")
	}
}

func TestATokenForAnotherIssuerIsRejected(t *testing.T) {
	t.Parallel()

	key, _ := jwt.GenerateKey()
	keys := jwt.NewKeySet()
	_ = keys.Add(key)

	foreign := testConfig()
	foreign.Issuer = "somebody-else"

	issuer, _ := jwt.NewIssuer(keys, foreign)
	token, _, _ := issuer.Issue(sampleRequest())

	if _, err := jwt.NewVerifier(keys, testConfig()).Verify(token); err == nil {
		t.Fatal("a token from another issuer was accepted")
	}
}

func TestATokenForAnotherAudienceIsRejected(t *testing.T) {
	t.Parallel()

	issuer, keys, _ := newIssuer(t)

	req := sampleRequest()
	req.Audience = []string{"dizen-dashboard"}

	token, _, _ := issuer.Issue(req)

	// A verifier configured for the app must not accept a dashboard token.
	if _, err := jwt.NewVerifier(keys, testConfig()).Verify(token); err == nil {
		t.Fatal("a token for another audience was accepted")
	}
}

func TestATokenWithoutAKIDIsRejected(t *testing.T) {
	t.Parallel()

	_, keys, key := newIssuer(t)

	token := gojwt.NewWithClaims(gojwt.SigningMethodEdDSA, gojwt.RegisteredClaims{
		Subject:   "u1",
		Issuer:    "dizen",
		ExpiresAt: gojwt.NewNumericDate(time.Now().Add(time.Hour)),
	})
	// No kid header on purpose.

	signed, err := token.SignedString(key.Private)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	_, err = jwt.NewVerifier(keys, testConfig()).Verify(signed)
	if !errors.Is(err, jwt.ErrMissingKID) {
		t.Errorf("the error is not ErrMissingKID: %v", err)
	}
}

// A verifying service must be usable with public keys only: that is what stops every
// service from holding material that could mint tokens.
func TestAVerifierWorksWithPublicKeysOnly(t *testing.T) {
	t.Parallel()

	issuer, _, key := newIssuer(t)

	token, _, _ := issuer.Issue(sampleRequest())

	publicOnly := jwt.NewKeySet()
	if err := publicOnly.Add(jwt.Key{ID: key.ID, Public: key.Public}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if _, err := jwt.NewVerifier(publicOnly, testConfig()).Verify(token); err != nil {
		t.Errorf("a public-only verifier rejected a valid token: %v", err)
	}

	// And it cannot issue.
	if _, err := jwt.NewIssuer(publicOnly, testConfig()); !errors.Is(err, jwt.ErrNoSigningKey) {
		t.Errorf("a key set with no private key built an issuer: %v", err)
	}
}

// PEM is the format the environment variable carries, so the round trip has to hold.
func TestKeysRoundTripThroughPEM(t *testing.T) {
	t.Parallel()

	original, _ := jwt.GenerateKey()

	privatePEM, err := jwt.EncodePrivateKeyPEM(original)
	if err != nil {
		t.Fatalf("EncodePrivateKeyPEM: %v", err)
	}

	publicPEM, err := jwt.EncodePublicKeyPEM(original)
	if err != nil {
		t.Fatalf("EncodePublicKeyPEM: %v", err)
	}

	parsedPrivate, err := jwt.ParsePrivateKeyPEM(privatePEM)
	if err != nil {
		t.Fatalf("ParsePrivateKeyPEM: %v", err)
	}

	if parsedPrivate.ID != original.ID {
		t.Errorf("the kid changed through PEM: %q vs %q", parsedPrivate.ID, original.ID)
	}

	if !parsedPrivate.CanSign() {
		t.Error("the parsed private key cannot sign")
	}

	parsedPublic, err := jwt.ParsePublicKeyPEM(publicPEM)
	if err != nil {
		t.Fatalf("ParsePublicKeyPEM: %v", err)
	}

	if parsedPublic.ID != original.ID {
		t.Errorf("the public kid does not match: %q vs %q", parsedPublic.ID, original.ID)
	}

	if parsedPublic.CanSign() {
		t.Error("a key parsed from a public PEM claims it can sign")
	}
}

func TestParsingRejectsGarbage(t *testing.T) {
	t.Parallel()

	if _, err := jwt.ParsePrivateKeyPEM([]byte("not a pem")); !errors.Is(err, jwt.ErrInvalidKey) {
		t.Errorf("garbage was accepted as a private key: %v", err)
	}

	if _, err := jwt.ParsePublicKeyPEM([]byte("not a pem")); !errors.Is(err, jwt.ErrInvalidKey) {
		t.Errorf("garbage was accepted as a public key: %v", err)
	}
}

// The kid must be derived from the key, so two services given the same key always agree on
// its name.
func TestTheKeyIDIsDerivedFromTheKey(t *testing.T) {
	t.Parallel()

	key, _ := jwt.GenerateKey()
	other, _ := jwt.GenerateKey()

	if jwt.KeyID(key.Public) != key.ID {
		t.Error("the kid is not derived from the public key")
	}

	if jwt.KeyID(key.Public) == jwt.KeyID(other.Public) {
		t.Error("two different keys produced the same kid")
	}
}

// LoadKeySet is the whole rotation procedure as configuration: the new private key plus the
// previous public one.
func TestLoadKeySetBuildsAnIssuingAndVerifyingSet(t *testing.T) {
	t.Parallel()

	active, _ := jwt.GenerateKey()
	retired, _ := jwt.GenerateKey()

	privatePEM, _ := jwt.EncodePrivateKeyPEM(active)
	activePublicPEM, _ := jwt.EncodePublicKeyPEM(active)
	retiredPublicPEM, _ := jwt.EncodePublicKeyPEM(retired)

	cfg := testConfig()
	cfg.PrivateKeyPEM = string(privatePEM)
	// Several PEM blocks concatenated, which is how they fit in one variable.
	cfg.PublicKeysPEM = string(activePublicPEM) + string(retiredPublicPEM)

	keys, err := jwt.LoadKeySet(cfg)
	if err != nil {
		t.Fatalf("LoadKeySet: %v", err)
	}

	if len(keys.Keys()) != 2 {
		t.Fatalf("%d keys loaded, want 2", len(keys.Keys()))
	}

	// The active key must keep its private half despite also appearing as a public block.
	signing, err := keys.Active()
	if err != nil {
		t.Fatalf("Active: %v", err)
	}

	if signing.ID != active.ID {
		t.Errorf("the active key is %q, want %q", signing.ID, active.ID)
	}

	if !signing.CanSign() {
		t.Error("the active key lost its private half when the public block was added")
	}

	// And the retired key is available for verification.
	if _, err := keys.Get(retired.ID); err != nil {
		t.Errorf("the retired key was not loaded: %v", err)
	}
}

// The JWKS is what a client would use to verify without holding the keys.
func TestTheJWKSPublishesOnlyPublicKeys(t *testing.T) {
	t.Parallel()

	_, keys, key := newIssuer(t)

	rec := httptest.NewRecorder()
	keys.JWKSHandler()(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, jwt.JWKSPath, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	var document jwt.JWKS
	if err := json.Unmarshal(rec.Body.Bytes(), &document); err != nil {
		t.Fatalf("the JWKS is not valid JSON: %v", err)
	}

	if len(document.Keys) != 1 {
		t.Fatalf("%d keys published, want 1", len(document.Keys))
	}

	published := document.Keys[0]

	// Ed25519 is published as an OKP key (RFC 8037).
	if published.KeyType != "OKP" || published.Curve != "Ed25519" || published.Algorithm != "EdDSA" {
		t.Errorf("the key is not published as Ed25519 OKP: %+v", published)
	}

	if published.KeyID != key.ID {
		t.Errorf("kid = %q, want %q", published.KeyID, key.ID)
	}

	// The private half must never appear, under any field name.
	body := rec.Body.String()

	for _, forbidden := range []string{`"d"`, "PRIVATE"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the JWKS leaked private material (%s): %s", forbidden, body)
		}
	}
}

func TestSetActiveRejectsAnUnknownOrPublicOnlyKey(t *testing.T) {
	t.Parallel()

	key, _ := jwt.GenerateKey()
	publicOnly, _ := jwt.GenerateKey()

	keys := jwt.NewKeySet()
	_ = keys.Add(key)
	_ = keys.Add(jwt.Key{ID: publicOnly.ID, Public: publicOnly.Public})

	if err := keys.SetActive("does-not-exist"); !errors.Is(err, jwt.ErrUnknownKey) {
		t.Errorf("an unknown kid was accepted: %v", err)
	}

	if err := keys.SetActive(publicOnly.ID); !errors.Is(err, jwt.ErrNoSigningKey) {
		t.Errorf("a key with no private half was made active: %v", err)
	}
}

func TestAddRejectsAMalformedPublicKey(t *testing.T) {
	t.Parallel()

	keys := jwt.NewKeySet()

	if err := keys.Add(jwt.Key{ID: "x", Public: []byte("too short")}); !errors.Is(err, jwt.ErrInvalidKey) {
		t.Errorf("a malformed key was accepted: %v", err)
	}
}

// TestLoadKeySetAcceptsEscapedPEM covers the two transports a key travels through.
//
// A .env file needs the escapes, because godotenv expands them on the way in; a secrets
// manager expands nothing, so the same value arrives with literal backslashes. Both forms
// are legitimate and the loader has to take either, or a deployment fails on a key that is
// correct but was carried by the other route.
func TestLoadKeySetAcceptsEscapedPEM(t *testing.T) {
	t.Parallel()

	key, err := jwt.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	privatePEM, err := jwt.EncodePrivateKeyPEM(key)
	if err != nil {
		t.Fatalf("EncodePrivateKeyPEM: %v", err)
	}

	publicPEM, err := jwt.EncodePublicKeyPEM(key)
	if err != nil {
		t.Fatalf("EncodePublicKeyPEM: %v", err)
	}

	escape := func(pem []byte) string {
		return strings.ReplaceAll(strings.TrimSpace(string(pem)), "\n", `\n`)
	}

	cases := []struct {
		name string
		cfg  jwt.Config
	}{
		{"issuer, real newlines", jwt.Config{
			PrivateKeyPEM: string(privatePEM), PublicKeysPEM: string(publicPEM),
		}},
		{"issuer, escaped newlines", jwt.Config{
			PrivateKeyPEM: escape(privatePEM), PublicKeysPEM: escape(publicPEM),
		}},
		{"verifier only, real newlines", jwt.Config{PublicKeysPEM: string(publicPEM)}},
		{"verifier only, escaped newlines", jwt.Config{PublicKeysPEM: escape(publicPEM)}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			keys, err := jwt.LoadKeySet(testCase.cfg)
			if err != nil {
				t.Fatalf("LoadKeySet: %v", err)
			}

			if len(keys.Keys()) != 1 {
				t.Fatalf("loaded %d keys, want 1", len(keys.Keys()))
			}
		})
	}
}

// TestLoadKeySetRejectsUnreadablePublicKeys is the half that used to be silent.
//
// Only the service that issues tokens holds a private key, and an unreadable one is loud
// because parsing it fails. The other four carry public keys alone: without this check they
// started with an empty key set, reported themselves healthy, and rejected every token they
// were given, with nothing in the log to say why.
func TestLoadKeySetRejectsUnreadablePublicKeys(t *testing.T) {
	t.Parallel()

	if _, err := jwt.LoadKeySet(jwt.Config{PublicKeysPEM: "not a pem block"}); !errors.Is(err, jwt.ErrInvalidKey) {
		t.Fatalf("LoadKeySet error = %v, want ErrInvalidKey", err)
	}
}
