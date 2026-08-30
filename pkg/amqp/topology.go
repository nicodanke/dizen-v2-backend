package amqp

import (
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Headers carried on every message. They are what makes the retry policy work without a
// database: the attempt count travels with the message.
const (
	// HeaderAttempts is how many times delivery has been attempted.
	HeaderAttempts = "x-dizen-attempts"
	// HeaderFirstFailure is when the first failure happened, for diagnosing a message
	// that has been bouncing for a while.
	HeaderFirstFailure = "x-dizen-first-failure"
	// HeaderLastError is the last failure reason, so the dead-letter queue is readable
	// without cross-referencing logs.
	HeaderLastError = "x-dizen-last-error"
	// HeaderTraceID ties the message back to the trace that produced it.
	HeaderTraceID = "x-dizen-trace-id"
)

// QueueSpec describes a queue and its binding.
type QueueSpec struct {
	// Name of the queue.
	Name string

	// RoutingKeys the queue binds to on the exchange. Topic patterns are allowed.
	RoutingKeys []string

	// Durable survives a broker restart. Always true for domain events.
	Durable bool
}

// DLQName returns the dead-letter queue name for a queue.
func DLQName(queue string) string {
	return queue + DLQSuffix
}

// declareTopology declares the exchange, the queue, its dead-letter queue and the
// bindings. It is idempotent: declaring what already exists with the same arguments is a
// no-op, which is what lets every replica run it at startup (RF-11).
func declareTopology(ch *amqp.Channel, exchange string, spec QueueSpec) error {
	if err := ch.ExchangeDeclare(
		exchange,
		DefaultExchangeKind,
		true,  // durable
		false, // not auto-deleted
		false, // not internal
		false, // wait for confirmation
		nil,
	); err != nil {
		return fmt.Errorf("declaring the exchange %q: %w", exchange, err)
	}

	if spec.Name == "" {
		// A publisher-only connection declares the exchange and nothing else.
		return nil
	}

	// The dead-letter queue is declared first: the main queue points at it, so it has to
	// exist before the binding is usable.
	dlq := DLQName(spec.Name)

	if _, err := ch.QueueDeclare(dlq, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declaring the dead-letter queue %q: %w", dlq, err)
	}

	// Exhausted messages are routed to the dead-letter queue by name, through the default
	// exchange, which needs no binding of its own.
	args := amqp.Table{
		"x-dead-letter-exchange":    "",
		"x-dead-letter-routing-key": dlq,
	}

	if _, err := ch.QueueDeclare(spec.Name, spec.Durable, false, false, false, args); err != nil {
		return fmt.Errorf("declaring the queue %q: %w", spec.Name, err)
	}

	for _, key := range spec.RoutingKeys {
		if err := ch.QueueBind(spec.Name, key, exchange, false, nil); err != nil {
			return fmt.Errorf("binding %q to %q on %q: %w", spec.Name, key, exchange, err)
		}
	}

	return nil
}
