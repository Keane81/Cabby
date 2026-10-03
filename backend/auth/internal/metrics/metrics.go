// Package metrics owns what the auth service exposes to Prometheus: the series of its requests and of
// its repositories, the interceptor that feeds them and the handler that serves them. It contains no
// SQL and no business rule.
package metrics

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Keane81/Cabby/backend/auth/internal/repo"
	"github.com/Keane81/Cabby/backend/platform/grpcobs"
	"github.com/Keane81/Cabby/backend/platform/metricshttp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// Outcome is the fixed label value describing how a call ended. The values carry no request
// data, which is what keeps them safe to expose (R-11, FR-004).
const (
	outcomeSuccess      = "success"
	outcomeInvalid      = "invalid_argument"
	outcomeTaken        = "already_exists"
	outcomeUnauthorized = "unauthorized"
	outcomeUnavailable  = "unavailable"
	outcomeFailure      = "failure"
)

// methods bounds the `method` label to the operations of the contract; anything else is
// reported as unknownMethod so an unexpected call cannot add series.
var methods = []string{"RegisterCabber", "CreateCabberSession", "DeleteCabberSession", "VerifyCabberSession"}

const unknownMethod = "unknown"

// queries are the repository calls counted by cabby_auth_repository_query_total.
const (
	queryCabberCreate       = "cabber_create"
	queryCabberFindByEmail  = "cabber_find_by_email"
	querySessionCreate      = "session_create"
	querySessionGetByDigest = "session_get_by_digest"
	querySessionTouch       = "session_touch"
	querySessionRevoke      = "session_revoke"
	querySessionPurge       = "session_purge"
)

// durationBuckets span the latency budget of the service: the p95 target of a session creation is 250 ms
// (plan.md) and the deadline of a gateway call is 2 s (R-09), so the last bucket is the point
// past which a caller has already given up.
var durationBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5}

// Metrics owns the operational counters of the service on a private registry: like the gateway,
// auth exports nothing but its own series.
type Metrics struct {
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	queries  *prometheus.CounterVec
}

// New registers the series and lights up the zero values of the fixed labels so a
// dashboard reads 0 instead of «no data» before the first request.
func New() *Metrics {
	registry := prometheus.NewRegistry()
	requests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "cabby_auth_requests_total",
		Help: "Number of auth requests processed per method and outcome.",
	}, []string{"method", "outcome"})
	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "cabby_auth_request_duration_seconds",
		Help:    "Duration of auth requests per method.",
		Buckets: durationBuckets,
	}, []string{"method"})
	queries := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "cabby_auth_repository_query_total",
		Help: "Number of repository queries run by auth per query and outcome.",
	}, []string{"query", "outcome"})

	for _, method := range methods {
		requests.WithLabelValues(method, outcomeSuccess)
		duration.WithLabelValues(method)
	}
	for _, query := range []string{
		queryCabberCreate, queryCabberFindByEmail, querySessionCreate,
		querySessionGetByDigest, querySessionTouch, querySessionRevoke, querySessionPurge,
	} {
		queries.WithLabelValues(query, outcomeSuccess)
		queries.WithLabelValues(query, outcomeFailure)
	}

	registry.MustRegister(requests, duration, queries)
	return &Metrics{registry: registry, requests: requests, duration: duration, queries: queries}
}

// Handler serves /metrics for the private :9094 listener.
func (m *Metrics) Handler() http.Handler {
	return metricshttp.Handler(m.registry)
}

// UnaryInterceptor is the observability of every call: the panic guard, the log line and the series
// of this package for the same outcome.
func (m *Metrics) UnaryInterceptor(logger zerolog.Logger) grpc.UnaryServerInterceptor {
	return grpcobs.UnaryInterceptor(grpcobs.Options{
		Service: "auth",
		Logger:  logger,
		Method:  methodOf,
		Observe: m.observeRequest,
	})
}

// observeRequest records one finished call.
func (m *Metrics) observeRequest(method string, code codes.Code, seconds float64) {
	outcome := outcomeOf(code)
	m.requests.WithLabelValues(method, outcome).Inc()
	m.duration.WithLabelValues(method).Observe(seconds)
}

