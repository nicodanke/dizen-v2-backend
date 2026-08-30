package outbox_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nicodanke/dizen-v2-backend/pkg/logger"
	"github.com/nicodanke/dizen-v2-backend/pkg/outbox"
)

// fakeStore is an in-memory outbox.
type fakeStore struct {
	mu sync.Mutex

	events      []outbox.Event
	published   []int64
	rescheduled []reschedule

	claimErr      error
	markErr       error
	rescheduleErr error
}

type reschedule struct {
	id      int64
	reason  string
	retryAt time.Time
}

func (f *fakeStore) ClaimPending(_ context.Context, limit int32) ([]outbox.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.claimErr != nil {
		return nil, f.claimErr
	}

	if int32(len(f.events)) <= limit {
		batch := f.events
		f.events = nil

		return batch, nil
	}

	batch := f.events[:limit]
	f.events = f.events[limit:]

	return batch, nil
}

func (f *fakeStore) MarkPublished(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.markErr != nil {
		return f.markErr
	}

	f.published = append(f.published, id)

	return nil
}

func (f *fakeStore) Reschedule(_ context.Context, id int64, reason string, retryAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.rescheduleErr != nil {
		return f.rescheduleErr
	}

	f.rescheduled = append(f.rescheduled, reschedule{id: id, reason: reason, retryAt: retryAt})

	return nil
}

func (f *fakeStore) CountPending(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return int64(len(f.events)), nil
}

// fakePublisher records what reached the broker.
type fakePublisher struct {
	mu sync.Mutex

	sent []string
	err  error
}

func (f *fakePublisher) Publish(_ context.Context, routingKey string, _ []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.err != nil {
		return f.err
	}

	f.sent = append(f.sent, routingKey)

	return nil
}

// fakeInserter records what a caller wrote inside its transaction.
type fakeInserter struct {
	inserted []outbox.NewEvent
	err      error
}

func (f *fakeInserter) Insert(_ context.Context, event outbox.NewEvent) (outbox.Event, error) {
	if f.err != nil {
		return outbox.Event{}, f.err
	}

	f.inserted = append(f.inserted, event)

	return outbox.Event{ID: int64(len(f.inserted)), RoutingKey: event.RoutingKey}, nil
}

func TestPublishRecordsTheEventOnTheGivenInserter(t *testing.T) {
	t.Parallel()

	inserter := &fakeInserter{}

	payload := map[string]string{"booking_id": "b1"}

	if err := outbox.Publish(t.Context(), inserter, "booking.confirmed", payload); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if len(inserter.inserted) != 1 {
		t.Fatalf("%d events recorded, want 1", len(inserter.inserted))
	}

	event := inserter.inserted[0]

	if event.RoutingKey != "booking.confirmed" {
		t.Errorf("routing key = %q", event.RoutingKey)
	}

	var decoded map[string]string
	if err := json.Unmarshal(event.Payload, &decoded); err != nil {
		t.Fatalf("the payload is not valid JSON: %v", err)
	}

	if decoded["booking_id"] != "b1" {
		t.Errorf("payload = %v", decoded)
	}
}

func TestPublishForAttributesTheEventToAnAggregate(t *testing.T) {
	t.Parallel()

	inserter := &fakeInserter{}

	err := outbox.PublishFor(t.Context(), inserter, "booking", "b1", "booking.confirmed", struct{}{})
	if err != nil {
		t.Fatalf("PublishFor: %v", err)
	}

	event := inserter.inserted[0]

	if event.Aggregate != "booking" || event.AggregateID != "b1" {
		t.Errorf("aggregate = %q/%q", event.Aggregate, event.AggregateID)
	}
}

