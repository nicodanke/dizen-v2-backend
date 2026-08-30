package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"

	"github.com/nicodanke/dizen-v2-backend/pkg/outbox"
	"github.com/nicodanke/dizen-v2-backend/services/identity/internal/repository"
	"github.com/nicodanke/dizen-v2-backend/services/identity/internal/repository/mocks"
	"github.com/nicodanke/dizen-v2-backend/services/identity/internal/service"
)

// These are the unit tests RF-18 asks for: the service driven against a generated mock of
// the repository interface, with no database anywhere. The same behavior against a real
// Postgres is covered by the integration tests.

func TestRecordUserRegisteredWritesTheEvent(t *testing.T) {
	t.Parallel()

	repo := mocks.NewMockOutboxRepository(t)

	// The expectation is the assertion: the service must write exactly this event, and
	// mockery fails the test at cleanup if it does not.
	repo.EXPECT().
		Insert(mock.Anything, mock.MatchedBy(func(event repository.NewOutboxEvent) bool {
			if event.Aggregate != "user" || event.AggregateID != "u1" {
				return false
			}

			if event.RoutingKey != outbox.RoutingKeyUserRegistered {
				return false
			}

			var payload map[string]string
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return false
			}

			return payload["user_id"] == "u1"
		})).
		Return(repository.OutboxEvent{ID: 1}, nil).
		Once()

	err := service.NewEventRecorder().RecordUserRegistered(context.Background(), repo, "u1")
	if err != nil {
		t.Fatalf("RecordUserRegistered: %v", err)
	}
}

// A repository failure must reach the caller: swallowing it would commit a state change
// whose event was never recorded, which is the exact failure the outbox exists to prevent.
func TestARepositoryFailureReachesTheCaller(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("deadlock detected")

	repo := mocks.NewMockOutboxRepository(t)
	repo.EXPECT().
		Insert(mock.Anything, mock.Anything).
		Return(repository.OutboxEvent{}, wantErr).
		Once()

	err := service.NewEventRecorder().RecordUserRegistered(context.Background(), repo, "u1")

	if !errors.Is(err, wantErr) {
		t.Errorf("the underlying error was lost: %v", err)
	}
}

// An event with no subject is one nobody can act on downstream, so it is rejected before it
// reaches the repository.
func TestAnEmptyUserIDIsRejectedWithoutTouchingTheRepository(t *testing.T) {
	t.Parallel()

	// No expectation is set: if the service called Insert, mockery would fail the test.
	repo := mocks.NewMockOutboxRepository(t)

	err := service.NewEventRecorder().RecordUserRegistered(context.Background(), repo, "")

	if !errors.Is(err, service.ErrMissingUserID) {
		t.Errorf("the error is not ErrMissingUserID: %v", err)
	}
}
