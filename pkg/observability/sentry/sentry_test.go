package sentry

import (
	"strings"
	"testing"

	sentrygo "github.com/getsentry/sentry-go"
)

// The filter of RF-10 is the one piece here that must not be taken on trust: everything
// else fails loudly when it breaks, and this fails by sending something it should not have.

func TestTheFilterRedactsSensitiveValuesInAMessage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
	}{
		{
			name: "a JWT, which is what our access and refresh tokens look like",
			in:   "rejecting eyJhbGciOiJFZERTQSJ9.eyJzdWIiOiJ1MSJ9.c2lnbmF0dXJl for user u1",
		},
		{
			name: "an authorization header that reached a message",
			in:   "upstream refused: Bearer abc123def456ghi789",
		},
		{
			name: "an email address",
			in:   "no account for nico@dizen.pro",
		},
		{
			name: "a connection string carrying its credentials",
			in:   "dial failed: postgres://dizen:s3cr3t@db.neon.tech:5432/identity",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			event := &sentrygo.Event{Message: tc.in}

			got := scrubEvent(event, nil)

			if !strings.Contains(got.Message, redacted) {
				t.Errorf("nothing was redacted in %q", got.Message)
			}

			for _, secret := range []string{"c2lnbmF0dXJl", "abc123def456ghi789", "nico@dizen.pro", "s3cr3t"} {
				if strings.Contains(got.Message, secret) {
					t.Errorf("%q survived the filter: %q", secret, got.Message)
				}
			}
		})
	}
}

func TestTheFilterRedactsATagByItsName(t *testing.T) {
	t.Parallel()

	event := &sentrygo.Event{
		Tags: map[string]string{
			"refresh_token": "not-a-recognizable-shape",
			"user_email":    "someone@example.com",
			"latitude":      "-34.6037",
			"trace_id":      "4bf92f3577b34da6a3ce929d0e0e4736",
			"service":       "identity",
		},
	}

	got := scrubEvent(event, nil)

	// Redacted by the name of the field, because an opaque token is not recognizable by
	// its shape -- it looks exactly like a request id.
	for _, key := range []string{"refresh_token", "user_email", "latitude"} {
		if got.Tags[key] != redacted {
			t.Errorf("tag %q was sent as %q", key, got.Tags[key])
		}
	}

	// And what must survive, survives: without these an event cannot be tied to its trace
	// or its service, which is the whole point of sending it.
	if got.Tags["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace_id was altered: %q", got.Tags["trace_id"])
	}

	if got.Tags["service"] != "identity" {
		t.Errorf("service was altered: %q", got.Tags["service"])
	}
}

func TestTheFilterKeepsOnlyTheUserID(t *testing.T) {
	t.Parallel()

	event := &sentrygo.Event{
		User: sentrygo.User{
			ID:        "u1",
			Email:     "nico@dizen.pro",
			Name:      "Nico",
			IPAddress: "203.0.113.10",
		},
	}

	got := scrubEvent(event, nil)

	if got.User.ID != "u1" {
		t.Errorf("the id is what makes an error actionable and it was dropped: %q", got.User.ID)
	}

	if got.User.Email != "" || got.User.Name != "" || got.User.IPAddress != "" {
		t.Errorf("RF-10 allows the id and nothing else, got %+v", got.User)
	}
}

func TestTheFilterRedactsInsteadOfDroppingTheEvent(t *testing.T) {
	t.Parallel()

	event := &sentrygo.Event{Message: "login failed for nico@dizen.pro"}

	if got := scrubEvent(event, nil); got == nil {
		t.Fatal("the event was dropped; an error nobody hears about is worse than a redacted one")
	}
}

func TestAnEmptyDSNDisablesReportingWithoutFailing(t *testing.T) {
	t.Parallel()

	sink, err := Setup(Options{Environment: "test"})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}

	if sink.Writer() != nil {
		t.Error("a disabled sink must have no writer, or the logger sends to nowhere")
	}

	if err := sink.Close(); err != nil {
		t.Errorf("Close on a disabled sink: %v", err)
	}
}

func TestANilSinkIsSafe(t *testing.T) {
	t.Parallel()

	var sink *Sink

	if sink.Writer() != nil {
		t.Error("Writer on a nil sink must be nil")
	}

	if err := sink.Close(); err != nil {
		t.Errorf("Close on a nil sink: %v", err)
	}
}

// The release string is a contract between two places that never see each other: this
// package, which reports it from the running binary, and scripts/sentry-release.sh, which
// registers it from CI. When they disagree Sentry shows a release with no events beside
// events with no release, which reads as a reporting failure and is not one.
func TestTheReleaseMatchesWhatCIRegisters(t *testing.T) {
	t.Parallel()

	// scripts/sentry-release.sh builds "${service}@${version}@${commit}".
	const asRegisteredByCI = "identity@staging-abc1234@abcdef1234567890"

	if got := Release("identity", "staging-abc1234", "abcdef1234567890"); got != asRegisteredByCI {
		t.Errorf("the binary reports %q, CI registers %q", got, asRegisteredByCI)
	}
}

func TestTheReleaseCarriesTheCommit(t *testing.T) {
	t.Parallel()

	// Without the commit every build of staging reports the same release, and a
	// regression is attributed to the one before it (RF-8).
	if got := Release("identity", "staging", "8bbcc11"); got != "identity@staging@8bbcc11" {
		t.Errorf("Release = %q", got)
	}

	// A local build has no commit, and an empty segment would make the release read as
	// if one were missing rather than absent.
	if got := Release("identity", "dev", "unknown"); got != "identity@dev" {
		t.Errorf("Release without a commit = %q", got)
	}
}
