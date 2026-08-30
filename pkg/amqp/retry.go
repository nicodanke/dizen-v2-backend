package amqp

import (
	"maps"
	"strconv"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// attemptsOf reads the attempt count carried by a delivery. A message that has never
// failed carries no header, which reads as zero.
//
// The header may come back as any integer width depending on which client wrote it, so
// every plausible type is handled rather than assuming int32.
func attemptsOf(delivery amqp.Delivery) int32 {
	raw, ok := delivery.Headers[HeaderAttempts]
	if !ok {
		return 0
	}

	switch value := raw.(type) {
	case int32:
		return value
	case int64:
		return int32(value)
	case int:
		return int32(value)
	case float64:
		return int32(value)
	case string:
		// ParseInt with a bit size of 32 rather than Atoi, so a header carrying a huge
		// number is rejected instead of wrapping around into a negative attempt count.
		parsed, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			return 0
		}

		return int32(parsed)
	default:
		return 0
	}
}

// backoffFor returns how long to wait before the given attempt, doubling each time and
// capped at the maximum.
//
// The cap matters: without it the fifth attempt of a message with a one-minute base would
// land sixteen minutes out, long after whoever is watching has stopped.
func backoffFor(attempt int32, initial, maximum time.Duration) time.Duration {
	if attempt <= 1 {
		return initial
	}

	backoff := initial

	for range attempt - 1 {
		backoff *= 2

		if backoff >= maximum {
			return maximum
		}
	}

	return backoff
}

// retryHeaders builds the headers for a redelivery, carrying the attempt count forward.
func retryHeaders(delivery amqp.Delivery, attempts int32, reason string) amqp.Table {
	headers := amqp.Table{}

	// Whatever the producer set is preserved: dropping it would lose the trace id.
	maps.Copy(headers, delivery.Headers)

	headers[HeaderAttempts] = attempts
	headers[HeaderLastError] = truncate(reason, maxErrorLength)

	if _, ok := headers[HeaderFirstFailure]; !ok {
		headers[HeaderFirstFailure] = time.Now().UTC().Format(time.RFC3339)
	}

	return headers
}

// maxErrorLength bounds the error stored in a header. A broker header is not a log: an
// unbounded error message here would bloat every redelivery.
const maxErrorLength = 512

// truncate shortens a string to n characters.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[:n] + "..."
}
