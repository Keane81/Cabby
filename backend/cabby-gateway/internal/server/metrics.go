package server

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Operations and outcomes of the cabber counters (R-11). Both label sets are closed: a request can
// only add a value from these lists, so no request data ever reaches a metric name.
const (
	operationRegister      = "register"
	operationCreateSession = "create_session"
	operationDeleteSession = "delete_session"
)

var cabberOperations = []string{operationRegister, operationCreateSession, operationDeleteSession}

const (
	outcomeSuccess      = "success"
	outcomeRejected     = "rejected"
	outcomeUnauthorized = "unauthorized"
	outcomeUnavailable  = "unavailable"
	outcomeFailure      = "failure"
)

var cabberOutcomes = []string{
	outcomeSuccess, outcomeRejected, outcomeUnauthorized, outcomeUnavailable, outcomeFailure,
}

// dependencyBuckets bound the observable delay of one gRPC call: R-09 gives it 2 s, and the last
// bucket is where a call that ran out of deadline lands.
var dependencyBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5}

// Metrics owns the fixed-cardinality counters the public port exports.
type Metrics struct {
	registry         *prometheus.Registry
	checks           *prometheus.CounterVec
	cabberRequests   *prometheus.CounterVec
	cabberDependency *prometheus.HistogramVec
}

// NewMetrics registers the series of the gateway. Every combination of labels is lit at zero up
// front, so a dashboard distinguishes «no requests» from «nothing exported».
func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()
	checks := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "cabby_gateway_health_checks_total",
		Help: "Number of health-check requests processed by cabby-gateway.",
	}, []string{"outcome"})
	checks.WithLabelValues("success")
	checks.WithLabelValues("failure")

	cabberRequests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "cabby_gateway_cabber_requests_total",
		Help: "Requests to a cabber operation of the published contract, by outcome.",
	}, []string{"operation", "outcome"})
	for _, operation := range cabberOperations {
		for _, outcome := range cabberOutcomes {
			cabberRequests.WithLabelValues(operation, outcome)
		}
	}

	cabberDependency := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "cabby_gateway_cabber_dependency_duration_seconds",
		Help:    "Duration of one call to the auth service, by operation.",
		Buckets: dependencyBuckets,
	}, []string{"operation"})
	for _, operation := range cabberOperations {
		cabberDependency.WithLabelValues(operation)
	}

	registry.MustRegister(checks, cabberRequests, cabberDependency)
	return &Metrics{
		registry:         registry,
		checks:           checks,
		cabberRequests:   cabberRequests,
		cabberDependency: cabberDependency,
	}
}

func (m *Metrics) observe(success bool) {
	if success {
		m.checks.WithLabelValues("success").Inc()
		return
	}
	m.checks.WithLabelValues("failure").Inc()
}

// observeCabber counts the answer a cabber operation gave. The status is the one the client read,
// so the counter and the response cannot drift apart.
func (m *Metrics) observeCabber(operation string, status int) {
	m.cabberRequests.WithLabelValues(operation, outcomeOfStatus(status)).Inc()
}

// observeDependency times the call to the auth service alone: R-11 splits the delay of a request
// between the HTTP layer of the gateway, the gRPC call and the database behind it.
func (m *Metrics) observeDependency(operation string, started time.Time) {
	m.cabberDependency.WithLabelValues(operation).Observe(time.Since(started).Seconds())
}

// outcomeOfStatus groups the answers of data-model §6 into the five values of R-11. A status the
// table does not know is a failure of ours, never someone else's rejection.
func outcomeOfStatus(status int) string {
	switch {
	case status == http.StatusUnauthorized:
		return outcomeUnauthorized
	case status == http.StatusServiceUnavailable:
		return outcomeUnavailable
	case status == http.StatusOK, status == http.StatusCreated, status == http.StatusNoContent:
		return outcomeSuccess
	case status == http.StatusBadRequest, status == http.StatusConflict:
		return outcomeRejected
	default:
		return outcomeFailure
	}
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
