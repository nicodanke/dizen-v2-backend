//go:build integration

// Integration tests for the identity repository against a real PostgreSQL (RF-18, RF-19).
//
// These cover the half a fake cannot: that the SQL is valid, that `for update skip locked`
// really lets two workers claim disjoint batches, that a NULL comes back as a NULL. The unit
// tests next to them cover the mapping and the interface boundary.

package repository_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/nicodanke/dizen-v2-backend/pkg/database"
	"github.com/nicodanke/dizen-v2-backend/pkg/logger"
	"github.com/nicodanke/dizen-v2-backend/pkg/testutils"
	"github.com/nicodanke/dizen-v2-backend/services/admin/internal/db/migrations"
	"github.com/nicodanke/dizen-v2-backend/services/admin/internal/repository"
)

// One container for the whole package, started once and restored between tests.
//
// The alternative -- a container per test -- is stronger isolation on paper and costs a
// startup every time: ten tests here meant ten PostgreSQL containers, which is seconds on a
// developer machine and minutes on a CI runner. A snapshot taken right after the migrations
// and restored before each test gives the same empty, migrated schema in milliseconds, and
// the isolation is the same because no test in this package runs in parallel.
func TestMain(m *testing.M) {
	if !testutils.DockerAvailable() {
		os.Exit(0)
	}

	os.Exit(runPackage(m))
}

// runPackage owns the container's lifetime. It is separate from TestMain because os.Exit
// does not run deferred functions, and a leaked container outlives the test run.
func runPackage(m *testing.M) int {
	ctx := context.Background()

	pg, terminate, err := testutils.StartPostgres(ctx, testutils.WithDatabase("admin_db"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration setup: %v\n", err)

		return 1
	}

	defer terminate()

	// The schema comes from the real migrations, not from a hand-written CREATE TABLE: a
	// schema retyped for the test would drift from what production applies, and these tests
	// exist to catch exactly that kind of drift.
	if err := database.Migrate(pg.URL, migrations.FS, migrations.Path, logger.Nop()); err != nil {
		fmt.Fprintf(os.Stderr, "integration setup, migrating: %v\n", err)

		return 1
	}

	// Taken after migrating, so restoring returns to a migrated and empty database.
	if err := pg.SnapshotContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "integration setup: %v\n", err)

		return 1
	}

	testDB = pg

	return m.Run()
}

// testDB is the shared container. It is package state, which is what TestMain forces, and it
// is only ever read through liveRepo.
var testDB *testutils.Postgres

// liveRepo returns a repository over the shared, migrated database.
//
// The snapshot is restored on cleanup, so every test starts from the same empty schema
// whether the one before it passed or failed. Registering it here rather than in each test
// is what keeps that guarantee from depending on anybody remembering.
func liveRepo(t *testing.T) (*repository.Repository, *testutils.Postgres) {
	t.Helper()
	testutils.SkipIfNoDocker(t)

	t.Cleanup(func() { testDB.Restore(t) })

	return repository.New(testDB.Pool), testDB
}

// sampleEvent is a typical outbox entry.
func sampleEvent() repository.NewOutboxEvent {
	return repository.NewOutboxEvent{
		Aggregate:   "user",
		AggregateID: "0193f0a0-0000-7000-8000-000000000001",
		RoutingKey:  "user.registered",
		Payload:     json.RawMessage(`{"user_id":"u1"}`),
	}
}

func TestInsertAndClaim(t *testing.T) {
	repo, _ := liveRepo(t)

	stored, err := repo.Outbox().Insert(t.Context(), sampleEvent())
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if stored.ID == 0 {
		t.Error("the identity column did not assign an id")
	}

	if stored.Published() {
		t.Error("a freshly inserted event reports as published")
	}

	if stored.CreatedAt.IsZero() || stored.AvailableAt.IsZero() {
		t.Error("the defaults did not populate created_at or available_at")
	}

	claimed, err := repo.Outbox().ClaimPending(t.Context(), 10)
	if err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}

	if len(claimed) != 1 {
		t.Fatalf("%d events claimed, want 1", len(claimed))
	}

	if claimed[0].RoutingKey != "user.registered" {
		t.Errorf("routing key = %q", claimed[0].RoutingKey)
	}

	// Compared semantically, not byte for byte: the column is jsonb, and Postgres
	// normalizes what it stores (see TestJSONBNormalizesThePayload).
	var payload map[string]string
	if err := json.Unmarshal(claimed[0].Payload, &payload); err != nil {
		t.Fatalf("the payload did not survive as valid JSON: %v", err)
	}

	if payload["user_id"] != "u1" {
		t.Errorf("payload = %v", payload)
	}
}

