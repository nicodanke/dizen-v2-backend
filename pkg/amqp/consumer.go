package amqp

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"

	"github.com/nicodanke/dizen-v2-backend/pkg/logger"
)

// RetrySuffix is appended to a queue name to build its retry queue.
const RetrySuffix = ".retry"

// RetryQueueName returns the retry queue name for a queue.
func RetryQueueName(queue string) string {
	return queue + RetrySuffix
}

// Handler processes one message. Returning an error triggers the retry policy; returning
// nil acknowledges the message.
//
// A handler must be idempotent: with at-least-once delivery the same message can arrive
// twice, and no amount of broker configuration changes that.
type Handler func(ctx context.Context, delivery amqp.Delivery) error

// Consumer reads from one queue, applying the retry and dead-letter policy of RF-11.
type Consumer struct {
	conn    *Connection
	channel *amqp.Channel
	spec    QueueSpec
	log     zerolog.Logger

	// tag names this consumer to the broker. It is ours rather than broker-assigned
	// because canceling by tag is what lets this type, instead of the library, decide
	// when the cancellation happens. See Consume.
	tag string

	// rpc serializes the channel operations that send a request and wait for its reply.
	//
	// amqp091's Channel.call does not serialize them: it takes the channel's single rpc
	// channel and selects on it. Two concurrent callers are therefore two receivers on
	// one channel, and the runtime hands each reply to whichever it picks -- so a cancel
	// can be handed the reply to a close, discard it, and leave the close waiting for a
	// frame the broker already sent. That deadlock is silent and lasts forever.
	rpc sync.Mutex

	// mu guards deliveries, which Consume sets and Close reads.
	mu sync.Mutex

	// deliveries is the channel the broker's messages arrive on, kept so that Close can
	// keep reading it while it waits. See keepDrained.
	deliveries <-chan amqp.Delivery
}

// NewConsumer declares the topology and opens a channel with the configured prefetch.
//
// The topology is declared on every start, which is safe because declaration is
// idempotent: several replicas booting at once all converge on the same queues.
func NewConsumer(conn *Connection, spec QueueSpec, log zerolog.Logger) (*Consumer, error) {
	channel, err := conn.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("opening the consumer channel: %w", err)
	}

	if err := declareTopology(channel, conn.cfg.Exchange, spec); err != nil {
		_ = channel.Close()

		return nil, err
	}

	if err := declareRetryQueue(channel, spec.Name); err != nil {
		_ = channel.Close()

		return nil, err
	}

	// Prefetch bounds how much work is in flight, and therefore how much is redelivered
	// when a consumer dies mid-batch.
	if err := channel.Qos(conn.cfg.Prefetch, 0, false); err != nil {
		_ = channel.Close()

		return nil, fmt.Errorf("setting the prefetch to %d: %w", conn.cfg.Prefetch, err)
	}

	return &Consumer{conn: conn, channel: channel, spec: spec, log: log, tag: consumerTag(spec.Name)}, nil
}

// declareRetryQueue declares the queue that implements the backoff.
//
// RabbitMQ has no native delayed delivery without a plugin. The standard way is a queue
// nobody consumes: a message is published there with a per-message TTL, and when it
// expires the queue dead-letters it back to the main one. The wait costs nothing and holds
// no consumer slot, which is why the backoff is not a sleep in the handler goroutine.
func declareRetryQueue(ch *amqp.Channel, queue string) error {
	if queue == "" {
		return nil
	}

	args := amqp.Table{
		"x-dead-letter-exchange":    "",
		"x-dead-letter-routing-key": queue,
	}

	if _, err := ch.QueueDeclare(RetryQueueName(queue), true, false, false, false, args); err != nil {
		return fmt.Errorf("declaring the retry queue %q: %w", RetryQueueName(queue), err)
	}

	return nil
}

// consumerTag names a consumer to the broker. The queue makes it readable in the
// management UI; the suffix makes it unique across processes and restarts.
func consumerTag(queue string) string {
	return queue + "-" + uuid.NewString()
}

// Consume reads messages until the context is canceled.
//
// The plain Consume is used rather than ConsumeWithContext, deliberately.
// ConsumeWithContext starts a goroutine that calls Channel.Cancel when the context ends,
// and that goroutine is outside this type's control: it can run at the same moment as
// Close, and two concurrent waiters on one channel's rpc reply deadlock (see the rpc
// field). Canceling here instead keeps every reply-waiting call on this channel under
// one mutex.
func (c *Consumer) Consume(ctx context.Context, handler Handler) error {
	deliveries, err := c.channel.Consume(c.spec.Name, c.tag,
		false, // manual acknowledgement: the retry policy depends on it
		false, // not exclusive
		false, // no-local is unsupported by RabbitMQ
		false, // wait for confirmation
		nil,
	)
	if err != nil {
		return fmt.Errorf("consuming from %q: %w", c.spec.Name, err)
	}

	c.mu.Lock()
	c.deliveries = deliveries
	c.mu.Unlock()

	c.log.Info().
		Str("queue", c.spec.Name).
		Int("prefetch", c.conn.cfg.Prefetch).
		Int32("max_attempts", c.conn.cfg.MaxAttempts).
		Msg("consuming")

	for {
		select {
		case <-ctx.Done():
			c.stop(deliveries)

			return nil

		case delivery, ok := <-deliveries:
			if !ok {
				// The channel closed: the connection dropped, and the supervisor above
				// decides whether to reconnect.
				return ErrConnectionClosed
			}

			c.handle(ctx, delivery, handler)
		}
	}
}

