package health

import (
	"encoding/json"
	"net/http"
)

// LivenessHandler answers /livez. It only proves the process is alive and able to serve an
// HTTP request; it deliberately checks no dependency, because a restart never fixes a
// database that is down.
func LivenessHandler(service, version string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  string(StatusUp),
			"service": service,
			"version": version,
		})
	}
}

// ReadinessHandler answers /readyz. It runs every registered check and returns 503 with the
// detail of what is failing, so the operator learns which dependency is down straight from
// the probe instead of having to go read logs (RF-6).
func (r *Registry) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		report := r.Check(req.Context())

		code := http.StatusOK
		if !report.Ready() {
			code = http.StatusServiceUnavailable
		}

		writeJSON(w, code, report)
	}
}

// writeJSON renders the body, setting the status before writing so the header is not
// already flushed.
func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	// Probes are polled constantly and must never be served from a cache.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)

	_ = json.NewEncoder(w).Encode(body)
}
