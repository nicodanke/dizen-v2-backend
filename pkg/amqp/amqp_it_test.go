//go:build integration

// Integration tests for pkg/amqp against a real RabbitMQ (RF-18).
//
// This is where acceptance criterion 4 is actually verified: the unit tests check the retry
// policy as a pure decision, and these check that a message really ends up in the
// dead-letter queue after the attempts run out.

package amqp_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	amqp091 "github.com/rabbitmq/amqp091-go"

	"github.com/nicodanke/dizen-v2-backend/pkg/amqp"
	"github.com/nicodanke/dizen-v2-backend/pkg/logger"
	"github.com/nicodanke/dizen-v2-backend/pkg/testutils"
)

// brokerURL points at the broker shared by the whole package.
//
// One broker, not one per test: RabbitMQ is by far the slowest of the three containers to
// boot. Sharing it requires TestMain, because registering the teardown on the first test
// would tear the container down as soon as that test finished. Tests isolate themselves
// with a unique queue name instead.
var brokerURL string

func TestMain(m *testing.M) {
	if !testutils.DockerAvailable() {
		fmt.Fprintln(os.Stderr, "skipping the amqp integration tests: Docker is not available")
		os.Exit(0)
	}

	url, terminate, err := testutils.StartAMQP(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not start RabbitMQ: %v\n", err)
		os.Exit(1)
	}

	brokerURL = url

	code := m.Run()

	terminate()
	os.Exit(code)
}

// brokerFor returns the shared broker URL.
func brokerFor(t *testing.T) string {
	t.Helper()

	if brokerURL == "" {
		t.Skip("the broker did not start")
	}

	return brokerURL
}

// config builds a configuration pointing at the shared broker.
func config(t *testing.T) amqp.Config {
	t.Helper()

	return amqp.Config{
		URL:            brokerFor(t),
		Exchange:       "dizen.events.test",
		Prefetch:       10,
		MaxAttempts:    5,
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     300 * time.Millisecond,
		PublishTimeout: 5 * time.Second,
	}
}

// connect opens a connection closed when the test ends.
func connect(t *testing.T, cfg amqp.Config) *amqp.Connection {
	t.Helper()

	conn, err := amqp.Connect(cfg, logger.Nop())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	t.Cleanup(func() { _ = conn.Close() })

	return conn
}

// uniqueQueue keeps tests from sharing a queue, which is what lets them run against one
// broker without interfering.
func uniqueQueue(t *testing.T) string {
	t.Helper()

	return fmt.Sprintf("test.%s.%d", t.Name(), time.Now().UnixNano())
}

func TestPublishAndConsumeRoundTrip(t *testing.T) {
	cfg := config(t)
	conn := connect(t, cfg)

	queue := uniqueQueue(t)

	consumer, err := amqp.NewConsumer(conn, amqp.QueueSpec{
		Name:        queue,
		RoutingKeys: []string{"user.registered"},
		Durable:     true,
	}, logger.Nop())
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}

	defer func() { _ = consumer.Close() }()

	publisher, err := amqp.NewPublisher(conn)
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}

	defer func() { _ = publisher.Close() }()

	received := make(chan []byte, 1)

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	go func() {
		_ = consumer.Consume(ctx, func(_ context.Context, d amqp091.Delivery) error {
			received <- d.Body

			return nil
		})
	}()

	// The consumer needs a moment to register before the message is published.
	time.Sleep(500 * time.Millisecond)

	payload := []byte(`{"user_id":"u1"}`)

	if err := publisher.Publish(ctx, "user.registered", payload); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case body := <-received:
		if string(body) != string(payload) {
			t.Errorf("body = %s, want %s", body, payload)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the message never arrived")
	}
}

