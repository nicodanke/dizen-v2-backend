package amqp

import (
	"context"
	"fmt"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"

	"github.com/nicodanke/dizen-v2-backend/pkg/health"
)

// CheckName is how the broker appears in the /readyz report.
const CheckName = "amqp"

// Connection wraps the broker connection and the channel, guarding both with a mutex so a
// publisher and a consumer can share one connection safely.
type Connection struct {
	mu      sync.RWMutex
	conn    *amqp.Connection
	channel *amqp.Channel

	cfg Config
	log zerolog.Logger
}

// Connect opens the connection and declares the exchange.
func Connect(cfg Config, log zerolog.Logger) (*Connection, error) {
	cfg = cfg.withDefaults()

	conn, err := amqp.Dial(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("connecting to RabbitMQ: %w", err)
	}

	channel, err := conn.Channel()
	if err != nil {
		_ = conn.Close()

		return nil, fmt.Errorf("opening the AMQP channel: %w", err)
	}

	if err := declareTopology(channel, cfg.Exchange, QueueSpec{}); err != nil {
		_ = channel.Close()
		_ = conn.Close()

		return nil, err
	}

	return &Connection{conn: conn, channel: channel, cfg: cfg, log: log}, nil
}

// Channel returns the shared channel.
func (c *Connection) Channel() *amqp.Channel {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.channel
}

// Config returns the configuration the connection was built with.
func (c *Connection) Config() Config {
	return c.cfg
}

// Ping reports whether the connection is usable. amqp091 has no ping, so liveness is read
// from the connection state, which the library keeps current from the heartbeat.
func (c *Connection) Ping(context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.conn == nil || c.conn.IsClosed() {
		return ErrConnectionClosed
	}

	return nil
}

// Close releases the channel and the connection.
func (c *Connection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.channel != nil {
		if err := c.channel.Close(); err != nil {
			c.log.Warn().Err(err).Msg("closing the AMQP channel")
		}
	}

	if c.conn != nil && !c.conn.IsClosed() {
		if err := c.conn.Close(); err != nil {
			return fmt.Errorf("closing the AMQP connection: %w", err)
		}
	}

	return nil
}

// HealthCheck is the readiness probe for the broker.
//
// It is critical: events are how the system stays consistent across services, and a
// service that cannot publish is silently dropping work rather than failing loudly.
func (c *Connection) HealthCheck() health.Check {
	return health.Check{
		Name:     CheckName,
		Critical: true,
		Probe:    c.Ping,
	}
}
