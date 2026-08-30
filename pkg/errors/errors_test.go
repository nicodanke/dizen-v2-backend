package errors_test

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	dizen "github.com/nicodanke/dizen-v2-backend/pkg/errors"
	"github.com/nicodanke/dizen-v2-backend/pkg/logger"
)

// ctxWithLog returns a context carrying a logger writing into buf, so the tests can assert
// on what was logged as well as on what was returned.
func ctxWithLog(t *testing.T) (context.Context, *bytes.Buffer) {
	t.Helper()

	buf := &bytes.Buffer{}
	log := logger.New(logger.Options{ServiceName: "identity", Level: "debug", Output: buf})

	return logger.WithContext(t.Context(), log), buf
}

// logEntries decodes the lines written to the buffer. Decoding rather than substring
// matching matters: zerolog escapes quotes, so a raw comparison against a message
// containing them fails even when the value is present.
func logEntries(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()

	var entries []map[string]any

	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}

		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("the log line is not JSON (%q): %v", line, err)
		}

		entries = append(entries, entry)
	}

	return entries
}

// loggedError returns the "error" field of the first entry that carries one.
func loggedError(t *testing.T, buf *bytes.Buffer) string {
	t.Helper()

	for _, entry := range logEntries(t, buf) {
		if msg, ok := entry["error"].(string); ok {
			return msg
		}
	}

	return ""
}

// This is the heart of RF-13: an unexpected failure tells the client nothing and tells the
// log everything.
func TestAnUnexpectedErrorLeaksNothingButIsLoggedInFull(t *testing.T) {
	t.Parallel()

	const secret = `pq: password authentication failed for user "dizen" at postgres://dizen:hunter2@db:5432`

	ctx, logs := ctxWithLog(t)

	err := dizen.ToStatus(ctx, stderrors.New(secret))

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("the result is not a gRPC status: %v", err)
	}

	if st.Code() != codes.Internal {
		t.Errorf("code = %s, want INTERNAL", st.Code())
	}

	// Nothing internal may reach the client.
	for _, leaked := range []string{"hunter2", "postgres://", "dizen", "password"} {
		if strings.Contains(st.Message(), leaked) {
			t.Errorf("the response leaked %q: %q", leaked, st.Message())
		}
	}

	// The client still gets a stable reason to branch on.
	if got := dizen.ReasonOf(err); got != dizen.ReasonInternal {
		t.Errorf("reason = %q, want %s", got, dizen.ReasonInternal)
	}

	// The log, on the other hand, has to carry the whole thing.
	if got := loggedError(t, logs); got != secret {
		t.Errorf("the log does not carry the full cause.\n got: %s\nwant: %s", got, secret)
	}

	if entries := logEntries(t, logs); entries[0]["level"] != "error" {
		t.Errorf("the failure was logged at level %v, want error", entries[0]["level"])
	}
}

// A domain error is reported exactly as declared, because its message was written to be
// read by a caller.
func TestADomainErrorIsReportedAsDeclared(t *testing.T) {
	t.Parallel()

	ctx, _ := ctxWithLog(t)

	domainErr := dizen.PermissionDenied(dizen.ReasonTourNotEntitled, "the tour requires a subscription")

	err := dizen.ToStatus(ctx, domainErr)

	st, _ := status.FromError(err)

	if st.Code() != codes.PermissionDenied {
		t.Errorf("code = %s, want PERMISSION_DENIED", st.Code())
	}

	if st.Message() != "the tour requires a subscription" {
		t.Errorf("message = %q", st.Message())
	}

	if got := dizen.ReasonOf(err); got != dizen.ReasonTourNotEntitled {
		t.Errorf("reason = %q, want %s", got, dizen.ReasonTourNotEntitled)
	}
}

// The cause of a domain error is the internal half: logged, never returned.
func TestTheCauseOfADomainErrorIsNotReturned(t *testing.T) {
	t.Parallel()

	const internal = "constraint bookings_slot_id_fkey violated"

	ctx, logs := ctxWithLog(t)

	domainErr := dizen.
		FailedPrecondition(dizen.ReasonSlotFull, "the slot has no capacity left").
		WithCause(stderrors.New(internal))

	err := dizen.ToStatus(ctx, domainErr)

	st, _ := status.FromError(err)

	if strings.Contains(st.Message(), internal) {
		t.Errorf("the response leaked the cause: %q", st.Message())
	}

	if got := loggedError(t, logs); got != internal {
		t.Errorf("the log does not carry the cause.\n got: %s\nwant: %s", got, internal)
	}
}

// Metadata is machine-readable context the client is meant to receive.
func TestMetadataReachesTheClient(t *testing.T) {
	t.Parallel()

	ctx, _ := ctxWithLog(t)

	domainErr := dizen.
		InvalidArgument(dizen.ReasonInvalidArgument, "the request is invalid").
		WithMetadata("field", "page_size").
		WithMetadata("limit", "200")

	err := dizen.ToStatus(ctx, domainErr)

	extracted, ok := dizen.As(domainErr)
	if !ok {
		t.Fatal("As did not recover the domain error")
	}

	if extracted.Metadata["field"] != "page_size" {
		t.Errorf("metadata = %v", extracted.Metadata)
	}

	if dizen.ReasonOf(err) != dizen.ReasonInvalidArgument {
		t.Error("the reason did not survive")
	}
}

