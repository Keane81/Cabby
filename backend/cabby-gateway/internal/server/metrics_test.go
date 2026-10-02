package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/cabby-gateway/internal/authclient"
	"github.com/rs/zerolog"
)

func TestHealthMetricsTrackEachOutcomeOnce(t *testing.T) {
	metrics := NewMetrics()
	assertMetric(t, metrics, "success", 0)
	assertMetric(t, metrics, "failure", 0)

	var ready atomic.Bool
	ready.Store(true)
	handler := NewRouter(ready.Load, zerolog.Nop(), metrics, nil, nil)
	request := func(method, path string) {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, path, nil))
	}

	request(http.MethodGet, "/healthz")
	request(http.MethodGet, "/healthz")
	ready.Store(false)
	request(http.MethodGet, "/healthz")
	request(http.MethodPost, "/healthz")
	request(http.MethodHead, "/healthz")
	request(http.MethodGet, "/unknown")
	assertMetric(t, metrics, "success", 2)
	assertMetric(t, metrics, "failure", 1)
}

func TestTechnicalMetricsHandler(t *testing.T) {
	metrics := NewMetrics()
	handler := metrics.Handler()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /metrics = %d", response.Code)
	}
	if !strings.Contains(response.Header().Get("Content-Type"), "text/plain") &&
		!strings.Contains(response.Header().Get("Content-Type"), "application/openmetrics-text") {
		t.Fatalf("unexpected content type: %q", response.Header().Get("Content-Type"))
	}
	for _, request := range []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodPost, "/metrics", http.StatusMethodNotAllowed},
		{http.MethodGet, "/other", http.StatusNotFound},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(request.method, request.path, nil))
		if response.Code != request.want {
			t.Fatalf("%s %s = %d, want %d", request.method, request.path, response.Code, request.want)
		}
	}
	assertMetric(t, metrics, "success", 0)
	assertMetric(t, metrics, "failure", 0)
}

func assertMetric(t *testing.T, metrics *Metrics, outcome string, want int) {
	t.Helper()
	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("metrics status = %d", response.Code)
	}
	expected := "cabby_gateway_health_checks_total{outcome=\"" + outcome + "\"} " + strconv.Itoa(want)
	if !strings.Contains(response.Body.String(), expected) {
		t.Fatalf("missing metric %q in %s", expected, response.Body.String())
	}
}

// The values a cabber request carries. The published contract answers two of them back — the
// address of the account a registration creates, the access of the session it opens — so those two
// are checked where a client has no claim on them: the refusals, the log and the metrics.
const (
	leakEmail    = "leak-check@example.com"
	leakPassword = "a-plain-password-no-sink-may-repeat"
	leakAccess   = "an-access-no-sink-may-repeat"
)

// TestCabberSinksCarryNoValueOfTheRequest is SC-002, FR-004 and FR-025 on the public port: the three
// operations run through the real router, and everything the gateway writes down about them names an
// operation and an outcome and nothing of the request they were made of.
func TestCabberSinksCarryNoValueOfTheRequest(t *testing.T) {
	var logged bytes.Buffer
	metrics := NewMetrics()
	operations := &stubOperations{
		cabber:  authclient.Cabber{ID: "cabber-1", Email: leakEmail},
		session: authclient.Session{AccessToken: leakAccess, ExpiresAt: time.Unix(1_800_000_000, 0).UTC()},
	}
	handler := NewRouter(func() bool { return true }, zerolog.New(&logged), metrics, operations, nil)
	registration := `{"name":"` + cabberName + `","email":"` + leakEmail + `","password":"` + leakPassword + `"}`
	createSessionBody := `{"email":"` + leakEmail + `","password":"` + leakPassword + `"}`

	if created := serveWith(handler, http.MethodPost, pathCabbers, registration); created.Code != http.StatusCreated {
		t.Fatalf("POST /cabbers = %d: %s", created.Code, created.Body)
	}
	opened := serveWith(handler, http.MethodPost, pathCabberSession, createSessionBody)
	if opened.Code != http.StatusCreated || !strings.Contains(opened.Body.String(), leakAccess) {
		t.Fatalf("POST /cabber/session = %d %s, want the access it issued", opened.Code, opened.Body)
	}

	// Every way an operation can refuse, including the one answer the contract lets name a field. A
	// body the parser rejects is refused before the service is asked at all.
	var refusals []string
	for _, failure := range []struct {
		method, path, body string
		err                error
	}{
		{http.MethodPost, pathCabbers, `{"name":`, nil},
		{http.MethodPost, pathCabbers, registration, authclient.ErrEmailTaken},
		{http.MethodPost, pathCabbers, registration, authclient.Invalid{Field: authclient.FieldPassword, Reason: "too_short"}},
		{http.MethodPost, pathCabbers, registration, authclient.ErrUnavailable},
		{http.MethodPost, pathCabberSession, createSessionBody, authclient.ErrUnauthorized},
		{http.MethodPost, pathCabberSession, createSessionBody, authclient.ErrUnavailable},
	} {
		operations.err = failure.err
		refusal := serveWith(handler, failure.method, failure.path, failure.body)
		if refusal.Code < http.StatusBadRequest {
			t.Fatalf("%s %s = %d %s, want a refusal", failure.method, failure.path, refusal.Code, refusal.Body)
		}
		refusals = append(refusals, refusal.Body.String())
	}
	operations.err = authclient.ErrUnauthorized
	refusals = append(refusals, deleteSessionRequest(t, handler, "Bearer "+leakAccess).Body.String())
	operations.err = nil
	refusals = append(refusals, deleteSessionRequest(t, handler, "Bearer "+leakAccess).Body.String())

	digest := sha256.Sum256([]byte(leakAccess))
	values := []string{
		leakEmail, leakPassword, leakAccess,
		fmt.Sprintf("%x", digest),
		base64.RawURLEncoding.EncodeToString(digest[:]),
	}
	for _, sink := range []struct{ name, text string }{
		{"the log", logged.String()},
		{"the metrics", scrape(t, metrics)},
	} {
		for _, value := range values {
			if strings.Contains(sink.text, value) {
				t.Errorf("%s repeats a value of the request: %s", sink.name, sink.text)
			}
		}
		// The names of those values belong to the request and, in a refusal, to the field at fault. A
		// line about a request and a metric about a service have no reason to carry one (FR-004).
		for _, field := range []string{"password", "access_token"} {
			if strings.Contains(sink.text, field) {
				t.Errorf("%s names the request field %q: %s", sink.name, field, sink.text)
			}
		}
	}
	for index, text := range refusals {
		for _, value := range values {
			if strings.Contains(text, value) {
				t.Errorf("refusal %d repeats a value of the request: %s", index, text)
			}
		}
	}
}
