package amqp

import (
	"errors"
	"testing"
	"time"
)

var errHandler = errors.New("the handler failed")

// This is acceptance criterion 4 of PRD-00: a message that fails five times ends up in the
// dead-letter queue. The policy is tested here as a pure function; that it really reaches
// the DLQ against a broker is verified by the integration tests of RF-18.
func TestAMessageIsDeadLetteredAfterFiveAttempts(t *testing.T) {
	t.Parallel()

	const maxAttempts int32 = 5

	initial, maximum := time.Second, time.Minute

	// Attempts 1 to 4 are retried; the fifth failure exhausts them.
	for prior := range maxAttempts - 1 {
		outcome := decide(errHandler, prior, maxAttempts, initial, maximum)

		if outcome.Action != ActionRetry {
			t.Errorf("with %d prior attempts the action is %s, want retry", prior, outcome.Action)
		}

		if outcome.Attempt != prior+1 {
			t.Errorf("attempt = %d, want %d", outcome.Attempt, prior+1)
		}
	}

	outcome := decide(errHandler, maxAttempts-1, maxAttempts, initial, maximum)

	if outcome.Action != ActionDeadLetter {
		t.Errorf("the fifth failure gives %s, want dead-letter", outcome.Action)
	}

	if outcome.Attempt != maxAttempts {
		t.Errorf("attempt = %d, want %d", outcome.Attempt, maxAttempts)
	}
}

// A message that has somehow overshot the limit must still be dead-lettered, not retried
// forever.
func TestAnOvershotAttemptCountIsStillDeadLettered(t *testing.T) {
	t.Parallel()

	outcome := decide(errHandler, 99, 5, time.Second, time.Minute)

	if outcome.Action != ActionDeadLetter {
		t.Errorf("action = %s, want dead-letter", outcome.Action)
	}
}

func TestASuccessfulHandlerAcknowledges(t *testing.T) {
	t.Parallel()

	outcome := decide(nil, 3, 5, time.Second, time.Minute)

	if outcome.Action != ActionAck {
		t.Errorf("action = %s, want ack", outcome.Action)
	}

	if outcome.Delay != 0 {
		t.Errorf("delay = %s, want zero on success", outcome.Delay)
	}
}

// The backoff has to grow: retrying a failing dependency at a fixed interval is how a
// consumer turns a blip into a hammering.
func TestTheBackoffGrowsExponentiallyAndIsCapped(t *testing.T) {
	t.Parallel()

	initial, maximum := time.Second, 8*time.Second

	want := []time.Duration{
		1 * time.Second, // attempt 1
		2 * time.Second, // attempt 2
		4 * time.Second, // attempt 3
		8 * time.Second, // attempt 4
		8 * time.Second, // attempt 5, capped
		8 * time.Second, // attempt 6, still capped
	}

	for i, expected := range want {
		attempt := int32(i + 1)

		if got := backoffFor(attempt, initial, maximum); got != expected {
			t.Errorf("backoffFor(%d) = %s, want %s", attempt, got, expected)
		}
	}
}

func TestTheDeadLetterQueueFollowsTheNamingConvention(t *testing.T) {
	t.Parallel()

	// RF-11 fixes the name: <queue>.dlq. A consumer and an operator looking for a failed
	// message have to agree on where it is.
	if got := DLQName("mail.notifications"); got != "mail.notifications.dlq" {
		t.Errorf("DLQName = %q, want mail.notifications.dlq", got)
	}

	if got := RetryQueueName("mail.notifications"); got != "mail.notifications.retry" {
		t.Errorf("RetryQueueName = %q, want mail.notifications.retry", got)
	}
}
