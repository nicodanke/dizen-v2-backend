package metrics_test

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/grpc/codes"

	"github.com/nicodanke/dizen-v2-backend/pkg/observability/metrics"
)

// gather collects the families the registry currently exposes, keyed by name.
func gather(t *testing.T, r *metrics.Registry) map[string]*dto.MetricFamily {
	t.Helper()

	families, err := r.Prometheus().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}

	out := make(map[string]*dto.MetricFamily, len(families))
	for _, family := range families {
		out[family.GetName()] = family
	}

	return out
}

// labelsOf renders a metric's labels as a map.
func labelsOf(m *dto.Metric) map[string]string {
	out := map[string]string{}
	for _, pair := range m.GetLabel() {
		out[pair.GetName()] = pair.GetValue()
	}

	return out
}

func TestTheRegistryExposesTheRuntimeCollectors(t *testing.T) {
	t.Parallel()

	families := gather(t, metrics.NewRegistry("identity"))

	if _, ok := families["go_goroutines"]; !ok {
		t.Error("the Go collector is not registered")
	}
}

func TestRequestStartedRecordsCounterAndHistogram(t *testing.T) {
	t.Parallel()

	registry := metrics.NewRegistry("tours")

	finish := registry.RequestStarted("/dizen.tours.v1.CatalogService/GetTour")
	finish(codes.OK)

	families := gather(t, registry)

	counter, ok := families["dizen_grpc_requests_total"]
	if !ok {
		t.Fatal("the request counter is missing")
	}

	if got := counter.GetMetric()[0].GetCounter().GetValue(); got != 1 {
		t.Errorf("counter = %v, want 1", got)
	}

	labels := labelsOf(counter.GetMetric()[0])

	if labels["method"] != "/dizen.tours.v1.CatalogService/GetTour" {
		t.Errorf("the method label = %q", labels["method"])
	}

	if labels["code"] != "OK" {
		t.Errorf("the code label = %q", labels["code"])
	}

	// The service label is constant and is what separates series across services when
	// they all scrape into one Prometheus.
	if labels["service"] != "tours" {
		t.Errorf("the service label = %q", labels["service"])
	}

	histogram, ok := families["dizen_grpc_request_duration_seconds"]
	if !ok {
		t.Fatal("the latency histogram is missing")
	}

	if got := histogram.GetMetric()[0].GetHistogram().GetSampleCount(); got != 1 {
		t.Errorf("histogram samples = %d, want 1", got)
	}
}

// The in-flight gauge has to come back to zero: a leak there makes the dashboard show
// permanently stuck requests.
func TestTheInFlightGaugeReturnsToZero(t *testing.T) {
	t.Parallel()

	registry := metrics.NewRegistry("booking")
	method := "/dizen.booking.v1.BookingService/CreateBooking"

	finish := registry.RequestStarted(method)

	inFlight := gather(t, registry)["dizen_grpc_requests_in_flight"]
	if got := inFlight.GetMetric()[0].GetGauge().GetValue(); got != 1 {
		t.Errorf("in flight during the call = %v, want 1", got)
	}

	finish(codes.Internal)

	inFlight = gather(t, registry)["dizen_grpc_requests_in_flight"]
	if got := inFlight.GetMetric()[0].GetGauge().GetValue(); got != 0 {
		t.Errorf("in flight after the call = %v, want 0", got)
	}
}

// An unknown code stringifies as "Code(17)", which would create an unbounded label space.
func TestAnUnknownCodeUsesItsNumericLabel(t *testing.T) {
	t.Parallel()

	registry := metrics.NewRegistry("admin")

	finish := registry.RequestStarted("/x/Y")
	finish(codes.Code(99))

	counter := gather(t, registry)["dizen_grpc_requests_total"]
	label := labelsOf(counter.GetMetric()[0])["code"]

	if strings.Contains(label, "Code(") {
		t.Errorf("the code label is unbounded: %q", label)
	}

	if label != "99" {
		t.Errorf("the code label = %q, want 99", label)
	}
}

func TestMustRegisterAcceptsBusinessMetrics(t *testing.T) {
	t.Parallel()

	registry := metrics.NewRegistry("tours")

	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: metrics.Namespace,
		Name:      "tour_runs_started_total",
		Help:      "Tour runs started.",
	})

	registry.MustRegister(counter)
	counter.Inc()

	if _, ok := gather(t, registry)["dizen_tour_runs_started_total"]; !ok {
		t.Error("the business metric was not exposed")
	}
}
