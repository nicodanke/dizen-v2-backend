package amqp

import (
	"strings"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// The attempt header may come back as any integer width depending on which client wrote
// it, so every plausible type has to read the same.
func TestAttemptsOfReadsEveryIntegerWidth(t *testing.T) {
	t.Parallel()

	cases := map[string]any{
		"int32":   int32(3),
		"int64":   int64(3),
		"int":     3,
		"float64": float64(3),
		"string":  "3",
	}

	for name, value := range cases {
		delivery := amqp.Delivery{Headers: amqp.Table{HeaderAttempts: value}}

		if got := attemptsOf(delivery); got != 3 {
			t.Errorf("%s: attemptsOf = %d, want 3", name, got)
		}
	}
}

// A message that has never failed carries no header, which must read as zero rather than
// as an error.
func TestAttemptsOfIsZeroOnAFirstDelivery(t *testing.T) {
	t.Parallel()

	if got := attemptsOf(amqp.Delivery{}); got != 0 {
		t.Errorf("attemptsOf on a bare delivery = %d, want 0", got)
	}

	unreadable := amqp.Delivery{Headers: amqp.Table{HeaderAttempts: []byte("nonsense")}}

	if got := attemptsOf(unreadable); got != 0 {
		t.Errorf("attemptsOf on an unreadable header = %d, want 0", got)
	}
}

// The headers the producer set must survive a redelivery: dropping them would lose the
// trace id and break the correlation the whole retry is logged under.
func TestRetryHeadersPreserveTheProducerHeaders(t *testing.T) {
	t.Parallel()

	original := amqp.Delivery{Headers: amqp.Table{
		HeaderTraceID: "4bf92f3577b34da6a3ce929d0e0e4736",
		"x-custom":    "kept",
	}}

	headers := retryHeaders(original, 2, "connection refused")

	if headers[HeaderTraceID] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("the trace id was lost: %v", headers[HeaderTraceID])
	}

	if headers["x-custom"] != "kept" {
		t.Errorf("a producer header was lost: %v", headers["x-custom"])
	}

	if headers[HeaderAttempts] != int32(2) {
		t.Errorf("attempts = %v, want 2", headers[HeaderAttempts])
	}

	if headers[HeaderLastError] != "connection refused" {
		t.Errorf("last error = %v", headers[HeaderLastError])
	}

	if headers[HeaderFirstFailure] == nil {
		t.Error("the first failure timestamp is missing")
	}
}

// The first failure timestamp must not be overwritten on later attempts: it is what tells
// an operator how long a message has been bouncing.
func TestTheFirstFailureTimestampIsNotOverwritten(t *testing.T) {
	t.Parallel()

	const firstFailure = "2026-08-30T12:00:00Z"

	original := amqp.Delivery{Headers: amqp.Table{HeaderFirstFailure: firstFailure}}

	headers := retryHeaders(original, 3, "still failing")

	if headers[HeaderFirstFailure] != firstFailure {
		t.Errorf("first failure = %v, want it preserved as %s", headers[HeaderFirstFailure], firstFailure)
	}
}

// A broker header is not a log: an unbounded error message would bloat every redelivery.
func TestTheErrorHeaderIsBounded(t *testing.T) {
	t.Parallel()

	huge := strings.Repeat("x", 5000)

	headers := retryHeaders(amqp.Delivery{}, 1, huge)

	stored, ok := headers[HeaderLastError].(string)
	if !ok {
		t.Fatalf("the error header is not a string: %T", headers[HeaderLastError])
	}

	if len(stored) > maxErrorLength+3 {
		t.Errorf("the error header is %d characters, it was not truncated", len(stored))
	}
}

func TestBackoffForTheFirstAttemptIsTheInitialDelay(t *testing.T) {
	t.Parallel()

	if got := backoffFor(1, 250*time.Millisecond, time.Minute); got != 250*time.Millisecond {
		t.Errorf("backoffFor(1) = %s, want 250ms", got)
	}

	if got := backoffFor(0, 250*time.Millisecond, time.Minute); got != 250*time.Millisecond {
		t.Errorf("backoffFor(0) = %s, want 250ms", got)
	}
}
