package amqp

import (
	"context"
	"fmt"
	"strconv"
	"time"

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

	return &Consumer{conn: conn, channel: channel, spec: spec, log: log}, nil
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

// Consume reads messages until the context is canceled.
func (c *Consumer) Consume(ctx context.Context, handler Handler) error {
	deliveries, err := c.channel.ConsumeWithContext(ctx, c.spec.Name, "", // broker-assigned tag
		false, // manual acknowledgement: the retry policy depends on it
		false, // not exclusive
		false, // no-local is unsupported by RabbitMQ
		false, // wait for confirmation
		nil,
	)
	if err != nil {
		return fmt.Errorf("consuming from %q: %w", c.spec.Name, err)
	}

	c.log.Info().
		Str("queue", c.spec.Name).
		Int("prefetch", c.conn.cfg.Prefetch).
		Int32("max_attempts", c.conn.cfg.MaxAttempts).
		Msg("consuming")

	for {
		select {
		case <-ctx.Done():
			c.drain(deliveries)

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

// drainTimeout bounds how long a shutdown waits for the broker to finish canceling the
// consumer. It is generous next to a graceful shutdown and short next to a hung one.
const drainTimeout = 5 * time.Second

// drain reads what is left on the deliveries channel until the library closes it.
//
// Returning from Consume without draining is a deadlock, and a quiet one. When the context
// ends, the library cancels the consumer, and that cancellation travels on the same dispatch
// loop that feeds this channel; an unread channel stalls the loop, so the cancellation is
// never confirmed and the Close that follows waits for a reply that cannot arrive. Nothing
// errors: the shutdown simply never finishes.
//
// The deliveries are discarded rather than handled, because the context they would run under
// is already over. They were never acknowledged, so the broker redelivers them -- which is
// the behavior the retry policy expects anyway.
func (c *Consumer) drain(deliveries <-chan amqp.Delivery) {
	timeout := time.NewTimer(drainTimeout)
	defer timeout.Stop()

	for {
		select {
		case _, ok := <-deliveries:
			if !ok {
				return
			}

		case <-timeout.C:
			c.log.Warn().
				Str("queue", c.spec.Name).
				Dur("after", drainTimeout).
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
func (c *Consumer) Close() error {
	if err := c.channel.Close(); err != nil {
		return fmt.Errorf("closing the consumer channel: %w", err)
	}

	return nil
}
