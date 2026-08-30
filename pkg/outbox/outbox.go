// Package outbox implements transactional event publication (RF-12, 01 section 4.3).
//
// The problem it solves: a service that confirms a booking and then publishes an event has
// two operations that can fail independently. If the publish fails, the booking is
// confirmed and the email never goes out; if the commit fails after a successful publish,
// an email goes out for a booking that does not exist.
//
// The outbox removes the gap by writing the event into the same transaction as the state
// change. A worker publishes it afterwards and marks it sent. Delivery becomes at least
// once, which is why every consumer has to be idempotent.
package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Routing keys of the domain events (01 section 4.3).
//
// They are re-exported here so a service that only records events depends on pkg/outbox
// rather than on pkg/amqp: what a service needs to know is the name of the event, not how it
// reaches the broker.
const (
	RoutingKeyUserRegistered    = "user.registered"
	RoutingKeyBookingConfirmed  = "booking.confirmed"
	RoutingKeyBookingCancelled  = "booking.cancelled"
	RoutingKeySubscriptionRenew = "subscription.renewed"
	RoutingKeyTourPublished     = "tour.published"
	RoutingKeyTourRunCompleted  = "tour_run.completed"
)

// Event is a pending event as the store hands it out.
type Event struct {
	ID          int64
	Aggregate   string
	AggregateID string
	RoutingKey  string
	Payload     json.RawMessage
	Attempts    int32
	LastError   string
	AvailableAt time.Time
	CreatedAt   time.Time
}

// NewEvent is what a caller records.
type NewEvent struct {
	// Aggregate the event belongs to, such as "booking".
	Aggregate string

	// AggregateID identifies the instance. Optional.
	AggregateID string

	// RoutingKey is the AMQP key, such as "booking.confirmed".
	RoutingKey string

	// Payload is the already serialized event body.
	Payload json.RawMessage
}

// Store is the persistence the worker needs.
//
// It is declared here, as an interface, so pkg/outbox depends on no service's repository:
// each service implements it over its own generated Querier. That is what lets one worker
// serve five services with five different databases.
type Store interface {
	// ClaimPending takes a batch of due events, locking them against other workers.
	ClaimPending(ctx context.Context, limit int32) ([]Event, error)

	// MarkPublished records that the broker confirmed the event.
	MarkPublished(ctx context.Context, id int64) error

	// Reschedule records a failed attempt and pushes the retry forward.
	Reschedule(ctx context.Context, id int64, reason string, retryAt time.Time) error

	// CountPending returns the backlog size.
	CountPending(ctx context.Context) (int64, error)
}

// Inserter records an event inside a transaction. A service's transactional repository
// satisfies it.
type Inserter interface {
	Insert(ctx context.Context, event NewEvent) (Event, error)
}

// Publisher sends the event to the broker. pkg/amqp.Publisher satisfies it.
type Publisher interface {
	Publish(ctx context.Context, routingKey string, payload []byte) error
}

// Publish records an event on the transaction already in progress (RF-12).
//
// The signature is the point: the inserter handed in is the transactional one, so a caller
// physically cannot write the event outside the transaction that is changing the state.
func Publish(ctx context.Context, inserter Inserter, routingKey string, payload any) error {
	return PublishFor(ctx, inserter, "", "", routingKey, payload)
}

// PublishFor records an event attributed to an aggregate instance, which is what makes the
// outbox readable when something goes wrong.
func PublishFor(
	ctx context.Context,
	inserter Inserter,
	aggregate, aggregateID, routingKey string,
	payload any,
) error {
	if routingKey == "" {
		return ErrEmptyRoutingKey
	}

	raw, err := marshal(payload)
	if err != nil {
		return err
	}

	if _, err := inserter.Insert(ctx, NewEvent{
		Aggregate:   aggregate,
		AggregateID: aggregateID,
		RoutingKey:  routingKey,
		Payload:     raw,
	}); err != nil {
		return fmt.Errorf("recording the outbox event %q: %w", routingKey, err)
	}

	return nil
}

// marshal serializes the payload, passing raw JSON through unchanged.
func marshal(payload any) (json.RawMessage, error) {
	if raw, ok := payload.(json.RawMessage); ok {
		return raw, nil
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("serializing the outbox payload: %w", err)
	}

	return raw, nil
}
