package server

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/Keane81/Cabby/backend/cabby-gateway/api"
	"github.com/rs/zerolog"
)

// TestCabberPathsKeepTheirMethods is SC-008 from the other side: adding the cabber operations must
// not turn a method they do not serve into anything other than 405 with the methods that are
// served here and now.
func TestCabberPathsKeepTheirMethods(t *testing.T) {
	operations := &stubOperations{}
	handler := NewRouter(func() bool { return true }, zerolog.Nop(), NewMetrics(), operations, nil)

	// The access of a cabber is opened by POST and closed by DELETE; nothing else is served on the
	// path, and Allow says so.
	sessionMethods := http.MethodDelete + ", " + http.MethodPost // the mux lists methods alphabetically
	for _, tc := range []struct {
		method string
		path   string
		allow  string
	}{
		{http.MethodGet, pathCabbers, http.MethodPost},
		{http.MethodHead, pathCabbers, http.MethodPost},
		{http.MethodDelete, pathCabbers, http.MethodPost},
		{http.MethodPut, pathCabberSession, sessionMethods},
		{http.MethodGet, pathCabberSession, sessionMethods},
		{http.MethodHead, pathCabberSession, sessionMethods},
	} {
		response := serveWith(handler, tc.method, tc.path, "")

		if response.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d, want 405", tc.method, tc.path, response.Code)
			continue
		}
		if got := response.Header().Get("Allow"); got != tc.allow {
			t.Errorf("%s %s: Allow = %q, want %q", tc.method, tc.path, got, tc.allow)
		}
		if detail := decodeError(t, response.Body.Bytes()); detail.Code != CodeMethodNotAllowed {
			t.Errorf("%s %s: code = %q, want %q", tc.method, tc.path, detail.Code, CodeMethodNotAllowed)
		}
	}
	// A rejected method never reaches the auth service.
	if operations.calls != 0 {
		t.Errorf("the service was called %d times, want none", operations.calls)
	}
}

// TestOperationsLeaveThePublishedPathsAlone re-checks spec 001 and 002 through the same router that
// now serves the cabber operations.
func TestOperationsLeaveThePublishedPathsAlone(t *testing.T) {
	handler := NewRouter(func() bool { return true }, zerolog.Nop(), NewMetrics(), &stubOperations{}, nil)

	health := serveWith(handler, http.MethodGet, pathHealth, "")
	if health.Code != http.StatusOK || !bytes.Contains(health.Body.Bytes(), []byte(`"status":"ok"`)) {
		t.Errorf("GET /healthz = %d %s, want 200 status=ok", health.Code, health.Body)
	}
	contract := serveWith(handler, http.MethodGet, pathContract, "")
	if contract.Code != http.StatusOK || !bytes.Equal(contract.Body.Bytes(), api.OpenAPIDocument) {
		t.Error("GET /openapi.yaml no longer serves the embedded contract byte for byte")
	}
	unknown := serveWith(handler, http.MethodPost, pathCabbers+"/password", `{"email":"a@b"}`)
	if unknown.Code != http.StatusNotFound {
		t.Errorf("POST /cabbers/password = %d, want 404", unknown.Code)
	}
}
