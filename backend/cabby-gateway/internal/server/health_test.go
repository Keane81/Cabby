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
	handler := NewPublicHandler(func() bool { return true }, logger)

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
	handler := NewPublicHandler(func() bool { return false }, logger)

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
	handler := NewPublicHandler(func() bool { return true }, logger)
	tests := []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodPost, "/healthz", http.StatusMethodNotAllowed},
		{http.MethodHead, "/healthz", http.StatusMethodNotAllowed},
		{http.MethodGet, "/unknown", http.StatusNotFound},
		{http.MethodGet, "/metrics", http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
		})
	}
}