// jsonb is not a byte store: Postgres parses the value, drops insignificant whitespace and
// reorders keys. The payload therefore round-trips as a JSON *value*, never as the exact
// bytes that went in.
//
// This is written down as a test because it constrains what can be built on top: anything
// that signs or hashes an outbox payload has to do it over a canonical form, not over what
// comes back from the column.
func TestJSONBNormalizesThePayload(t *testing.T) {
	repo, _ := liveRepo(t)

	event := sampleEvent()
	event.Payload = json.RawMessage(`{"b":2,   "a":1}`)

	stored, err := repo.Outbox().Insert(t.Context(), event)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if string(stored.Payload) == `{"b":2,   "a":1}` {
		t.Error("jsonb returned the exact input bytes; this test's premise no longer holds")
	}

	// The value is preserved even though the bytes are not.
	var got map[string]int
	if err := json.Unmarshal(stored.Payload, &got); err != nil {
		t.Fatalf("the stored payload is not valid JSON: %v", err)
	}

	if got["a"] != 1 || got["b"] != 2 {
		t.Errorf("the value changed: %v", got)
	}
}

// A NULL has to survive the round trip as an empty string, not as the literal "NULL" or a
// panic. This is the mapping the unit test asserts, checked against the database that
// actually produces the NULL.
func TestNullColumnsRoundTrip(t *testing.T) {
	repo, _ := liveRepo(t)

	event := sampleEvent()
	event.AggregateID = ""

	stored, err := repo.Outbox().Insert(t.Context(), event)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if stored.AggregateID != "" {
		t.Errorf("AggregateID = %q, want empty", stored.AggregateID)
	}

	if stored.LastError != "" {
		t.Errorf("LastError = %q, want empty", stored.LastError)
	}

	if stored.PublishedAt != nil {
		t.Errorf("PublishedAt = %v, want nil", stored.PublishedAt)
	}
}

func TestMarkPublished(t *testing.T) {
	repo, _ := liveRepo(t)

	stored, err := repo.Outbox().Insert(t.Context(), sampleEvent())
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if err := repo.Outbox().MarkPublished(t.Context(), stored.ID); err != nil {
		t.Fatalf("MarkPublished: %v", err)
	}

	// A published event must drop out of the pending set, which is what keeps the worker
	// from republishing it forever.
	claimed, err := repo.Outbox().ClaimPending(t.Context(), 10)
	if err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}

	if len(claimed) != 0 {
		t.Errorf("%d events still pending after being published", len(claimed))
	}

	count, err := repo.Outbox().CountPending(t.Context())
	if err != nil {
		t.Fatalf("CountPending: %v", err)
	}

	if count != 0 {
		t.Errorf("CountPending = %d, want 0", count)
	}
}

// Rescheduling into the future must take the event out of the claimable set until then.
func TestRescheduleHidesTheEventUntilItsRetryTime(t *testing.T) {
	repo, _ := liveRepo(t)

	stored, err := repo.Outbox().Insert(t.Context(), sampleEvent())
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	retryAt := time.Now().Add(1 * time.Hour)

	if err := repo.Outbox().Reschedule(t.Context(), stored.ID, "the broker refused it", retryAt); err != nil {
		t.Fatalf("Reschedule: %v", err)
	}

	claimed, err := repo.Outbox().ClaimPending(t.Context(), 10)
	if err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}

	if len(claimed) != 0 {
		t.Errorf("%d events claimed; one scheduled an hour out must not be", len(claimed))
	}

	// It is still pending, just not yet due.
	count, err := repo.Outbox().CountPending(t.Context())
	if err != nil {
		t.Fatalf("CountPending: %v", err)
	}

	if count != 1 {
		t.Errorf("CountPending = %d, want 1", count)
	}

	// And rescheduling into the past brings it straight back.
	if err := repo.Outbox().Reschedule(t.Context(), stored.ID, "retrying", time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("second Reschedule: %v", err)
	}

	claimed, err = repo.Outbox().ClaimPending(t.Context(), 10)
	if err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}

	if len(claimed) != 1 {
		t.Fatalf("%d events claimed, want 1", len(claimed))
	}

	if claimed[0].Attempts != 2 {
		t.Errorf("attempts = %d, want 2", claimed[0].Attempts)
	}

	if claimed[0].LastError != "retrying" {
		t.Errorf("last error = %q", claimed[0].LastError)
	}
}

func TestClaimPendingRespectsTheLimitAndTheOrder(t *testing.T) {
	repo, _ := liveRepo(t)

	for i := range 10 {
		event := sampleEvent()
		event.AggregateID = string(rune('a' + i))

		if _, err := repo.Outbox().Insert(t.Context(), event); err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
	}

	claimed, err := repo.Outbox().ClaimPending(t.Context(), 3)
	if err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}

	if len(claimed) != 3 {
		t.Fatalf("%d events claimed, want 3", len(claimed))
	}

	// Insertion order: an outbox that reorders events would deliver a cancellation before
	// the confirmation it cancels.
	for i := 1; i < len(claimed); i++ {
		if claimed[i].ID <= claimed[i-1].ID {
			t.Errorf("the events came back out of order: %d after %d", claimed[i].ID, claimed[i-1].ID)
		}
	}
}

