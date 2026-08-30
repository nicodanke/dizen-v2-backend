package amqp

import "time"

// Action is what the consumer does with a delivery once the handler has returned.
type Action int

const (
	// ActionAck: the handler succeeded and the message is done.
	ActionAck Action = iota

	// ActionRetry: the handler failed and attempts remain, so the message is scheduled
	// for redelivery after a backoff.
	ActionRetry

	// ActionDeadLetter: the attempts are exhausted and the message goes to the
	// dead-letter queue (RF-11: after 5 attempts).
	ActionDeadLetter
)

// String makes the action readable in logs.
func (a Action) String() string {
	switch a {
	case ActionAck:
		return "ack"
	case ActionRetry:
		return "retry"
	case ActionDeadLetter:
		return "dead-letter"
	default:
		return "unknown"
	}
}

// Outcome is the full decision for a delivery: what to do, on which attempt, and how long
// to wait before trying again.
type Outcome struct {
	Action Action

	// Attempt is the number of the attempt just made, starting at 1.
	Attempt int32

	// Delay is how long to wait before redelivery. Meaningful only for ActionRetry.
	Delay time.Duration
}

// decide is the retry policy, isolated as a pure function so the rule of RF-11 -- retry
// with exponential backoff, dead-letter after maxAttempts -- can be verified without a
// broker.
//
// priorAttempts is what the message carries in its header: zero on first delivery.
func decide(handlerErr error, priorAttempts, maxAttempts int32, initial, maximum time.Duration) Outcome {
	attempt := priorAttempts + 1

	if handlerErr == nil {
		return Outcome{Action: ActionAck, Attempt: attempt}
	}

	// The attempt just made counts: with maxAttempts of 5, the fifth failure is the last
	// one and the message is dead-lettered rather than retried a sixth time.
	if attempt >= maxAttempts {
		return Outcome{Action: ActionDeadLetter, Attempt: attempt}
	}

	return Outcome{
		Action:  ActionRetry,
		Attempt: attempt,
		Delay:   backoffFor(attempt, initial, maximum),
	}
}
