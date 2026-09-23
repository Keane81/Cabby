package server

import (
	"net/http"

	"github.com/rs/zerolog"
)

// NewPublicHandler serves the only public command of cabby-gateway.
func NewPublicHandler(ready func() bool, logger zerolog.Logger, metrics ...*Metrics) http.Handler {
	var observer *Metrics
	if len(metrics) > 0 {
		observer = metrics[0]
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

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
	})
}