// An event with no routing key is unroutable: it would sit in the table failing forever, so
// it is rejected where it is written.
func TestPublishRejectsAnEmptyRoutingKey(t *testing.T) {
	t.Parallel()

	inserter := &fakeInserter{}

	err := outbox.Publish(t.Context(), inserter, "", map[string]string{})
	if !errors.Is(err, outbox.ErrEmptyRoutingKey) {
		t.Errorf("the error is not ErrEmptyRoutingKey: %v", err)
	}

	if len(inserter.inserted) != 0 {
		t.Error("an event with no routing key was recorded")
	}
}

func TestPublishPassesRawJSONThrough(t *testing.T) {
	t.Parallel()

	inserter := &fakeInserter{}
	raw := json.RawMessage(`{"already":"encoded"}`)

	if err := outbox.Publish(t.Context(), inserter, "tour.published", raw); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if string(inserter.inserted[0].Payload) != `{"already":"encoded"}` {
		t.Errorf("the raw JSON was re-encoded: %s", inserter.inserted[0].Payload)
	}
}

// A payload that cannot be serialized has to fail the transaction, not be stored broken.
func TestPublishRejectsAnUnserializablePayload(t *testing.T) {
	t.Parallel()

	inserter := &fakeInserter{}

	err := outbox.Publish(t.Context(), inserter, "tour.published", make(chan int))
	if err == nil {
		t.Fatal("an unserializable payload was accepted")
	}

	if len(inserter.inserted) != 0 {
		t.Error("a broken event was recorded")
	}
}

// The guarantee of RF-12: an event is marked published only after the broker confirmed it.
func TestTheWorkerPublishesAndThenMarks(t *testing.T) {
	t.Parallel()

	store := &fakeStore{events: []outbox.Event{
		{ID: 1, RoutingKey: "user.registered", Payload: json.RawMessage(`{}`)},
		{ID: 2, RoutingKey: "booking.confirmed", Payload: json.RawMessage(`{}`)},
	}}
	publisher := &fakePublisher{}

	worker := outbox.NewWorker(store, publisher, outbox.WorkerConfig{}, logger.Nop())

	published, err := worker.ProcessBatch(t.Context())
	if err != nil {
		t.Fatalf("ProcessBatch: %v", err)
	}

	if published != 2 {
		t.Errorf("%d events published, want 2", published)
	}

	if len(publisher.sent) != 2 {
		t.Fatalf("%d events reached the broker, want 2", len(publisher.sent))
	}

	if len(store.published) != 2 {
		t.Errorf("%d events marked, want 2", len(store.published))
	}
}

// A publication that fails must not be marked published: the event has to survive for the
// next round.
func TestAFailedPublicationIsRescheduledAndNotMarked(t *testing.T) {
	t.Parallel()

	store := &fakeStore{events: []outbox.Event{
		{ID: 7, RoutingKey: "booking.confirmed", Payload: json.RawMessage(`{}`), Attempts: 0},
	}}
	publisher := &fakePublisher{err: errors.New("the broker did not confirm")}

	worker := outbox.NewWorker(store, publisher, outbox.WorkerConfig{}, logger.Nop())

	published, err := worker.ProcessBatch(t.Context())
	if err != nil {
		t.Fatalf("ProcessBatch: %v", err)
	}

	if published != 0 {
		t.Errorf("%d events reported published, want 0", published)
	}

	if len(store.published) != 0 {
		t.Error("a failed event was marked published")
	}

	if len(store.rescheduled) != 1 {
		t.Fatalf("%d events rescheduled, want 1", len(store.rescheduled))
	}

	got := store.rescheduled[0]

	if got.id != 7 {
		t.Errorf("rescheduled id = %d", got.id)
	}

	if got.reason == "" {
		t.Error("the failure reason was not recorded")
	}

	if !got.retryAt.After(time.Now()) {
		t.Errorf("retryAt is %s, it must be in the future", got.retryAt)
	}
}

