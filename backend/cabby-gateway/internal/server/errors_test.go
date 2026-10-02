package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func decodeError(t *testing.T, body []byte) errorDetail {
	t.Helper()
	var envelope errorEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("error body is not the unified envelope: %v: %s", err, body)
	}
	return envelope.Error
}

func TestUnifiedErrorEnvelope(t *testing.T) {
	handler := NewRouter(func() bool { return true }, zerolog.Nop(), NewMetrics(), nil, nil)

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantCode   ErrorCode
		wantAllow  string
	}{
		{"unknown path", http.MethodGet, "/unknown", http.StatusNotFound, CodeUnknownOperation, ""},
		{"metrics not external", http.MethodGet, "/metrics", http.StatusNotFound, CodeUnknownOperation, ""},
		{"post healthz", http.MethodPost, "/healthz", http.StatusMethodNotAllowed, CodeMethodNotAllowed, http.MethodGet},
		{"head healthz", http.MethodHead, "/healthz", http.StatusMethodNotAllowed, CodeMethodNotAllowed, http.MethodGet},
		{"post contract", http.MethodPost, "/openapi.yaml", http.StatusMethodNotAllowed, CodeMethodNotAllowed, http.MethodGet},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if got := response.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("content type = %q, want application/json", got)
			}
			if test.wantAllow != "" {
				if got := response.Header().Get("Allow"); got != test.wantAllow {
					t.Fatalf("Allow = %q, want %q", got, test.wantAllow)
				}
			}
			detail := decodeError(t, response.Body.Bytes())
			if detail.Code != test.wantCode {
				t.Fatalf("code = %q, want %q", detail.Code, test.wantCode)
			}
			if detail.Message == "" {
				t.Fatal("message is empty")
			}
		})
	}
}

func TestErrorDoesNotLeakRequestData(t *testing.T) {
	handler := NewRouter(func() bool { return true }, zerolog.Nop(), NewMetrics(), nil, nil)

	const secret = "super-secret-token"
	request := httptest.NewRequest(http.MethodPost, "/healthz", strings.NewReader(secret))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	body := response.Body.String()
	if strings.Contains(body, secret) {
		t.Fatalf("request body leaked into error response: %s", body)
	}
	if strings.Contains(body, "/healthz") {
		t.Fatalf("request path echoed into error response: %s", body)
	}
}