// The builders return copies, so a shared sentinel cannot be mutated by a caller that
// attaches a cause to it.
func TestTheBuildersDoNotMutateTheOriginal(t *testing.T) {
	t.Parallel()

	base := dizen.NotFound(dizen.ReasonNotFound, "not found")

	withCause := base.WithCause(stderrors.New("row not found"))
	withMeta := base.WithMetadata("id", "t1")

	if base.Unwrap() != nil {
		t.Error("WithCause mutated the original")
	}

	if base.Metadata != nil {
		t.Error("WithMetadata mutated the original")
	}

	if withCause.Unwrap() == nil {
		t.Error("the copy lost the cause")
	}

	if withMeta.Metadata["id"] != "t1" {
		t.Error("the copy lost the metadata")
	}
}

// Comparing by reason is what lets a caller branch without matching message text.
func TestErrorsIsComparesByReason(t *testing.T) {
	t.Parallel()

	notFound := dizen.NotFound(dizen.ReasonNotFound, "the tour does not exist")
	otherNotFound := dizen.NotFound(dizen.ReasonNotFound, "a different message entirely")
	slotFull := dizen.FailedPrecondition(dizen.ReasonSlotFull, "no capacity")

	if !stderrors.Is(notFound, otherNotFound) {
		t.Error("two errors with the same reason did not compare equal")
	}

	if stderrors.Is(notFound, slotFull) {
		t.Error("two errors with different reasons compared equal")
	}
}

// errors.As has to reach through a wrapped chain, which is how a repository error survives
// being wrapped by a service.
func TestAsReachesThroughAWrappedChain(t *testing.T) {
	t.Parallel()

	domainErr := dizen.NotFound(dizen.ReasonNotFound, "the user does not exist")

	wrapped := stderrors.Join(stderrors.New("loading the profile"), domainErr)

	got, ok := dizen.As(wrapped)
	if !ok {
		t.Fatal("As did not find the domain error in the chain")
	}

	if got.Reason != dizen.ReasonNotFound {
		t.Errorf("reason = %q", got.Reason)
	}
}

// A caller hanging up is not a server failure: reporting it as INTERNAL would make every
// abandoned request look like an incident.
func TestACanceledContextIsNotAnInternalError(t *testing.T) {
	t.Parallel()

	ctx, _ := ctxWithLog(t)

	if got := status.Code(dizen.ToStatus(ctx, context.Canceled)); got != codes.Canceled {
		t.Errorf("code = %s, want CANCELED", got)
	}

	if got := status.Code(dizen.ToStatus(ctx, context.DeadlineExceeded)); got != codes.DeadlineExceeded {
		t.Errorf("code = %s, want DEADLINE_EXCEEDED", got)
	}
}

// A status built deliberately by an interceptor below must pass through untouched.
func TestAnExistingStatusPassesThrough(t *testing.T) {
	t.Parallel()

	ctx, _ := ctxWithLog(t)

	original := status.Error(codes.InvalidArgument, "invalid request: lat: value must be <= 90")

	err := dizen.ToStatus(ctx, original)

	st, _ := status.FromError(err)

	if st.Code() != codes.InvalidArgument {
		t.Errorf("code = %s", st.Code())
	}

	if !strings.Contains(st.Message(), "lat") {
		t.Errorf("the validation detail was lost: %q", st.Message())
	}
}

func TestToStatusOnNil(t *testing.T) {
	t.Parallel()

	ctx, _ := ctxWithLog(t)

	if err := dizen.ToStatus(ctx, nil); err != nil {
		t.Errorf("ToStatus(nil) = %v, want nil", err)
	}
}

// A client error must not be logged at error level: burying real failures under 404s is how
// an error rate stops meaning anything.
func TestAClientErrorIsNotLoggedAtErrorLevel(t *testing.T) {
	t.Parallel()

	ctx, logs := ctxWithLog(t)

	_ = dizen.ToStatus(ctx, dizen.NotFound(dizen.ReasonNotFound, "the tour does not exist"))

	for _, entry := range logEntries(t, logs) {
		if entry["level"] == "error" {
			t.Errorf("a NOT_FOUND was logged at error level: %v", entry)
		}
	}

	// It is still logged, with its reason, so the rate can be counted.
	if !strings.Contains(logs.String(), dizen.ReasonNotFound.String()) {
		t.Errorf("the reason is missing from the log:\n%s", logs.String())
	}
}

// A domain error is usable even by a caller that forgot ToStatus, because it carries its
// own status.
func TestADomainErrorCarriesItsOwnStatus(t *testing.T) {
	t.Parallel()

	domainErr := dizen.Unauthenticated(dizen.ReasonSessionRevoked, "the session was revoked")

	st, ok := status.FromError(domainErr)
	if !ok {
		t.Fatal("the domain error does not expose a gRPC status")
	}

	if st.Code() != codes.Unauthenticated {
		t.Errorf("code = %s", st.Code())
	}

	if dizen.ReasonOf(domainErr) != dizen.ReasonSessionRevoked {
		t.Error("the reason is not readable from the bare error")
	}
}

func TestReasonOfOnAnErrorWithoutOne(t *testing.T) {
	t.Parallel()

	if got := dizen.ReasonOf(nil); got != "" {
		t.Errorf("ReasonOf(nil) = %q", got)
	}

	if got := dizen.ReasonOf(stderrors.New("plain")); got != "" {
		t.Errorf("ReasonOf on a plain error = %q", got)
	}
}
