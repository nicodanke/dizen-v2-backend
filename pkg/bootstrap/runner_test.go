package bootstrap_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nicodanke/dizen-v2-backend/pkg/bootstrap"
	"github.com/nicodanke/dizen-v2-backend/pkg/logger"
)

// recorder tracks the order components were started and stopped in.
type recorder struct {
	mu      sync.Mutex
	started []string
	stopped []string
}

func (r *recorder) start(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.started = append(r.started, name)
}

func (r *recorder) stop(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stopped = append(r.stopped, name)
}

func (r *recorder) stopOrder() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.stopped...)
}

// blocking builds a component that runs until its context is canceled.
func blocking(rec *recorder, name string) bootstrap.Component {
	return bootstrap.Component{
		Name: name,
		Start: func(ctx context.Context) error {
			rec.start(name)
			<-ctx.Done()

			return nil
		},
		Stop: func(context.Context) error {
			rec.stop(name)

			return nil
		},
	}
}

// Shutdown in reverse order is what keeps a transport from being drained after the database
// it needs has already closed.
func TestComponentsStopInReverseOrder(t *testing.T) {
	t.Parallel()

	rec := &recorder{}

	runner := bootstrap.NewRunner(logger.Nop(), time.Second)
	runner.Add(blocking(rec, "database"))
	runner.Add(blocking(rec, "cache"))
	runner.Add(blocking(rec, "grpc"))

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}

	want := []string{"grpc", "cache", "database"}
	got := rec.stopOrder()

	if len(got) != len(want) {
		t.Fatalf("stopped %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("stop order = %v, want %v", got, want)

			break
		}
	}
}

// A component that dies has to bring the service down, not leave it running half broken.
func TestAFailingComponentBringsTheServiceDown(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	wantErr := errors.New("the port is already in use")

	runner := bootstrap.NewRunner(logger.Nop(), time.Second)
	runner.Add(blocking(rec, "database"))
	runner.Add(bootstrap.Component{
		Name:  "grpc",
		Start: func(context.Context) error { return wantErr },
		Stop:  func(context.Context) error { rec.stop("grpc"); return nil },
	})

	err := runner.Run(context.Background())
	if err == nil {
		t.Fatal("Run succeeded even though a component failed")
	}

	if !errors.Is(err, wantErr) {
		t.Errorf("the original error was lost: %v", err)
	}

	// Everything still gets drained.
	if len(rec.stopOrder()) != 2 {
		t.Errorf("not every component was stopped: %v", rec.stopOrder())
	}
}

// A failure to stop one component must not prevent the rest from being drained.
func TestAFailureToStopDoesNotPreventTheOthers(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	stopErr := errors.New("the connection was already closed")

	runner := bootstrap.NewRunner(logger.Nop(), time.Second)
	runner.Add(blocking(rec, "database"))
	runner.Add(bootstrap.Component{
		Name:  "cache",
		Start: func(ctx context.Context) error { <-ctx.Done(); return nil },
		Stop:  func(context.Context) error { return stopErr },
	})
	runner.Add(blocking(rec, "grpc"))

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	err := <-done

	if !errors.Is(err, stopErr) {
		t.Errorf("the stop failure was not reported: %v", err)
	}

	// database and grpc were still stopped despite cache failing between them.
	stopped := rec.stopOrder()
	if len(stopped) != 2 {
		t.Errorf("the other components were not drained: %v", stopped)
	}
}

// A component with no Stop is legitimate, such as a worker that ends when its context is
// canceled.
func TestAComponentWithoutStopIsSupported(t *testing.T) {
	t.Parallel()

	var ran bool

	runner := bootstrap.NewRunner(logger.Nop(), time.Second)
	runner.Add(bootstrap.Component{
		Name: "outbox-worker",
		Start: func(ctx context.Context) error {
			ran = true
			<-ctx.Done()

			return nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	if err := <-done; err != nil {
		t.Errorf("Run: %v", err)
	}

	if !ran {
		t.Error("the component never started")
	}
}

// Stop runs on a context that is not already canceled, or a graceful shutdown would have no
// time to drain anything.
func TestStopGetsAUsableContext(t *testing.T) {
	t.Parallel()

	var stopCtxErr error

	runner := bootstrap.NewRunner(logger.Nop(), time.Second)
	runner.Add(bootstrap.Component{
		Name:  "grpc",
		Start: func(ctx context.Context) error { <-ctx.Done(); return nil },
		Stop: func(ctx context.Context) error {
			stopCtxErr = ctx.Err()

			return nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if stopCtxErr != nil {
		t.Errorf("Stop received an already canceled context: %v", stopCtxErr)
	}
}
