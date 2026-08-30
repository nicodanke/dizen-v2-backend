package repository

import (
	"context"
	"time"

	"github.com/nicodanke/dizen-v2-backend/pkg/outbox"
)

// OutboxStore adapts the service repository to the outbox.Store and outbox.Inserter
// contracts.
//
// The adapter exists so pkg/outbox depends on no service: it declares what it needs, and
// each service supplies it over its own generated Querier. That is what lets one worker
// serve five services with five different databases.
type OutboxStore struct {
	repo OutboxRepository
}

var (
	_ outbox.Store    = (*OutboxStore)(nil)
	_ outbox.Inserter = (*OutboxStore)(nil)
)

// NewOutboxStore builds the adapter over any implementation of the repository contract.
// Taking the interface rather than reaching for a concrete type is what lets the adapter be
// exercised with a fake.
func NewOutboxStore(repo OutboxRepository) *OutboxStore {
	return &OutboxStore{repo: repo}
}

// OutboxStore returns the adapter the outbox worker consumes. Bound to whichever handle it
// was built from, so calling it on a transactional Repository writes inside that
// transaction.
func (r *Repository) OutboxStore() *OutboxStore {
	return NewOutboxStore(r.Outbox())
}

// Insert records an event on the transaction this repository is bound to.
func (s *OutboxStore) Insert(ctx context.Context, event outbox.NewEvent) (outbox.Event, error) {
	stored, err := s.repo.Insert(ctx, NewOutboxEvent{
		Aggregate:   event.Aggregate,
		AggregateID: event.AggregateID,
		RoutingKey:  event.RoutingKey,
		Payload:     event.Payload,
	})
	if err != nil {
		return outbox.Event{}, err
	}

	return toOutboxEvent(stored), nil
}

// ClaimPending takes a batch of due events.
func (s *OutboxStore) ClaimPending(ctx context.Context, limit int32) ([]outbox.Event, error) {
	events, err := s.repo.ClaimPending(ctx, limit)
	if err != nil {
		return nil, err
	}

	out := make([]outbox.Event, 0, len(events))
	for _, event := range events {
		out = append(out, toOutboxEvent(event))
	}

	return out, nil
}

// MarkPublished records a successful publication.
func (s *OutboxStore) MarkPublished(ctx context.Context, id int64) error {
	return s.repo.MarkPublished(ctx, id)
}

// Reschedule records a failed attempt.
func (s *OutboxStore) Reschedule(ctx context.Context, id int64, reason string, retryAt time.Time) error {
	return s.repo.Reschedule(ctx, id, reason, retryAt)
}

// CountPending returns the backlog size.
func (s *OutboxStore) CountPending(ctx context.Context) (int64, error) {
	return s.repo.CountPending(ctx)
}

// toOutboxEvent maps the domain event to the shape pkg/outbox expects.
func toOutboxEvent(event OutboxEvent) outbox.Event {
	return outbox.Event{
		ID:          event.ID,
		Aggregate:   event.Aggregate,
		AggregateID: event.AggregateID,
		RoutingKey:  event.RoutingKey,
		Payload:     event.Payload,
		Attempts:    event.Attempts,
		LastError:   event.LastError,
		AvailableAt: event.AvailableAt,
		CreatedAt:   event.CreatedAt,
	}
}