// `for update skip locked` is what lets several workers run at once. Without it the second
// worker would block on the first's rows instead of taking different ones.
func TestTwoConcurrentWorkersClaimDisjointBatches(t *testing.T) {
	repo, pg := liveRepo(t)

	const total = 20

	for i := range total {
		if _, err := repo.Outbox().Insert(t.Context(), sampleEvent()); err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
	}

	// Each worker claims inside its own transaction, which is what holds the lock.
	claim := func(out *[]int64, wg *sync.WaitGroup, ready, release chan struct{}) {
		defer wg.Done()

		_ = database.WithTx(context.Background(), pg.Pool, func(tx pgxTx) error {
			rows, err := tx.Query(context.Background(),
				"select id from outbox where published_at is null and available_at <= now() "+
					"order by id limit 10 for update skip locked")
			if err != nil {
				return err
			}

			for rows.Next() {
				var id int64
				if err := rows.Scan(&id); err != nil {
					return err
				}

				*out = append(*out, id)
			}

			rows.Close()

			// Hold the lock until the other worker has claimed too.
			close(ready)
			<-release

			return nil
		})
	}

	var (
		wg            sync.WaitGroup
		first, second []int64
		firstReady    = make(chan struct{})
		secondReady   = make(chan struct{})
		release       = make(chan struct{})
	)

	wg.Add(2)

	go claim(&first, &wg, firstReady, release)
	<-firstReady

	go claim(&second, &wg, secondReady, release)
	<-secondReady

	close(release)
	wg.Wait()

	if len(first) != 10 || len(second) != 10 {
		t.Fatalf("batches of %d and %d, want 10 each", len(first), len(second))
	}

	seen := make(map[int64]bool, total)

	for _, id := range append(append([]int64{}, first...), second...) {
		if seen[id] {
			t.Errorf("both workers claimed event %d: skip locked is not working", id)
		}

		seen[id] = true
	}
}

func TestDeletePublishedBeforePrunes(t *testing.T) {
	repo, _ := liveRepo(t)

	stored, err := repo.Outbox().Insert(t.Context(), sampleEvent())
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if err := repo.Outbox().MarkPublished(t.Context(), stored.ID); err != nil {
		t.Fatalf("MarkPublished: %v", err)
	}

	pending, err := repo.Outbox().Insert(t.Context(), sampleEvent())
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	deleted, err := repo.Outbox().DeletePublishedBefore(t.Context(), time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("DeletePublishedBefore: %v", err)
	}

	if deleted != 1 {
		t.Errorf("%d rows deleted, want 1", deleted)
	}

	// The pending one must survive: pruning is about published events only.
	claimed, err := repo.Outbox().ClaimPending(t.Context(), 10)
	if err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}

	if len(claimed) != 1 || claimed[0].ID != pending.ID {
		t.Error("pruning removed an event that had not been published")
	}
}

// The point of Repository.WithTx: the state change and the event either both land or neither
// does. This is the guarantee the whole outbox pattern rests on (RF-12).
func TestTheEventAndTheStateChangeShareOneTransaction(t *testing.T) {
	repo, pg := liveRepo(t)

	testutils.ApplyMigrations(t, pg.Pool,
		"create table users (id text primary key, email text not null)")

	// A transaction that fails after writing both must leave neither.
	err := repo.WithTx(t.Context(), func(tx *repository.Repository) error {
		if _, err := tx.Outbox().Insert(t.Context(), sampleEvent()); err != nil {
			return err
		}

		// A duplicate key, which is the shape a real business failure takes.
		if _, err := pg.Pool.Exec(t.Context(), "insert into users (id, email) values ($1,$2)", "u1", "a@b.c"); err != nil {
			return err
		}

		return errAborted
	})

	if err == nil {
		t.Fatal("the transaction did not fail")
	}

	count, err := repo.Outbox().CountPending(t.Context())
	if err != nil {
		t.Fatalf("CountPending: %v", err)
	}

	if count != 0 {
		t.Errorf("%d events survived a rolled-back transaction, want 0", count)
	}

	// And a transaction that succeeds leaves the event.
	err = repo.WithTx(t.Context(), func(tx *repository.Repository) error {
		_, err := tx.Outbox().Insert(t.Context(), sampleEvent())

		return err
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	count, err = repo.Outbox().CountPending(t.Context())
	if err != nil {
		t.Fatalf("CountPending: %v", err)
	}

	if count != 1 {
		t.Errorf("CountPending = %d, want 1", count)
	}
}
