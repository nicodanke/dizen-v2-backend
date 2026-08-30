// Package bootstrap runs the components of a service and shuts them down in order.
//
// It exists so main.go stays pure composition: the wiring of what a service is made of
// belongs in main.go, but the choreography of starting several long-running components,
// waiting for a signal and draining them in the right order is the same in all five and
// does not belong copied five times.
//
// It contains no business logic and knows nothing about any component: a component is just
// a name, a start function and a stop function.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

// DefaultShutdownTimeout bounds the whole shutdown. It is the total budget, not per
// component: a deployment that never finishes is worse than a few cut connections.
const DefaultShutdownTimeout = 30 * time.Second

// Component is one long-running part of a service.
type Component struct {
	// Name identifies it in the logs.
	Name string

	// Start blocks until the component stops. Returning nil means a clean stop; any other
	// error brings the whole service down.
	Start func(ctx context.Context) error

	// Stop drains the component. It is called in reverse registration order, so the
	// transports close before the dependencies they use.
	Stop func(ctx context.Context) error
}

// Runner starts components and shuts them down together.
type Runner struct {
	components []Component
	log        zerolog.Logger
	timeout    time.Duration
}

// NewRunner builds a runner.
func NewRunner(log zerolog.Logger, timeout time.Duration) *Runner {
	if timeout <= 0 {
		timeout = DefaultShutdownTimeout
	}

	return &Runner{log: log, timeout: timeout}
}

// Add registers a component. Order matters: shutdown runs in reverse, so components are
// added from the innermost dependency outwards -- database first, transports last -- and
// the transports therefore stop first.
func (r *Runner) Add(component Component) {
	r.components = append(r.components, component)
}

// Run starts every component and blocks until one fails or a termination signal arrives,
// then shuts everything down.
//
// The two ways out are deliberately symmetric: whether the process was asked to stop or a
// component died, everything registered is drained the same way.
func (r *Runner) Run(ctx context.Context) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	group, groupCtx := errgroup.WithContext(ctx)

	for _, component := range r.components {
		if component.Start == nil {
			continue
		}

		r.log.Debug().Str("component", component.Name).Msg("starting")

		group.Go(func() error {
			if err := component.Start(groupCtx); err != nil {
				return fmt.Errorf("%s: %w", component.Name, err)
			}

			return nil
		})
	}

	// Waiting on the group context rather than on the group itself is what lets the
	// shutdown begin the moment the first component fails, instead of after every other
	// one has noticed.
	<-groupCtx.Done()

	r.log.Info().Msg("shutting down")

	shutdownErr := r.shutdown(ctx)

	// The group is drained after the stops so a component that returns an error while
	// closing is still reported.
	runErr := group.Wait()

	// A context canceled by the signal is the normal exit, not a failure.
	if errors.Is(runErr, context.Canceled) {
		runErr = nil
	}

	return errors.Join(runErr, shutdownErr)
}

// shutdown stops the components in reverse order, under one shared budget.
//
// The cancellation of the incoming context is deliberately dropped: by the time this runs
// that context is already canceled -- it is what triggered the shutdown -- and a Stop
// handed an expired context would drain nothing. Its values are kept so a trace started
// before the signal still covers the shutdown.
func (r *Runner) shutdown(parent context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), r.timeout)
	defer cancel()

	var errs []error

	for _, component := range slices.Backward(r.components) {
		if component.Stop == nil {
			continue
		}

		r.log.Debug().Str("component", component.Name).Msg("stopping")

		if err := component.Stop(ctx); err != nil {
			// A failure to stop one component must not prevent the rest from being
			// drained: the errors are collected and reported together.
			r.log.Error().Err(err).Str("component", component.Name).Msg("failed to stop")

			errs = append(errs, fmt.Errorf("stopping %s: %w", component.Name, err))
		}
	}

	return errors.Join(errs...)
}