// drainTimeout bounds the wait for a broker that never confirms the cancellation. It is a
// safety net: a healthy cancellation takes milliseconds, so reaching this means something
// is wrong, and blocking a shutdown forever is the worse of the two failures.
const drainTimeout = 30 * time.Second

// keepDrained reads and discards deliveries in the background until the returned function
// is called, which then waits for that reading to have stopped.
//
// This exists because of how amqp091 dispatches. One goroutine reads every frame off the
// connection and hands each one to its destination, and it does so synchronously: a
// delivery it cannot hand over, because nothing is reading the consumer's channel, stops
// that goroutine entirely. Every other frame is then stuck behind it -- including the
// replies that a cancel or a close is waiting for. The result is a deadlock with no error
// and no timeout, and it is why both shutdown paths here drain before they wait.
func (c *Consumer) keepDrained() (stop func()) {
	c.mu.Lock()
	deliveries := c.deliveries
	c.mu.Unlock()

	if deliveries == nil {
		return func() {}
	}

	halt := make(chan struct{})
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)

		for {
			select {
			case _, ok := <-deliveries:
				if !ok {
					return
				}

			case <-halt:
				return
			}
		}
	}()

	return func() {
		close(halt)
		<-stopped
	}
}

// stop cancels the consumer and drains what the broker had already sent.
//
// The order matters and is not the obvious one. Canceling first and draining afterwards
// deadlocks: the library delivers through an intermediate goroutine that blocks writing to
// an unread deliveries channel, and while it is blocked it cannot process the cancellation
// that would close that channel. So the drain starts first and runs while the cancel is in
// flight; the cancel then completes, the library closes the channel, and the drain ends.
//
// Whatever is drained was never acknowledged, so the broker redelivers it. That is the
// same path a crashed consumer takes, and the retry policy already expects it.
func (c *Consumer) stop(deliveries <-chan amqp.Delivery) {
	drained := make(chan struct{})

	go func() {
		defer close(drained)

		c.drain(deliveries)
	}()

	c.cancel()

	<-drained
}

// cancel tells the broker to stop delivering, which is what closes the deliveries channel.
func (c *Consumer) cancel() {
	c.rpc.Lock()
	defer c.rpc.Unlock()

	// A channel closed underneath us -- a dropped connection -- has already canceled
	// every consumer on it. Asking again would send on a closed channel.
	if c.channel.IsClosed() {
		return
	}

	if err := c.channel.Cancel(c.tag, false); err != nil {
		c.log.Warn().Err(err).
			Str("queue", c.spec.Name).
			Msg("canceling the consumer")
	}
}

// drain reads the deliveries channel until the library closes it.
func (c *Consumer) drain(deliveries <-chan amqp.Delivery) {
	timeout := time.NewTimer(drainTimeout)
	defer timeout.Stop()

	drained := 0

	for {
		select {
		case _, ok := <-deliveries:
			if !ok {
				c.log.Debug().
					Str("queue", c.spec.Name).
					Int("redelivered", drained).
					Msg("the consumer was canceled and its deliveries drained")

				return
			}

			drained++

		case <-timeout.C:
			c.log.Warn().
				Str("queue", c.spec.Name).
				Dur("after", drainTimeout).
				Int("redelivered", drained).
				Msg("gave up draining the consumer; the broker did not confirm the cancellation")

			return
		}
	}
}

// handle runs the handler and applies the outcome.
func (c *Consumer) handle(ctx context.Context, delivery amqp.Delivery, handler Handler) {
	cfg := c.conn.cfg

	// The trace id traveled with the message; putting it back on the context is what ties
	// the consumer's log to the request that published the event.
	ctx = c.contextFor(ctx, delivery)

	handlerErr := handler(ctx, delivery)

	outcome := decide(handlerErr, attemptsOf(delivery), cfg.MaxAttempts, cfg.InitialBackoff, cfg.MaxBackoff)

	switch outcome.Action {
	case ActionAck:
		c.ack(ctx, delivery)

	case ActionRetry:
		c.scheduleRetry(ctx, delivery, outcome, handlerErr)

	case ActionDeadLetter:
		c.deadLetter(ctx, delivery, outcome, handlerErr)
	}
}

