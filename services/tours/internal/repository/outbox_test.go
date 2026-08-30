package repository_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nicodanke/dizen-v2-backend/pkg/outbox"
	"github.com/nicodanke/dizen-v2-backend/services/tours/internal/repository"
)

// fakeOutbox is a stand-in for the repository, of the shape a service test uses. It exists
// here to prove the interface is usable without a database: if OutboxRepository leaked an
// sqlc or pgx type, this type could not be written.
type fakeOutbox struct {
	inserted []repository.NewOutboxEvent
	events   []repository.OutboxEvent
	err      error
}

func (f *fakeOutbox) Insert(
	_ context.Context,
	event repository.NewOutboxEvent,
) (repository.OutboxEvent, error) {
	if f.err != nil {
		return repository.OutboxEvent{}, f.err
	}

	f.inserted = append(f.inserted, event)

	return repository.OutboxEvent{
		ID:         int64(len(f.inserted)),
		Aggregate:  event.Aggregate,
		RoutingKey: event.RoutingKey,
		Payload:    event.Payload,
	}, nil
}

func (f *fakeOutbox) ClaimPending(context.Context, int32) ([]repository.OutboxEvent, error) {
	return f.events, f.err
}

func (f *fakeOutbox) MarkPublished(context.Context, int64) error { return f.err }

func (f *fakeOutbox) Reschedule(context.Context, int64, string, time.Time) error { return f.err }

func (f *fakeOutbox) CountPending(context.Context) (int64, error) {
	return int64(len(f.events)), f.err
}

func (f *fakeOutbox) DeletePublishedBefore(context.Context, time.Time) (int64, error) {
	return 0, f.err
}

// The compile-time check is the actual assertion of RF-9: the fake satisfies the contract
// using only standard library and domain types.
var _ repository.OutboxRepository = (*fakeOutbox)(nil)

// publisher stands in for a service: it depends on the interface, never on sqlc.
type publisher struct {
	outbox repository.OutboxRepository
}

func (p *publisher) recordUserRegistered(ctx context.Context, userID string) error {
	payload, err := json.Marshal(map[string]string{"user_id": userID})
	if err != nil {
		return err
	}

	_, err = p.outbox.Insert(ctx, repository.NewOutboxEvent{
		Aggregate:   "user",
		AggregateID: userID,
		RoutingKey:  "user.registered",
		Payload:     payload,
	})

	return err
}

func TestAServiceCanBeTestedAgainstTheInterfaceWithoutADatabase(t *testing.T) {
	t.Parallel()

	fake := &fakeOutbox{}
	svc := &publisher{outbox: fake}

	if err := svc.recordUserRegistered(t.Context(), "0193f0a0-0000-7000-8000-000000000001"); err != nil {
		t.Fatalf("recordUserRegistered: %v", err)
	}

	if len(fake.inserted) != 1 {
		t.Fatalf("%d events recorded, want 1", len(fake.inserted))
	}

	event := fake.inserted[0]

	if event.RoutingKey != "user.registered" {
		t.Errorf("routing key = %q", event.RoutingKey)
	}

	if event.Aggregate != "user" {
		t.Errorf("aggregate = %q", event.Aggregate)
	}

	var payload map[string]string
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("the payload is not valid JSON: %v", err)
	}

	if payload["user_id"] != "0193f0a0-0000-7000-8000-000000000001" {
		t.Errorf("user_id = %q", payload["user_id"])
	}
}

func TestAFailureFromTheRepositoryReachesTheService(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("connection lost")
	svc := &publisher{outbox: &fakeOutbox{err: wantErr}}

	err := svc.recordUserRegistered(t.Context(), "u1")
	if !errors.Is(err, wantErr) {
		t.Errorf("the error did not reach the service: %v", err)
	}
}

// The boundary of RF-9, checked by reflection: no method of OutboxRepository may mention a
// generated or driver type. This is what stops sqlc from leaking into the service layer
// one signature at a time.
func TestTheRepositoryInterfaceLeaksNoGeneratedType(t *testing.T) {
	t.Parallel()

	forbidden := []string{
		"/internal/db/sqlc",
		"github.com/jackc/pgx",
	}

	iface := reflect.TypeFor[repository.OutboxRepository]()

	for method := range iface.Methods() {
		signature := method.Type.String()

		for _, bad := range forbidden {
			if strings.Contains(signature, bad) {
				t.Errorf("%s leaks %q in its signature: %s", method.Name, bad, signature)
			}
		}
	}
}

func TestPublishedReportsWhetherTheEventWasSent(t *testing.T) {
	t.Parallel()

	if (repository.OutboxEvent{}).Published() {
		t.Error("an event with no published_at reported as published")
	}

	now := time.Now()
	if !(repository.OutboxEvent{PublishedAt: &now}).Published() {
		t.Error("a published event reported as pending")
	}
}

// The adapter is what lets pkg/outbox stay independent of any service: the package
// declares the contract, and each service supplies it over its own generated Querier.
func TestTheOutboxStoreAdaptsTheRepositoryToThePackageContract(t *testing.T) {
	t.Parallel()

	fake := &fakeOutbox{}
	store := repository.NewOutboxStore(fake)

	// It satisfies both contracts pkg/outbox declares.
	var _ outbox.Store = store
	var _ outbox.Inserter = store

	// Writing through the adapter reaches the underlying repository, translating the
	// package's event shape into the domain one.
	_, err := store.Insert(t.Context(), outbox.NewEvent{
		Aggregate:   "user",
		AggregateID: "u1",
		RoutingKey:  "user.registered",
		Payload:     json.RawMessage(`{"user_id":"u1"}`),
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if len(fake.inserted) != 1 {
		t.Fatalf("%d events reached the repository, want 1", len(fake.inserted))
	}

	got := fake.inserted[0]

	if got.Aggregate != "user" || got.AggregateID != "u1" || got.RoutingKey != "user.registered" {
		t.Errorf("the adapter mangled the event: %+v", got)
	}
}

// Claiming has to translate back, so the worker never sees a domain type.
func TestTheOutboxStoreTranslatesClaimedEvents(t *testing.T) {
	t.Parallel()

	fake := &fakeOutbox{events: []repository.OutboxEvent{
		{ID: 9, Aggregate: "booking", RoutingKey: "booking.confirmed", Attempts: 2},
	}}

	events, err := repository.NewOutboxStore(fake).ClaimPending(t.Context(), 10)
	if err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("%d events, want 1", len(events))
	}

	if events[0].ID != 9 || events[0].RoutingKey != "booking.confirmed" || events[0].Attempts != 2 {
		t.Errorf("the translated event is wrong: %+v", events[0])
	}
}
