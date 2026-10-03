// Package metrics owns what the location service exposes to Prometheus: the series of its requests and
// of its repository, the interceptor that feeds them and the handler that serves them. It contains no
// SQL and no business rule.
package metrics

import (
	"context"
	"net/http"
	"strings"

	"github.com/Keane81/Cabby/backend/location/internal/repo"
	"github.com/Keane81/Cabby/backend/platform/grpcobs"
	"github.com/Keane81/Cabby/backend/platform/metricshttp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// Outcome is the fixed label value describing how a call ended. The values carry no request data
// and in particular no coordinate (spec 004 FR-011, SC-006).
const (
	outcomeSuccess     = "success"
	outcomeInvalid     = "invalid_argument"
	outcomeUnavailable = "unavailable"
	outcomeFailure     = "failure"
)

// durationBuckets span the latency budget: the p95 target of a record is 300 ms (plan.md) and the
// deadline of a gateway call is 2 s, so the last bucket is the point past which a caller has given up.
var durationBuckets = []float64{0.01, 0.025, 0.05, 0.1, 0.3, 1, 2.5}

// Metrics owns the operational series of the service on a private registry.
type Metrics struct {
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	duration prometheus.Histogram
	inserts  *prometheus.CounterVec
}

// New registers the series and lights up the fixed labels so a dashboard reads 0 instead of
// «no data» before the first request.
func New() *Metrics {
	registry := prometheus.NewRegistry()
	requests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "cabby_location_requests_total",
		Help: "Number of location record requests processed per outcome.",
	}, []string{"outcome"})
	duration := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "cabby_location_request_duration_seconds",
		Help:    "Duration of location record requests.",
		Buckets: durationBuckets,
	})
	inserts := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "cabby_location_records_total",
		Help: "Number of record inserts attempted by location per outcome; the growth of the table.",
	}, []string{"outcome"})

	for _, outcome := range []string{outcomeSuccess, outcomeInvalid, outcomeUnavailable, outcomeFailure} {
		requests.WithLabelValues(outcome)
	}
	inserts.WithLabelValues(outcomeSuccess)
	inserts.WithLabelValues(outcomeFailure)

	registry.MustRegister(requests, duration, inserts)
	return &Metrics{registry: registry, requests: requests, duration: duration, inserts: inserts}
}

// Handler serves /metrics for the private :9096 listener.
func (m *Metrics) Handler() http.Handler {
	return metricshttp.Handler(m.registry)
}

// UnaryInterceptor is the observability of every call: the panic guard, the log line and the series
// of this package for the same outcome.
func (m *Metrics) UnaryInterceptor(logger zerolog.Logger) grpc.UnaryServerInterceptor {
	return grpcobs.UnaryInterceptor(grpcobs.Options{
		Service: "location",
		Logger:  logger,
		Method:  methodOf,
		Observe: func(_ string, code codes.Code, seconds float64) { m.observeRequest(code, seconds) },
	})
}

// methodOf reduces a full gRPC method name to the one the contract defines; the log field stays in
// a closed set, so an unexpected call cannot put text of a caller's choosing into a log line.
func methodOf(fullMethod string) string {
	if strings.HasSuffix(fullMethod, "/RecordCabberLocation") {
		return "RecordCabberLocation"
	}
	return "unknown"
}

func (m *Metrics) observeRequest(code codes.Code, seconds float64) {
	m.requests.WithLabelValues(outcomeOf(code)).Inc()
	m.duration.Observe(seconds)
}

// outcomeOf maps a gRPC code onto the fixed label values.
func outcomeOf(code codes.Code) string {
	switch code {
	case codes.OK:
		return outcomeSuccess
	case codes.InvalidArgument:
		return outcomeInvalid
	case codes.Unavailable, codes.DeadlineExceeded:
		return outcomeUnavailable
	default:
		return outcomeFailure
	}
}

// Locations counts the inserts of the repository. The SQL stays in repo; this wrapper only names
// the call and its outcome.
func (m *Metrics) Locations(inner repo.LocationRepository) repo.LocationRepository {
	return observedLocations{inner: inner, metrics: m}
}

type observedLocations struct {
	inner   repo.LocationRepository
	metrics *Metrics
}

func (o observedLocations) Insert(ctx context.Context, location repo.Location) error {
	err := o.inner.Insert(ctx, location)
	outcome := outcomeSuccess
	if err != nil {
		outcome = outcomeFailure
	}
	o.metrics.inserts.WithLabelValues(outcome).Inc()
	return err
}