// The retry has to back off: republishing at a fixed interval against a broker that is down
// is how the worker turns an outage into a hammering.
func TestTheRetryBacksOffWithTheAttemptCount(t *testing.T) {
	t.Parallel()

	cfg := outbox.WorkerConfig{InitialBackoff: time.Second, MaxBackoff: time.Minute}

	attemptCounts := []int32{0, 1, 2, 3}
	delays := make([]time.Duration, 0, len(attemptCounts))

	for _, attempts := range attemptCounts {
		store := &fakeStore{events: []outbox.Event{
			{ID: 1, RoutingKey: "x", Payload: json.RawMessage(`{}`), Attempts: attempts},
		}}
		publisher := &fakePublisher{err: errors.New("down")}

		worker := outbox.NewWorker(store, publisher, cfg, logger.Nop())

		if _, err := worker.ProcessBatch(t.Context()); err != nil {
			t.Fatalf("ProcessBatch: %v", err)
		}

		delays = append(delays, time.Until(store.rescheduled[0].retryAt).Round(time.Second))
	}

	for i := 1; i < len(delays); i++ {
		if delays[i] <= delays[i-1] {
			t.Errorf("the delay did not grow between attempt %d and %d: %v", i, i+1, delays)
		}
	}
}

// An event that was published but could not be marked is republished next round. That is
// the at-least-once side of the trade, and it must be reported as published so the round
// keeps draining.
func TestAnEventPublishedButNotMarkedCountsAsPublished(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		events:  []outbox.Event{{ID: 1, RoutingKey: "x", Payload: json.RawMessage(`{}`)}},
		markErr: errors.New("connection lost"),
	}
	publisher := &fakePublisher{}

	worker := outbox.NewWorker(store, publisher, outbox.WorkerConfig{}, logger.Nop())

	published, err := worker.ProcessBatch(t.Context())
	if err != nil {
		t.Fatalf("ProcessBatch: %v", err)
	}

	if published != 1 {
		t.Errorf("published = %d, want 1", published)
	}

	if len(publisher.sent) != 1 {
		t.Error("the event did not reach the broker")
	}
}

func TestProcessBatchReportsAFailureToClaim(t *testing.T) {
	t.Parallel()

	store := &fakeStore{claimErr: errors.New("the database is down")}

	worker := outbox.NewWorker(store, &fakePublisher{}, outbox.WorkerConfig{}, logger.Nop())

	if _, err := worker.ProcessBatch(t.Context()); err == nil {
		t.Error("a failure to claim was swallowed")
	}
}

// The loop has to stop when the context is canceled, or a shutdown would hang.
func TestRunStopsOnCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	worker := outbox.NewWorker(&fakeStore{}, &fakePublisher{}, outbox.WorkerConfig{
		PollInterval: 10 * time.Millisecond,
	}, logger.Nop())

	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned an error on cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Run did not stop when the context was canceled")
	}
}

// A draining backlog must not wait for the poll interval between rounds.
func TestRunDrainsTheBacklogWithoutWaiting(t *testing.T) {
	t.Parallel()

	events := make([]outbox.Event, 0, 250)
	for i := range 250 {
		events = append(events, outbox.Event{
			ID:         int64(i + 1),
			RoutingKey: "x",
			Payload:    json.RawMessage(`{}`),
		})
	}

	store := &fakeStore{events: events}
	publisher := &fakePublisher{}

	// A poll interval far longer than the test would allow: if the worker slept between
	// rounds, the backlog would not drain in time.
	worker := outbox.NewWorker(store, publisher, outbox.WorkerConfig{
		PollInterval: 30 * time.Second,
		BatchSize:    100,
	}, logger.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		_ = worker.Run(ctx)
		close(done)
	}()

	deadline := time.After(2 * time.Second)

	for {
		store.mu.Lock()
		sent := len(store.published)
		store.mu.Unlock()

		if sent == 250 {
			cancel()
			<-done

			return
		}

		select {
		case <-deadline:
			t.Fatalf("only %d of 250 events drained: the worker slept between rounds", sent)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