// Publisher confirms are what the outbox depends on: without them a publish lost in the
// socket would be recorded as delivered (RF-12).
func TestPublishWaitsForTheBrokerConfirm(t *testing.T) {
	cfg := config(t)
	conn := connect(t, cfg)

	queue := uniqueQueue(t)

	if _, err := amqp.NewConsumer(conn, amqp.QueueSpec{
		Name:        queue,
		RoutingKeys: []string{"tour.published"},
		Durable:     true,
	}, logger.Nop()); err != nil {
		t.Fatalf("declaring the queue: %v", err)
	}

	publisher, err := amqp.NewPublisher(conn)
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}

	defer func() { _ = publisher.Close() }()

	// Publishing several in a row exercises the confirm channel rather than a single
	// lucky round trip.
	for i := range 20 {
		payload, _ := json.Marshal(map[string]int{"n": i})

		if err := publisher.Publish(t.Context(), "tour.published", payload); err != nil {
			t.Fatalf("publish %d was not confirmed: %v", i, err)
		}
	}
}

// This is acceptance criterion 4 of PRD-00, end to end: a message that fails five times ends
// up in the dead-letter queue.
func TestAMessageThatKeepsFailingEndsInTheDeadLetterQueue(t *testing.T) {
	cfg := config(t)
	conn := connect(t, cfg)

	queue := uniqueQueue(t)

	consumer, err := amqp.NewConsumer(conn, amqp.QueueSpec{
		Name:        queue,
		RoutingKeys: []string{"booking.confirmed"},
		Durable:     true,
	}, logger.Nop())
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}

	defer func() { _ = consumer.Close() }()

	publisher, err := amqp.NewPublisher(conn)
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}

	defer func() { _ = publisher.Close() }()

	var (
		mu       sync.Mutex
		attempts int
	)

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	go func() {
		_ = consumer.Consume(ctx, func(context.Context, amqp091.Delivery) error {
			mu.Lock()
			attempts++
			mu.Unlock()

			return errors.New("the handler always fails")
		})
	}()

	time.Sleep(500 * time.Millisecond)

	if err := publisher.Publish(ctx, "booking.confirmed", []byte(`{"booking_id":"b1"}`)); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Poll the dead-letter queue until the message lands there.
	dlq := amqp.DLQName(queue)

	deadline := time.After(45 * time.Second)

	for {
		count, err := queueDepth(t, cfg.URL, dlq)
		if err != nil {
			t.Fatalf("reading %s: %v", dlq, err)
		}

		if count > 0 {
			break
		}

		select {
		case <-deadline:
			mu.Lock()
			got := attempts
			mu.Unlock()

			t.Fatalf("the message never reached %s after %d attempts", dlq, got)
		case <-time.After(500 * time.Millisecond):
		}
	}

	mu.Lock()
	got := attempts
	mu.Unlock()

	// Five attempts, no more: the sixth would mean the limit is not being honored.
	if got != int(cfg.MaxAttempts) {
		t.Errorf("the handler ran %d times, want %d", got, cfg.MaxAttempts)
	}

	// The dead-lettered message has to carry the diagnosis, so the queue is readable
	// without cross-referencing logs.
	headers, body := firstMessage(t, cfg.URL, dlq)

	if string(body) != `{"booking_id":"b1"}` {
		t.Errorf("the body was mangled: %s", body)
	}

	if headers[amqp.HeaderLastError] == nil {
		t.Error("the dead-lettered message carries no last error")
	}

	if headers[amqp.HeaderAttempts] == nil {
		t.Error("the dead-lettered message carries no attempt count")
	}
}

