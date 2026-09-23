package server

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"
)

func TestHealthMetricsTrackEachOutcomeOnce(t *testing.T) {
	metrics := NewMetrics()
	assertMetric(t, metrics, "success", 0)
	assertMetric(t, metrics, "failure", 0)

	var ready atomic.Bool
	ready.Store(true)
	handler := NewRouter(ready.Load, zerolog.Nop(), metrics)
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
