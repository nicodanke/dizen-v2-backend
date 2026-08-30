// Package amqp is the RabbitMQ publisher and consumer (RF-11).
//
// The use case is a task queue, not an event log (ADR A-3): send an email, send a push,
// invalidate a cache. That is why the design leans on publisher confirms, dead-letter
// queues and competing consumers rather than on retention and replay.
package amqp

import "time"

// Exchange and topology defaults, fixed by 01 section 4.3.
const (
	// DefaultExchange is the topic exchange every domain event is published to.
	DefaultExchange = "dizen.events"

	// DefaultExchangeKind is a topic exchange, so a consumer binds by routing pattern.
	DefaultExchangeKind = "topic"

	// DLQSuffix is appended to a queue name to build its dead-letter queue.
	DLQSuffix = ".dlq"

	// DefaultMaxAttempts is how many times a message is retried before it is dead-lettered
	// (RF-11).
	DefaultMaxAttempts = 5

	// DefaultPrefetch is how many unacknowledged messages a consumer holds. It bounds how
	// much work is lost when a consumer dies.
	DefaultPrefetch = 10
)

// Config describes the broker connection and the consumer behavior.
type Config struct {
	// URL is the connection string, such as amqp://dizen:dizen@localhost:5672/. Required.
	URL string `env:"AMQP_URL" validate:"required"`

	// Exchange is the topic exchange events are published to.
	Exchange string `env:"AMQP_EXCHANGE" envDefault:"dizen.events"`

	// Prefetch is how many unacknowledged messages a consumer holds at once.
	Prefetch int `env:"AMQP_PREFETCH" envDefault:"10" validate:"min=1"`

	// MaxAttempts is how many times a message is retried before the dead-letter queue.
	MaxAttempts int32 `env:"AMQP_MAX_ATTEMPTS" envDefault:"5" validate:"min=1"`

	// InitialBackoff is the wait before the second delivery attempt.
	InitialBackoff time.Duration `env:"AMQP_INITIAL_BACKOFF" envDefault:"1s"`

	// MaxBackoff caps the exponential growth of the retry delay.
	MaxBackoff time.Duration `env:"AMQP_MAX_BACKOFF" envDefault:"1m"`

	// PublishTimeout bounds waiting for a publisher confirm.
	PublishTimeout time.Duration `env:"AMQP_PUBLISH_TIMEOUT" envDefault:"5s"`

	// ReconnectDelay is the wait between reconnection attempts.
	ReconnectDelay time.Duration `env:"AMQP_RECONNECT_DELAY" envDefault:"5s"`
}

// withDefaults fills the zero values.
func (c Config) withDefaults() Config {
	if c.Exchange == "" {
		c.Exchange = DefaultExchange
	}

	if c.Prefetch <= 0 {
		c.Prefetch = DefaultPrefetch
	}

	if c.MaxAttempts <= 0 {
		c.MaxAttempts = DefaultMaxAttempts
	}

	if c.InitialBackoff <= 0 {
		c.InitialBackoff = time.Second
	}

	if c.MaxBackoff <= 0 {
		c.MaxBackoff = time.Minute
	}

	if c.PublishTimeout <= 0 {
		c.PublishTimeout = 5 * time.Second
	}

	if c.ReconnectDelay <= 0 {
		c.ReconnectDelay = 5 * time.Second
	}

	return c
}

// Routing keys of 01 section 4.3. They are constants because a publisher and a consumer
// that disagree on a string fail silently: the message is published and nobody is bound.
const (
	RoutingUserRegistered    = "user.registered"
	RoutingBookingConfirmed  = "booking.confirmed"
	RoutingBookingCancelled  = "booking.cancelled"
	RoutingSubscriptionRenew = "subscription.renewed"
	RoutingTourPublished     = "tour.published"
	RoutingTourRunCompleted  = "tour_run.completed"
)
