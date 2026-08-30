package outbox

import (
	"context"
	"errors"
	"time"

	"github.com/rs/zerolog"

	"github.com/nicodanke/dizen-v2-backend/pkg/logger"
)

// Worker defaults.
const (
	// DefaultPollInterval is how often the table is checked when it was empty. It is short
	// because the interval is the floor on how long an event waits before being published.
	DefaultPollInterval = 1 * time.Second

	// DefaultBatchSize is how many events are claimed per round.
	DefaultBatchSize = 100

	// DefaultInitialBackoff is the wait before retrying a failed publication.
	DefaultInitialBackoff = 5 * time.Second

	// DefaultMaxBackoff caps that growth.
	DefaultMaxBackoff = 5 * time.Minute
)

// WorkerConfig configures the publishing loop.
type WorkerConfig struct {
	// PollInterval is how often an empty table is polled.
	PollInterval time.Duration `env:"OUTBOX_POLL_INTERVAL" envDefault:"1s"`

	// BatchSize is how many events are claimed per round.
	BatchSize int32 `env:"OUTBOX_BATCH_SIZE" envDefault:"100" validate:"min=1"`

	// InitialBackoff is the wait before retrying a failed publication.
	InitialBackoff time.Duration `env:"OUTBOX_INITIAL_BACKOFF" envDefault:"5s"`

	// MaxBackoff caps the retry growth.
	MaxBackoff time.Duration `env:"OUTBOX_MAX_BACKOFF" envDefault:"5m"`
}

// withDefaults fills the zero values.
func (c WorkerConfig) withDefaults() WorkerConfig {
	if c.PollInterval <= 0 {
		c.PollInterval = DefaultPollInterval
	}

	if c.BatchSize <= 0 {
		c.BatchSize = DefaultBatchSize
	}

	if c.InitialBackoff <= 0 {
		c.InitialBackoff = DefaultInitialBackoff
	}

	if c.MaxBackoff <= 0 {
		c.MaxBackoff = DefaultMaxBackoff
	}

	return c
}

// Worker publishes pending events and marks them sent (RF-12).
type Worker struct {
	store     Store
	publisher Publisher
	cfg       WorkerConfig
	log       zerolog.Logger

	// now is injectable so the backoff can be tested without waiting.
	now func() time.Time
}

// NewWorker builds the worker.
func NewWorker(store Store, publisher Publisher, cfg WorkerConfig, log zerolog.Logger) *Worker {
	return &Worker{
		store:     store,
		publisher: publisher,
		cfg:       cfg.withDefaults(),
		log:       log,
		now:       time.Now,
	}
}

// Run polls until the context is canceled.
//
// A round that published something polls again immediately rather than waiting: a backlog
// should drain at the speed of the broker, not at the speed of the poll interval.
func (w *Worker) Run(ctx context.Context) error {
	w.log.Info().
		Dur("poll_interval", w.cfg.PollInterval).
		Int32("batch_size", w.cfg.BatchSize).
		Msg("the outbox worker started")

	for {
		// Checked at the top so a canceled context stops the worker before it claims
		// another batch. Run returns nil on a clean shutdown, so main.go can treat any
		// non-nil result as fatal.
		select {
		case <-ctx.Done():
			w.log.Info().Msg("the outbox worker stopped")

			return nil
		default:
		}

		published, err := w.ProcessBatch(ctx)

		// A round cut short by the shutdown is not a failure worth reporting: the context
		// was canceled out from under it.
		if err != nil && !errors.Is(err, context.Canceled) {
			w.log.Error().Err(err).Msg("the outbox round failed")
		}

		if published > 0 {
			// There may be more waiting; do not sleep on a draining backlog.
			continue
		}

		select {
		case <-ctx.Done():
			w.log.Info().Msg("the outbox worker stopped")

			return nil
		case <-time.After(w.cfg.PollInterval):
		}
	}
}

// ProcessBatch claims a batch, publishes each event and records the outcome. It returns how
// many were published.
//
// It is exported so a test can drive one round deterministically instead of racing the
// loop.
func (w *Worker) ProcessBatch(ctx context.Context) (int, error) {
	events, err := w.store.ClaimPending(ctx, w.cfg.BatchSize)
	if err != nil {
		return 0, err
	}

	var published int

	for _, event := range events {
		if ctx.Err() != nil {
			return published, ctx.Err()
		}

		if w.publishOne(ctx, event) {
			published++
		}
	}

	return published, nil
}

// publishOne publishes a single event, reporting whether it succeeded.
//
// The order is what makes the guarantee hold: the event is marked published only after the
// broker has confirmed it. A crash between the two means the event is published twice,
// which the consumers absorb by being idempotent; the reverse order would lose it.
func (w *Worker) publishOne(ctx context.Context, event Event) bool {
	log := w.log.With().
		Int64("event_id", event.ID).
		Str("routing_key", event.RoutingKey).
		Logger()

	if err := w.publisher.Publish(ctx, event.RoutingKey, event.Payload); err != nil {
		w.reschedule(ctx, event, err, log)

		return false
	}

	if err := w.store.MarkPublished(ctx, event.ID); err != nil {
		// The event went out but could not be marked. It will be published again on the
		// next round; that is the at-least-once side of the trade and it is logged so the
		// duplicate is explainable.
		log.Error().Err(err).Msg("the event was published but could not be marked, it will be republished")

		return true
	}

	log.Debug().Msg("event published")

	return true
}

// reschedule records the failure and pushes the retry forward with exponential backoff.
func (w *Worker) reschedule(ctx context.Context, event Event, cause error, log zerolog.Logger) {
	delay := backoffFor(event.Attempts+1, w.cfg.InitialBackoff, w.cfg.MaxBackoff)
	retryAt := w.now().Add(delay)

	log.Warn().
		Err(cause).
		Int32("attempts", event.Attempts+1).
		Dur("retry_in", delay).
		Msg("publishing the event failed, rescheduling")

	if err := w.store.Reschedule(ctx, event.ID, cause.Error(), retryAt); err != nil {
		log.Error().Err(err).Msg("could not reschedule the event")
	}
}

// backoffFor doubles the delay per attempt, capped at the maximum.
func backoffFor(attempt int32, initial, maximum time.Duration) time.Duration {
	if attempt <= 1 {
		return initial
	}

	backoff := initial

	for range attempt - 1 {
		backoff *= 2

		if backoff >= maximum {
			return maximum
		}
	}

	return backoff
}

// LogBacklog reports the size of the backlog. A backlog that only grows means the worker is
// not keeping up or the broker is down.
func (w *Worker) LogBacklog(ctx context.Context) {
	pending, err := w.store.CountPending(ctx)
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).Msg("could not read the outbox backlog")

		return
	}

	logger.Ctx(ctx).Info().Int64("pending", pending).Msg("outbox backlog")
}