// A handler that fails once and then succeeds must not reach the dead-letter queue: the
// retry has to actually redeliver.
func TestAMessageThatRecoversIsNotDeadLettered(t *testing.T) {
	cfg := config(t)
	conn := connect(t, cfg)

	queue := uniqueQueue(t)

	consumer, err := amqp.NewConsumer(conn, amqp.QueueSpec{
		Name:        queue,
		RoutingKeys: []string{"subscription.renewed"},
		Durable:     true,
	}, logger.Nop())
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}

	defer func() { _ = consumer.Close() }()

	publisher, err := amqp.NewPublisher(conn)
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}

	defer func() { _ = publisher.Close() }()

	var (
		mu       sync.Mutex
		attempts int
	)

	succeeded := make(chan struct{})

	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()

	go func() {
		_ = consumer.Consume(ctx, func(context.Context, amqp091.Delivery) error {
			mu.Lock()
			attempts++
			current := attempts
			mu.Unlock()

			if current < 3 {
				return errors.New("not yet")
			}

			close(succeeded)

			return nil
		})
	}()

	time.Sleep(500 * time.Millisecond)

	if err := publisher.Publish(ctx, "subscription.renewed", []byte(`{}`)); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case <-succeeded:
	case <-time.After(30 * time.Second):
		t.Fatal("the message never succeeded on a retry")
	}

	// Give the ack a moment, then check nothing was dead-lettered.
	time.Sleep(1 * time.Second)

	count, err := queueDepth(t, cfg.URL, amqp.DLQName(queue))
	if err != nil {
		t.Fatalf("reading the DLQ: %v", err)
	}

	if count != 0 {
		t.Errorf("%d messages reached the DLQ; a message that recovered must not", count)
	}
}

// Declaring the topology twice must be a no-op: every replica does it at startup (RF-11).
func TestTopologyDeclarationIsIdempotent(t *testing.T) {
	cfg := config(t)
	conn := connect(t, cfg)

	queue := uniqueQueue(t)

	spec := amqp.QueueSpec{Name: queue, RoutingKeys: []string{"tour.published"}, Durable: true}

	for i := range 3 {
		consumer, err := amqp.NewConsumer(conn, spec, logger.Nop())
		if err != nil {
			t.Fatalf("declaration %d failed: %v", i+1, err)
		}

		_ = consumer.Close()
	}
}

func TestPingReportsAClosedConnection(t *testing.T) {
	cfg := config(t)

	conn, err := amqp.Connect(cfg, logger.Nop())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if err := conn.Ping(t.Context()); err != nil {
		t.Errorf("Ping on a live connection: %v", err)
	}

	if err := conn.HealthCheck().Probe(t.Context()); err != nil {
		t.Errorf("the health probe failed on a live connection: %v", err)
	}

	_ = conn.Close()

	if err := conn.Ping(t.Context()); err == nil {
		t.Error("Ping succeeded on a closed connection")
	}
}

// queueDepth returns how many messages a queue holds, using a passive declare so it never
// creates the queue by accident.
func queueDepth(t *testing.T, url, queue string) (int, error) {
	t.Helper()

	conn, err := amqp091.Dial(url)
	if err != nil {
		return 0, err
	}

	defer func() { _ = conn.Close() }()

	ch, err := conn.Channel()
	if err != nil {
		return 0, err
	}

	defer func() { _ = ch.Close() }()

	q, err := ch.QueueDeclarePassive(queue, true, false, false, false, nil)
	if err != nil {
		// The queue may not exist yet, which reads as empty rather than as a failure.
		return 0, nil
	}

	return q.Messages, nil
}

// firstMessage takes one message off a queue and returns its headers and body.
func firstMessage(t *testing.T, url, queue string) (amqp091.Table, []byte) {
	t.Helper()

	conn, err := amqp091.Dial(url)
	if err != nil {
		t.Fatalf("dialing: %v", err)
	}

	defer func() { _ = conn.Close() }()

	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("opening a channel: %v", err)
	}

	defer func() { _ = ch.Close() }()

	delivery, ok, err := ch.Get(queue, true)
	if err != nil {
		t.Fatalf("getting from %s: %v", queue, err)
	}

	if !ok {
		t.Fatalf("%s is empty", queue)
	}

	return delivery.Headers, delivery.Body
}

