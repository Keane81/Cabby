package server

import (
	"net/http"

	"github.com/rs/zerolog"
)

// handleHealth processes GET /healthz. HTTP 200 with status=ok is the only
// positive result; not-ready or an internal failure yields HTTP 503 with
// status=unavailable. Each reached call increments exactly one metrics outcome.
// The request body is never read or logged.
func handleHealth(w http.ResponseWriter, ready func() bool, logger zerolog.Logger, observer *Metrics) {
	w.Header().Set("Content-Type", "application/json")
	if !ready() {
		if observer != nil {
			observer.observe(false)
		}
		logger.Error().
			Str("operation", "health_check").
			Str("error_class", "not_ready").
			Msg("health check unavailable")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("{\"status\":\"unavailable\"}\n"))
		return
	}

	if observer != nil {
		observer.observe(true)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
}
