// Package metricshttp serves a Prometheus registry on the private metrics listener of a service.
package metricshttp

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handler serves /metrics from registry and nothing else: any other path is a 404 and any method but
// GET is a 405, so the listener exposes no second surface.
func Handler(registry *prometheus.Registry) http.Handler {
	exporter := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		exporter.ServeHTTP(w, r)
	})
}
