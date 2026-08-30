package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nicodanke/dizen-v2-backend/services/identity/internal/db/sqlc"
)

// The mapping between the generated row and the domain type is the one place the two
// shapes meet, and the one place a NULL can quietly turn into a zero value. These are pure
// functions, so they are tested here; the methods that issue queries need a real Postgres
// and get their coverage from the integration tests in RF-18.

func TestNullableTurnsAnEmptyStringIntoNull(t *testing.T) {
	t.Parallel()

	if got := nullable(""); got != nil {
		t.Errorf("nullable(\"\") = %v, want nil: an empty string must be stored as NULL", *got)
	}

	got := nullable("value")
	if got == nil {
		t.Fatal("nullable(\"value\") returned nil")
	}

	if *got != "value" {
		t.Errorf("nullable(\"value\") = %q", *got)
	}
}

func TestDerefTurnsNullIntoAnEmptyString(t *testing.T) {
	t.Parallel()

	if got := deref(nil); got != "" {
		t.Errorf("deref(nil) = %q, want empty", got)
	}

	value := "value"
	if got := deref(&value); got != "value" {
		t.Errorf("deref = %q", got)
	}
}

// The round trip has to be stable: what nullable stores, deref must read back unchanged.
func TestNullableAndDerefRoundTrip(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"", "a", "a longer value with spaces"} {
		if got := deref(nullable(input)); got != input {
			t.Errorf("round trip of %q gave %q", input, got)
		}
	}
}

func TestToDomainMapsEveryField(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	availableAt := createdAt.Add(time.Minute)
	publishedAt := createdAt.Add(2 * time.Minute)

	aggregateID := "0193f0a0-0000-7000-8000-000000000001"
	lastError := "the broker refused the publication"

	row := sqlc.Outbox{
		ID:          42,
		Aggregate:   "user",
		AggregateID: &aggregateID,
		RoutingKey:  "user.registered",
		Payload:     json.RawMessage(`{"user_id":"u1"}`),
		Attempts:    3,
		LastError:   &lastError,
		AvailableAt: availableAt,
		CreatedAt:   createdAt,
		PublishedAt: &publishedAt,
	}

	got := toDomain(row)

	if got.ID != 42 {
		t.Errorf("ID = %d", got.ID)
	}

	if got.Aggregate != "user" {
		t.Errorf("Aggregate = %q", got.Aggregate)
	}

	if got.AggregateID != aggregateID {
		t.Errorf("AggregateID = %q", got.AggregateID)
	}

	if got.RoutingKey != "user.registered" {
		t.Errorf("RoutingKey = %q", got.RoutingKey)
	}

	if string(got.Payload) != `{"user_id":"u1"}` {
		t.Errorf("Payload = %s", got.Payload)
	}

	if got.Attempts != 3 {
		t.Errorf("Attempts = %d", got.Attempts)
	}

	if got.LastError != lastError {
		t.Errorf("LastError = %q", got.LastError)
	}

	if !got.AvailableAt.Equal(availableAt) {
		t.Errorf("AvailableAt = %s", got.AvailableAt)
	}

	if !got.CreatedAt.Equal(createdAt) {
		t.Errorf("CreatedAt = %s", got.CreatedAt)
	}

	if got.PublishedAt == nil || !got.PublishedAt.Equal(publishedAt) {
		t.Errorf("PublishedAt = %v", got.PublishedAt)
	}

	if !got.Published() {
		t.Error("Published() is false on a row that carries published_at")
	}
}

// A pending event has NULL in published_at, aggregate_id and last_error. That must map to
// a pending domain event, not to one that looks published.
func TestToDomainMapsAPendingRow(t *testing.T) {
	t.Parallel()

	got := toDomain(sqlc.Outbox{
		ID:         1,
		Aggregate:  "booking",
		RoutingKey: "booking.confirmed",
		Payload:    json.RawMessage(`{}`),
	})

	if got.Published() {
		t.Error("a row with published_at NULL reported as published")
	}

	if got.PublishedAt != nil {
		t.Errorf("PublishedAt = %v, want nil", got.PublishedAt)
	}

	if got.AggregateID != "" {
		t.Errorf("AggregateID = %q, want empty", got.AggregateID)
	}

	if got.LastError != "" {
		t.Errorf("LastError = %q, want empty", got.LastError)
	}
}
