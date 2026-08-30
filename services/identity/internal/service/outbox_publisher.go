package service

import (
	"context"
	"fmt"

	"github.com/nicodanke/dizen-v2-backend/pkg/outbox"
	"github.com/nicodanke/dizen-v2-backend/services/identity/internal/repository"
)

// EventRecorder records domain events for later publication.
//
// It is the shape every service in this repository uses to emit an event: the caller hands
// it the transactional repository, so the event is written in the same transaction as the
// state change and cannot be recorded for a change that never landed (RF-12).
type EventRecorder struct{}

// NewEventRecorder builds the recorder.
func NewEventRecorder() *EventRecorder {
	return &EventRecorder{}
}

// RecordUserRegistered writes the user.registered event (01 section 4.3).
//
// It takes the repository rather than holding one, because the repository it must use is the
// one bound to the caller's transaction.
func (r *EventRecorder) RecordUserRegistered(
	ctx context.Context,
	repo repository.OutboxRepository,
	userID string,
) error {
	if userID == "" {
		return fmt.Errorf("recording user.registered: %w", ErrMissingUserID)
	}

	_, err := repo.Insert(ctx, repository.NewOutboxEvent{
		Aggregate:   "user",
		AggregateID: userID,
		RoutingKey:  outbox.RoutingKeyUserRegistered,
		Payload:     mustPayload(map[string]string{"user_id": userID}),
	})
	if err != nil {
		return fmt.Errorf("recording user.registered for %s: %w", userID, err)
	}

	return nil
}
