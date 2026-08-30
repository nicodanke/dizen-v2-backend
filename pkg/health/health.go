// Package health implements the liveness and readiness probes (RF-6).
//
// The distinction matters operationally: /livez says the process is alive, and a failure
// there means the orchestrator should restart it. /readyz says it can serve traffic, and a
// failure means it should be pulled from the load balancer but left running -- a database
// that is briefly down is not a reason to restart every service that talks to it.
package health

import (
	"context"
	"sync"
	"time"
)

// Status is the outcome of a check.
type Status string

const (
	// StatusUp means the dependency answered.
	StatusUp Status = "up"
	// StatusDown means it did not.
	StatusDown Status = "down"
)

// CheckFunc probes one dependency. It must respect the context deadline: a probe that
// hangs turns readiness into a liability instead of a signal.
type CheckFunc func(ctx context.Context) error

// Check is a named dependency probe.
type Check struct {
	// Name identifies the dependency in the response, such as "postgres".
	Name string

	// Probe is the function that tests it.
	Probe CheckFunc

	// Critical marks a dependency the service cannot serve without. A non-critical one
	// that fails is reported but does not make the service unready.
	Critical bool
}

// Result is the outcome of one check, as rendered in the response.
type Result struct {
	Name     string        `json:"name"`
	Status   Status        `json:"status"`
	Critical bool          `json:"critical"`
	Latency  string        `json:"latency"`
	Error    string        `json:"error,omitempty"`
	duration time.Duration `json:"-"`
}

// Report is the whole readiness answer.
type Report struct {
	Status  Status   `json:"status"`
	Service string   `json:"service"`
	Version string   `json:"version"`
	Checks  []Result `json:"checks"`
}

// Ready reports whether every critical check passed.
func (r Report) Ready() bool {
	return r.Status == StatusUp
}

// Registry holds the checks of one service.
//
// Checks are registered by whoever owns the dependency -- pkg/database registers postgres,
// pkg/cache registers redis, pkg/amqp registers amqp -- so this package never imports any
// of them and the set grows without this file changing.
type Registry struct {
	mu      sync.RWMutex
	checks  []Check
	service string
	version string

	// timeout bounds each individual probe.
	timeout time.Duration
}

// DefaultTimeout bounds a single probe. It is short on purpose: readiness is polled every
// few seconds, and a probe slower than this is already a problem worth reporting.
const DefaultTimeout = 2 * time.Second

// NewRegistry builds an empty registry.
func NewRegistry(service, version string) *Registry {
	return &Registry{
		service: service,
		version: version,
		timeout: DefaultTimeout,
	}
}

// Register adds a check. It is safe to call while the server is running.
func (r *Registry) Register(check Check) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.checks = append(r.checks, check)
}

// SetTimeout overrides the per-probe timeout.
func (r *Registry) SetTimeout(timeout time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.timeout = timeout
}

// Check runs every probe and builds the report.
//
// Probes run concurrently: readiness latency should be that of the slowest dependency, not
// the sum of all of them.
func (r *Registry) Check(ctx context.Context) Report {
	r.mu.RLock()
	checks := make([]Check, len(r.checks))
	copy(checks, r.checks)
	timeout := r.timeout
	service, version := r.service, r.version
	r.mu.RUnlock()

	results := make([]Result, len(checks))

	var wg sync.WaitGroup

	for i, check := range checks {
		wg.Go(func() {
			results[i] = run(ctx, check, timeout)
		})
	}

	wg.Wait()

	report := Report{
		Status:  StatusUp,
		Service: service,
		Version: version,
		Checks:  results,
	}

	for _, result := range results {
		if result.Status == StatusDown && result.Critical {
			report.Status = StatusDown

			break
		}
	}

	return report
}

// run executes one probe under its own timeout.
func run(ctx context.Context, check Check, timeout time.Duration) Result {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	err := check.Probe(ctx)
	elapsed := time.Since(start)

	result := Result{
		Name:     check.Name,
		Status:   StatusUp,
		Critical: check.Critical,
		Latency:  elapsed.String(),
		duration: elapsed,
	}

	if err != nil {
		result.Status = StatusDown
		result.Error = err.Error()
	}

	return result
}