// A different attempt limit has to be honored end to end, not just by the decision function:
// this is what proves MaxAttempts reaches the consumer rather than being read from a default.
func TestTheConfiguredAttemptLimitIsHonored(t *testing.T) {
	cfg := config(t)
	cfg.MaxAttempts = 2

	conn := connect(t, cfg)
	queue := uniqueQueue(t)

	consumer, err := amqp.NewConsumer(conn, amqp.QueueSpec{
		Name:        queue,
		RoutingKeys: []string{"tour_run.completed"},
		Durable:     true,
	}, logger.Nop())
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}

	defer func() { _ = consumer.Close() }()

	publisher, err := amqp.NewPublisher(conn)
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}

	defer func() { _ = publisher.Close() }()

	var (
		mu       sync.Mutex
		attempts int
	)

	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()

	go func() {
		_ = consumer.Consume(ctx, func(context.Context, amqp091.Delivery) error {
			mu.Lock()
			attempts++
			mu.Unlock()

			return errors.New("always fails")
		})
	}()

	time.Sleep(500 * time.Millisecond)

	if err := publisher.Publish(ctx, "tour_run.completed", []byte(`{}`)); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	deadline := time.After(30 * time.Second)

	for {
		count, err := queueDepth(t, cfg.URL, amqp.DLQName(queue))
		if err != nil {
			t.Fatalf("reading the DLQ: %v", err)
		}

		if count > 0 {
			break
		}

		select {
		case <-deadline:
			t.Fatal("the message never reached the DLQ")
		case <-time.After(500 * time.Millisecond):
		}
	}

	mu.Lock()
	got := attempts
	mu.Unlock()

	if got != 2 {
		t.Errorf("the handler ran %d times, want 2: MaxAttempts did not reach the consumer", got)
	}
}

// TestCloseReturnsAfterTheContextEndsWithMessagesInFlight is a regression test for a
// shutdown that never finished.
//
// Consume used to return the moment the context ended, leaving the deliveries channel
// unread. The library cancels the consumer when the context ends, and that cancellation
// travels on the same dispatch loop that feeds the channel: an unread channel stalls the
// loop, the cancellation is never confirmed, and Close waits forever for a reply that cannot
// arrive. Nothing errors, which is what made it hard to see -- in CI it surfaced only as a
// test binary killed at its timeout.
//
// The condition needs messages still in flight when the context ends, which is what the
// prefetch and the blocked handler below arrange.
func TestCloseReturnsAfterTheContextEndsWithMessagesInFlight(t *testing.T) {
	cfg := config(t)
	conn := connect(t, cfg)

	queue := uniqueQueue(t)

	consumer, err := amqp.NewConsumer(conn, amqp.QueueSpec{
		Name:        queue,
		RoutingKeys: []string{"user.registered"},
		Durable:     true,
	}, logger.Nop())
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}

	publisher, err := amqp.NewPublisher(conn)
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}

	defer func() { _ = publisher.Close() }()

	ctx, cancel := context.WithCancel(t.Context())

	consuming := make(chan struct{})
	blocked := make(chan struct{})

	go func() {
		defer close(consuming)

		_ = consumer.Consume(ctx, func(_ context.Context, _ amqp091.Delivery) error {
			// The first delivery parks here, so the ones behind it stay unread in the
			// library's buffer -- which is the state that used to deadlock the close.
			<-blocked

			return nil
		})
	}()

	time.Sleep(500 * time.Millisecond)

	for range 5 {
		if err := publisher.Publish(t.Context(), "user.registered", []byte(`{"user_id":"u1"}`)); err != nil {
			t.Fatalf("Publish: %v", err)
		}
	}

	time.Sleep(time.Second)

	cancel()
	close(blocked)

	select {
	case <-consuming:
	case <-time.After(30 * time.Second):
		t.Fatal("Consume did not return after the context ended")
	}

	done := make(chan error, 1)
	go func() { done <- consumer.Close() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Close blocked after the context ended: the deliveries were not drained")
	}
}
