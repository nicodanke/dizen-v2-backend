package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nicodanke/dizen-v2-backend/services/identity/internal/db/sqlc"
)

// OutboxEvent is the domain view of a pending event. It is deliberately not sqlc.Outbox:
// the service layer must never see a generated type.
type OutboxEvent struct {
	ID          int64
	Aggregate   string
	AggregateID string
	RoutingKey  string
	Payload     json.RawMessage
	Attempts    int32
	LastError   string
	AvailableAt time.Time
	CreatedAt   time.Time
	PublishedAt *time.Time
}

// Published reports whether the event has already been published.
func (e OutboxEvent) Published() bool {
	return e.PublishedAt != nil
}

// NewOutboxEvent is what a caller supplies to record an event.
type NewOutboxEvent struct {
	Aggregate   string
	AggregateID string
	RoutingKey  string
	Payload     json.RawMessage
}

// OutboxRepository is the contract the service layer depends on.
//
// It is an interface so the service can be tested against a fake, and so mockery can
// generate the double (RF-18). The concrete implementation below is the only thing that
// knows sqlc exists.
type OutboxRepository interface {
	// Insert records an event. Called inside the transaction that changed the state.
	Insert(ctx context.Context, event NewOutboxEvent) (OutboxEvent, error)

	// ClaimPending takes a batch of events due for publication, locking them against
	// other workers.
	ClaimPending(ctx context.Context, limit int32) ([]OutboxEvent, error)

	// MarkPublished records that the broker confirmed the event.
	MarkPublished(ctx context.Context, id int64) error

	// Reschedule records a failed attempt and pushes the retry forward.
	Reschedule(ctx context.Context, id int64, reason string, retryAt time.Time) error

	// CountPending returns the size of the backlog, exposed as a metric.
	CountPending(ctx context.Context) (int64, error)

	// DeletePublishedBefore prunes already published events.
	DeletePublishedBefore(ctx context.Context, before time.Time) (int64, error)
}

// outboxRepository is the sqlc-backed implementation.
type outboxRepository struct {
	queries *sqlc.Queries
}

// compile-time check that the implementation satisfies the contract.
var _ OutboxRepository = (*outboxRepository)(nil)

// Insert records an event inside the caller's transaction.
func (r *outboxRepository) Insert(ctx context.Context, event NewOutboxEvent) (OutboxEvent, error) {
	row, err := r.queries.InsertOutboxEvent(ctx, sqlc.InsertOutboxEventParams{
		Aggregate:   event.Aggregate,
		AggregateID: nullable(event.AggregateID),
		RoutingKey:  event.RoutingKey,
		Payload:     event.Payload,
	})
	if err != nil {
		return OutboxEvent{}, fmt.Errorf("inserting the outbox event %q: %w", event.RoutingKey, err)
	}

	return toDomain(row), nil
}

// ClaimPending takes a batch of due events.
func (r *outboxRepository) ClaimPending(ctx context.Context, limit int32) ([]OutboxEvent, error) {
	rows, err := r.queries.ClaimPendingOutboxEvents(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("claiming pending outbox events: %w", err)
	}

	events := make([]OutboxEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, toDomain(row))
	}

	return events, nil
}

// MarkPublished records a successful publication.
func (r *outboxRepository) MarkPublished(ctx context.Context, id int64) error {
	if err := r.queries.MarkOutboxEventPublished(ctx, id); err != nil {
		return fmt.Errorf("marking outbox event %d as published: %w", id, err)
	}

	return nil
}

// Reschedule records a failed attempt.
func (r *outboxRepository) Reschedule(ctx context.Context, id int64, reason string, retryAt time.Time) error {
	err := r.queries.RescheduleOutboxEvent(ctx, sqlc.RescheduleOutboxEventParams{
		ID:          id,
		LastError:   nullable(reason),
		AvailableAt: retryAt,
	})
	if err != nil {
		return fmt.Errorf("rescheduling outbox event %d: %w", id, err)
	}

	return nil
}

// CountPending returns the backlog size.
func (r *outboxRepository) CountPending(ctx context.Context) (int64, error) {
	count, err := r.queries.CountPendingOutboxEvents(ctx)
	if err != nil {
		return 0, fmt.Errorf("counting pending outbox events: %w", err)
	}

	return count, nil
}

// DeletePublishedBefore prunes published events.
func (r *outboxRepository) DeletePublishedBefore(ctx context.Context, before time.Time) (int64, error) {
	deleted, err := r.queries.DeletePublishedOutboxEventsBefore(ctx, &before)
	if err != nil {
		return 0, fmt.Errorf("pruning published outbox events: %w", err)
	}

	return deleted, nil
}

// toDomain maps the generated row to the domain type. It is the only place the two shapes
// meet, which is what keeps sqlc out of the rest of the service.
func toDomain(row sqlc.Outbox) OutboxEvent {
	return OutboxEvent{
		ID:          row.ID,
		Aggregate:   row.Aggregate,
		AggregateID: deref(row.AggregateID),
		RoutingKey:  row.RoutingKey,
		Payload:     row.Payload,
		Attempts:    row.Attempts,
		LastError:   deref(row.LastError),
		AvailableAt: row.AvailableAt,
		CreatedAt:   row.CreatedAt,
		PublishedAt: row.PublishedAt,
	}
}

// nullable turns an empty string into a NULL, so "not set" and "set to empty" stay
// distinguishable in the database.
func nullable(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

// deref turns a NULL into an empty string for the domain type, where the distinction does
// not matter to any caller.
func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}