// outcomeOf maps a gRPC code onto the fixed label values of R-11.
func outcomeOf(code codes.Code) string {
	switch code {
	case codes.OK:
		return outcomeSuccess
	case codes.InvalidArgument:
		return outcomeInvalid
	case codes.AlreadyExists:
		return outcomeTaken
	case codes.Unauthenticated:
		return outcomeUnauthorized
	case codes.Unavailable, codes.DeadlineExceeded:
		return outcomeUnavailable
	default:
		return outcomeFailure
	}
}

// methodOf reduces a full gRPC method name to the contract method, keeping the label set closed.
func methodOf(fullMethod string) string {
	name := fullMethod[strings.LastIndexByte(fullMethod, '/')+1:]
	for _, method := range methods {
		if name == method {
			return method
		}
	}
	return unknownMethod
}

// Cabbers counts the queries of the cabber repository. The SQL stays in repo; this wrapper only
// names the call and its outcome.
func (m *Metrics) Cabbers(inner repo.CabberRepository) repo.CabberRepository {
	return observedCabbers{inner: inner, metrics: m}
}

// Sessions counts the queries of the session repository.
func (m *Metrics) Sessions(inner repo.SessionRepository) repo.SessionRepository {
	return observedSessions{inner: inner, metrics: m}
}

type observedCabbers struct {
	inner   repo.CabberRepository
	metrics *Metrics
}

func (o observedCabbers) Create(ctx context.Context, cabber repo.Cabber) (string, error) {
	id, err := o.inner.Create(ctx, cabber)
	o.metrics.queries.WithLabelValues(queryCabberCreate, outcomeOfError(err)).Inc()
	return id, err
}

func (o observedCabbers) FindByEmail(ctx context.Context, email string) (repo.Cabber, error) {
	cabber, err := o.inner.FindByEmail(ctx, email)
	o.metrics.queries.WithLabelValues(queryCabberFindByEmail, outcomeOfError(err)).Inc()
	return cabber, err
}

type observedSessions struct {
	inner   repo.SessionRepository
	metrics *Metrics
}

func (o observedSessions) Create(ctx context.Context, session repo.Session) error {
	err := o.inner.Create(ctx, session)
	o.metrics.queries.WithLabelValues(querySessionCreate, outcomeOfError(err)).Inc()
	return err
}

func (o observedSessions) GetByDigest(ctx context.Context, digest []byte) (repo.Session, bool, error) {
	session, found, err := o.inner.GetByDigest(ctx, digest)
	o.metrics.queries.WithLabelValues(querySessionGetByDigest, outcomeOfError(err)).Inc()
	return session, found, err
}

func (o observedSessions) Touch(ctx context.Context, id string, seenAt time.Time) (bool, error) {
	written, err := o.inner.Touch(ctx, id, seenAt)
	o.metrics.queries.WithLabelValues(querySessionTouch, outcomeOfError(err)).Inc()
	return written, err
}

func (o observedSessions) Revoke(ctx context.Context, digest []byte, revokedAt time.Time) (bool, error) {
	revoked, err := o.inner.Revoke(ctx, digest, revokedAt)
	o.metrics.queries.WithLabelValues(querySessionRevoke, outcomeOfError(err)).Inc()
	return revoked, err
}

func (o observedSessions) PurgeExpired(ctx context.Context, before time.Time) (int64, error) {
	deleted, err := o.inner.PurgeExpired(ctx, before)
	o.metrics.queries.WithLabelValues(querySessionPurge, outcomeOfError(err)).Inc()
	return deleted, err
}

// outcomeOfError counts what storage answered, not what the case decided: a taken address and a
// missing row are correct answers, so only a failed query or a lost connection is a failure.
func outcomeOfError(err error) string {
	if err != nil && !errors.Is(err, repo.ErrCabberNotFound) && !errors.Is(err, repo.ErrEmailTaken) {
		return outcomeFailure
	}
	return outcomeSuccess
}
