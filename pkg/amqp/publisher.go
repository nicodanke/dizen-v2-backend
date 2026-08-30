package amqp

import (
	"context"
	"errors"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/nicodanke/dizen-v2-backend/pkg/logger"
)

// ErrNotConfirmed is returned when the broker did not confirm a publication.
//
// It matters because of the outbox (RF-12): the worker must only mark an event published
// after the broker has taken responsibility for it. Without confirms, a publish that was
// lost in the socket would be recorded as delivered.
var ErrNotConfirmed = errors.New("the broker did not confirm the publication")

// ErrConnectionClosed is returned when the connection is no longer usable.
var ErrConnectionClosed = errors.New("the AMQP connection is closed")

// Publisher publishes events with publisher confirms enabled (RF-11).
type Publisher struct {
	conn    *Connection
	channel *amqp.Channel
	confirm chan amqp.Confirmation
}

// NewPublisher opens a dedicated channel with confirms enabled.
//
// The channel is dedicated because confirm mode is a channel-level setting: sharing it
// with a consumer would put both under the same flow control.
func NewPublisher(conn *Connection) (*Publisher, error) {
	channel, err := conn.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("opening the publisher channel: %w", err)
	}

	if err := channel.Confirm(false); err != nil {
		_ = channel.Close()

		return nil, fmt.Errorf("enabling publisher confirms: %w", err)
	}

	return &Publisher{
		conn:    conn,
		channel: channel,
		confirm: channel.NotifyPublish(make(chan amqp.Confirmation, 1)),
	}, nil
}

// Publish sends a message and waits for the broker to confirm it.
//
// It blocks until the confirm arrives or the timeout expires. That is deliberate: the only
// caller that matters is the outbox worker, which must not advance until it knows the
// broker has the message.
func (p *Publisher) Publish(ctx context.Context, routingKey string, payload []byte) error {
	return p.publishTo(ctx, p.conn.cfg.Exchange, routingKey, payload, nil, "")
}

// PublishWithHeaders sends a message carrying extra headers.
func (p *Publisher) PublishWithHeaders(
	ctx context.Context,
	routingKey string,
	payload []byte,
	headers amqp.Table,
) error {
	return p.publishTo(ctx, p.conn.cfg.Exchange, routingKey, payload, headers, "")
}

// publishTo is the single publication path. Everything else funnels through it so the
// confirm handling exists in exactly one place.
func (p *Publisher) publishTo(
	ctx context.Context,
	exchange, routingKey string,
	payload []byte,
	headers amqp.Table,
	expiration string,
) error {
	if headers == nil {
		headers = amqp.Table{}
	}

	// The trace id travels with the message so a consumer's log can be correlated with the
	// request that produced the event (01 section 7).
	if traceID := logger.TraceID(ctx); traceID != "" {
		headers[HeaderTraceID] = traceID
	}

	ctx, cancel := context.WithTimeout(ctx, p.conn.cfg.PublishTimeout)
	defer cancel()

	err := p.channel.PublishWithContext(ctx, exchange, routingKey,
		true,  // mandatory: an event nobody is bound to must not vanish silently
		false, // immediate is deprecated and unsupported by RabbitMQ
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Timestamp:    time.Now().UTC(),
			Headers:      headers,
			Body:         payload,
			Expiration:   expiration,
		},
	)
	if err != nil {
		return fmt.Errorf("publishing %q: %w", routingKey, err)
	}

	return p.waitForConfirm(ctx, routingKey)
}

// waitForConfirm blocks until the broker acknowledges the publication.
func (p *Publisher) waitForConfirm(ctx context.Context, routingKey string) error {
	select {
	case confirmation, ok := <-p.confirm:
		if !ok {
			return ErrConnectionClosed
		}

		if !confirmation.Ack {
			return fmt.Errorf("%w: %s", ErrNotConfirmed, routingKey)
		}

		return nil

	case <-ctx.Done():
		return fmt.Errorf("%w: %s: %w", ErrNotConfirmed, routingKey, ctx.Err())
	}
}

// Close releases the publisher channel.
func (p *Publisher) Close() error {
	if err := p.channel.Close(); err != nil {
		return fmt.Errorf("closing the publisher channel: %w", err)
	}

	return nil
}
