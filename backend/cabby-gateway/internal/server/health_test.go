package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestHealthResponse(t *testing.T) {
	var logs bytes.Buffer
	logger := zerolog.New(&logs).With().Timestamp().Logger()
	handler := NewRouter(func() bool { return true }, logger, nil, nil, nil)

	request := httptest.NewRequest(http.MethodGet, "/healthz", strings.NewReader("ignored-body"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}
	var result map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["status"] != "ok" {
		t.Fatalf("status body = %q", result["status"])
	}
	if logs.Len() != 0 {
		t.Fatalf("successful health check logged unexpectedly: %s", logs.String())
	}
}

func TestHealthUnavailableAndSafeLog(t *testing.T) {
	var logs bytes.Buffer
	logger := zerolog.New(&logs).With().Timestamp().Logger()
	handler := NewRouter(func() bool { return false }, logger, nil, nil, nil)

	request := httptest.NewRequest(http.MethodGet, "/healthz", strings.NewReader("secret-body"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}
	var result map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["status"] != "unavailable" {
		t.Fatalf("status body = %q", result["status"])
	}
	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatalf("invalid JSON log: %v: %s", err, logs.String())
	}
	for _, key := range []string{"time", "level", "operation", "error_class"} {
		if entry[key] == nil {
			t.Fatalf("missing log field %q: %s", key, logs.String())
		}
	}
	if entry["operation"] != "health_check" || entry["error_class"] != "not_ready" {
		t.Fatalf("unexpected diagnostic log: %s", logs.String())
	}
	if strings.Contains(logs.String(), "secret-body") {
		t.Fatalf("request body leaked into log: %s", logs.String())
	}
}

func TestUnsupportedPublicRequests(t *testing.T) {
	logger := zerolog.Nop()
	handler := NewRouter(func() bool { return true }, logger, nil, nil, nil)
	tests := []struct {
		method   string
		path     string
		want     int
		wantCode ErrorCode
	}{
		{http.MethodPost, "/healthz", http.StatusMethodNotAllowed, CodeMethodNotAllowed},
		{http.MethodHead, "/healthz", http.StatusMethodNotAllowed, CodeMethodNotAllowed},
		{http.MethodGet, "/unknown", http.StatusNotFound, CodeUnknownOperation},
		{http.MethodGet, "/metrics", http.StatusNotFound, CodeUnknownOperation},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
			if got := response.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("content type = %q, want application/json", got)
			}
			if detail := decodeError(t, response.Body.Bytes()); detail.Code != test.wantCode {
				t.Fatalf("error code = %q, want %q", detail.Code, test.wantCode)
			}
		})
	}
}

func TestHealthStatusSchemaConformance(t *testing.T) {
	cases := []struct {
		name       string
		ready      bool
		wantStatus int
		wantValue  string
	}{
		{"ready", true, http.StatusOK, "ok"},
		{"not ready", false, http.StatusServiceUnavailable, "unavailable"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ready := test.ready
			handler := NewRouter(func() bool { return ready }, zerolog.Nop(), nil, nil, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
			var result map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatalf("body is not a JSON object: %v", err)
			}
			if len(result) != 1 {
				t.Fatalf("HealthStatus must have exactly one field, got %d: %s", len(result), response.Body.String())
			}
			var value string
			if err := json.Unmarshal(result["status"], &value); err != nil {
				t.Fatalf("status is not a string: %v", err)
			}
			if value != test.wantValue {
				t.Fatalf("status = %q, want %q", value, test.wantValue)
			}
		})
	}
}