// contextFor rebuilds the request context from the message headers.
func (c *Consumer) contextFor(ctx context.Context, delivery amqp.Delivery) context.Context {
	log := c.log.With().
		Str("queue", c.spec.Name).
		Str("routing_key", delivery.RoutingKey).
		Logger()

	if traceID, ok := delivery.Headers[HeaderTraceID].(string); ok && traceID != "" {
		log = log.With().Str(logger.FieldTraceID, traceID).Logger()
	}

	return logger.WithContext(ctx, log)
}

// ack acknowledges a message the handler processed successfully.
func (c *Consumer) ack(ctx context.Context, delivery amqp.Delivery) {
	if err := delivery.Ack(false); err != nil {
		logger.Ctx(ctx).Error().Err(err).Msg("acknowledging the message")
	}
}

// scheduleRetry publishes the message to the retry queue, where its TTL implements the
// backoff, and acknowledges the original so the prefetch slot is released immediately.
func (c *Consumer) scheduleRetry(
	ctx context.Context,
	delivery amqp.Delivery,
	outcome Outcome,
	handlerErr error,
) {
	logger.Ctx(ctx).Warn().
		Err(handlerErr).
		Int32("attempt", outcome.Attempt).
		Int32("max_attempts", c.conn.cfg.MaxAttempts).
		Dur("retry_in", outcome.Delay).
		Msg("the handler failed, scheduling a retry")

	headers := retryHeaders(delivery, outcome.Attempt, handlerErr.Error())
	expiration := strconv.FormatInt(outcome.Delay.Milliseconds(), 10)

	err := c.publishRaw(ctx, "", RetryQueueName(c.spec.Name), delivery.Body, headers, expiration)
	if err != nil {
		// The retry could not be scheduled. The message is negatively acknowledged
		// without requeue, so the queue's dead-letter policy catches it rather than the
		// event being lost.
		logger.Ctx(ctx).Error().Err(err).Msg("could not schedule the retry, dead-lettering")

		if nackErr := delivery.Nack(false, false); nackErr != nil {
			logger.Ctx(ctx).Error().Err(nackErr).Msg("negatively acknowledging the message")
		}

		return
	}

	c.ack(ctx, delivery)
}

// deadLetter sends an exhausted message to the dead-letter queue.
//
// This is acceptance criterion 4 of PRD-00: after the attempts run out the message ends up
// in the DLQ and an error-level log is emitted carrying the trace_id.
func (c *Consumer) deadLetter(
	ctx context.Context,
	delivery amqp.Delivery,
	outcome Outcome,
	handlerErr error,
) {
	logger.Ctx(ctx).Error().
		Err(handlerErr).
		Int32("attempts", outcome.Attempt).
		Str("queue", c.spec.Name).
		Str("dlq", DLQName(c.spec.Name)).
		Msg("attempts exhausted, the message goes to the dead-letter queue")

	headers := retryHeaders(delivery, outcome.Attempt, handlerErr.Error())

	if err := c.publishRaw(ctx, "", DLQName(c.spec.Name), delivery.Body, headers, ""); err != nil {
		// Publishing to the DLQ failed. Nack without requeue falls back to the queue's
		// own dead-letter policy, which routes to the same place with fewer headers.
		logger.Ctx(ctx).Error().Err(err).Msg("could not publish to the DLQ, falling back to nack")

		if nackErr := delivery.Nack(false, false); nackErr != nil {
			logger.Ctx(ctx).Error().Err(nackErr).Msg("negatively acknowledging the message")
		}

		return
	}

	c.ack(ctx, delivery)
}

// publishRaw sends a message on the consumer channel, used for retries and dead-lettering.
func (c *Consumer) publishRaw(
	ctx context.Context,
	exchange, routingKey string,
	body []byte,
	headers amqp.Table,
	expiration string,
) error {
	err := c.channel.PublishWithContext(ctx, exchange, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Headers:      headers,
		Body:         body,
		Expiration:   expiration,
	})
	if err != nil {
		return fmt.Errorf("publishing to %q: %w", routingKey, err)
	}

	return nil
}

// Close releases the consumer channel.
//
// It takes the same mutex as cancel so that a Close arriving while Consume is still
// shutting down waits for that cancellation instead of racing it for the reply, and it
// keeps the deliveries moving while it waits so that its own confirmation can reach it.
//
// Both are needed. Serializing alone still deadlocks when Close wins the mutex before
// Consume has started draining: the close confirmation then queues behind a delivery that
// nobody is reading.
func (c *Consumer) Close() error {
	release := c.keepDrained()
	defer release()

	c.rpc.Lock()
	defer c.rpc.Unlock()

	if err := c.channel.Close(); err != nil {
		return fmt.Errorf("closing the consumer channel: %w", err)
	}

	return nil
}
