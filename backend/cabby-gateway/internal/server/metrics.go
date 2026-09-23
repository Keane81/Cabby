package server

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics owns the fixed-cardinality counters for the public health command.
type Metrics struct {
	registry *prometheus.Registry
	checks   *prometheus.CounterVec
}

func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()
	checks := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "cabby_gateway_health_checks_total",
		Help: "Number of health-check requests processed by cabby-gateway.",
	}, []string{"outcome"})
	checks.WithLabelValues("success")
	checks.WithLabelValues("failure")
	registry.MustRegister(checks)
	return &Metrics{registry: registry, checks: checks}
}

func (m *Metrics) observe(success bool) {
	if success {
		m.checks.WithLabelValues("success").Inc()
		return
	}
	m.checks.WithLabelValues("failure").Inc()
}

func (m *Metrics) Handler() http.Handler {
	exporter := promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
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
