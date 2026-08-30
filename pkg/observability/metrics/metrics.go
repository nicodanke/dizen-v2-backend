// Package metrics owns the Prometheus registry and the gRPC instrumentation exposed on
// /metrics (RF-6, 01 section 7).
//
// This is the one place where package-level state is allowed by convention, and only
// because the Prometheus client model requires collectors to be registered once per
// process. Everything else is injected explicitly.
package metrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"google.golang.org/grpc/codes"
)

// Namespace prefixes every metric this repository defines.
const Namespace = "dizen"

// Subsystem and label names, kept as constants because dashboards and alerts are written
// against them: renaming one silently breaks every query.
const (
	subsystemGRPC = "grpc"
	labelMethod   = "method"
	labelCode     = "code"
)

// Registry holds the collectors of one service.
type Registry struct {
	prom *prometheus.Registry

	grpcRequests *prometheus.CounterVec
	grpcDuration *prometheus.HistogramVec
	grpcInFlight *prometheus.GaugeVec
}

// NewRegistry builds a registry with the Go runtime and process collectors plus the gRPC
// instrumentation of 01 section 7: a counter and a latency histogram per method.
func NewRegistry(serviceName string) *Registry {
	prom := prometheus.NewRegistry()

	labels := prometheus.Labels{"service": serviceName}

	r := &Registry{
		prom: prom,

		grpcRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   Namespace,
			Subsystem:   subsystemGRPC,
			Name:        "requests_total",
			Help:        "Total gRPC requests handled, by method and status code.",
			ConstLabels: labels,
		}, []string{labelMethod, labelCode}),

		grpcDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace:   Namespace,
			Subsystem:   subsystemGRPC,
			Name:        "request_duration_seconds",
			Help:        "gRPC request latency, by method and status code.",
			ConstLabels: labels,
			// Buckets tuned for an API that answers in milliseconds: the interesting
			// resolution is below 100ms, and anything past 5s is already an incident.
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{labelMethod, labelCode}),

		grpcInFlight: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace:   Namespace,
			Subsystem:   subsystemGRPC,
			Name:        "requests_in_flight",
			Help:        "gRPC requests currently being handled, by method.",
			ConstLabels: labels,
		}, []string{labelMethod}),
	}

	prom.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		r.grpcRequests,
		r.grpcDuration,
		r.grpcInFlight,
	)

	return r
}

// Prometheus exposes the underlying registry, so the HTTP server can serve it and services
// can register their own business metrics.
func (r *Registry) Prometheus() *prometheus.Registry {
	return r.prom
}

// MustRegister adds service-specific collectors, such as tour_runs_started_total.
func (r *Registry) MustRegister(collectors ...prometheus.Collector) {
	r.prom.MustRegister(collectors...)
}

// RequestStarted marks a request as in flight and returns the function that closes it out.
// Returning a closure keeps the start timestamp off the caller and makes the pairing hard
// to get wrong: the interceptor just defers it.
func (r *Registry) RequestStarted(method string) func(code codes.Code) {
	start := time.Now()
	r.grpcInFlight.WithLabelValues(method).Inc()

	return func(code codes.Code) {
		r.grpcInFlight.WithLabelValues(method).Dec()

		label := codeLabel(code)
		r.grpcRequests.WithLabelValues(method, label).Inc()
		r.grpcDuration.WithLabelValues(method, label).Observe(time.Since(start).Seconds())
	}
}

// codeLabel renders a gRPC code as a stable label value.
func codeLabel(code codes.Code) string {
	name := code.String()

	// An unknown code stringifies as `Code(17)`, which would create an unbounded label
	// space; the numeric form is bounded and still diagnosable.
	if len(name) > 5 && name[:5] == "Code(" {
		return strconv.Itoa(int(code))
	}

	return name
}
