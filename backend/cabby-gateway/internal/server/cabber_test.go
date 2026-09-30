package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/cabby-gateway/internal/authclient"
	"github.com/Keane81/Cabby/backend/cabby-gateway/internal/requestid"
	"github.com/rs/zerolog"
)

const (
	cabberName     = "Иван"
	cabberEmail    = "ivan@example.com"
	cabberPassword = "1234"
	// cabberAccess stands for a live credential, which is why no sink may repeat it (FR-004).
	cabberAccess = "an-opaque-access-of-a-cabber"
)

// stubOperations is the auth service behind the Operations port. A test sets the one answer its
// handler has to report and reads back what reached the port.
type stubOperations struct {
	cabber  authclient.Cabber
	session authclient.Session
	err     error

	calls     int
	sawName   string
	sawEmail  string
	sawPlain  string
	sawAccess string

	// sawRequestID is what the operation reads out of the context of the call: the identifier the
	// dispatcher minted for the request it serves.
	sawRequestID string
}

func (s *stubOperations) RegisterCabber(ctx context.Context, name, email, password string) (authclient.Cabber, error) {
	s.calls++
	s.sawName, s.sawEmail, s.sawPlain = name, email, password
	s.sawRequestID = requestid.From(ctx)
	return s.cabber, s.err
}

func (s *stubOperations) CreateCabberSession(ctx context.Context, email, password string) (authclient.Session, error) {
	s.calls++
	s.sawEmail, s.sawPlain = email, password
	s.sawRequestID = requestid.From(ctx)
	return s.session, s.err
}

func (s *stubOperations) DeleteCabberSession(ctx context.Context, accessToken string) error {
	s.calls++
	s.sawAccess = accessToken
	s.sawRequestID = requestid.From(ctx)
	return s.err
}

func TestRegisterCabberAnswersTheAccountItCreated(t *testing.T) {
	operations := &stubOperations{cabber: authclient.Cabber{ID: "cabber-1", Email: cabberEmail}}
	response := cabberRequest(t, operations, http.MethodPost, pathCabbers,
		`{"name":"`+cabberName+`","email":"Ivan@Example.com","password":"`+cabberPassword+`"}`)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", response.Code, response.Body)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("content type = %q, want application/json", got)
	}
	var answer cabberAnswer
	if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil {
		t.Fatalf("answer body: %v: %s", err, response.Body)
	}
	if answer != (cabberAnswer{CabberID: "cabber-1", Email: cabberEmail}) {
		t.Errorf("answer = %+v", answer)
	}
	// The gateway passes the data on and does not decide what a valid account is.
	if operations.sawName != cabberName || operations.sawEmail != "Ivan@Example.com" || operations.sawPlain != cabberPassword {
		t.Errorf("the service saw %q %q %q", operations.sawName, operations.sawEmail, operations.sawPlain)
	}
}

func TestCreateCabberSessionAnswersTheAccessAndItsLimit(t *testing.T) {
	// The service answers in a zone of its own; the contract promises date-time, so the moment
	// travels in UTC.
	east := time.FixedZone("UTC+3", 3*60*60)
	operations := &stubOperations{session: authclient.Session{
		AccessToken: "opaque-access",
		ExpiresAt:   time.Date(2026, 9, 28, 15, 0, 0, 0, east),
	}}
	response := cabberRequest(t, operations, http.MethodPost, pathCabberSession,
		`{"email":"`+cabberEmail+`","password":"`+cabberPassword+`"}`)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", response.Code, response.Body)
	}
	want := `{"access_token":"opaque-access","expires_at":"2026-09-28T12:00:00Z"}`
	if got := strings.TrimSpace(response.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
	if operations.sawEmail != cabberEmail || operations.sawPlain != cabberPassword {
		t.Errorf("the service saw %q and %q", operations.sawEmail, operations.sawPlain)
	}
}

// TestCabberFailuresFollowTheTable is the mapping of data-model §6 as an external client reads it.
func TestCabberFailuresFollowTheTable(t *testing.T) {
	const body = `{"email":"` + cabberEmail + `","password":"` + cabberPassword + `"}`

	for _, tc := range []struct {
		desc      string
		err       error
		wantHTTP  int
		wantCode  ErrorCode
		wantField string
	}{
		{
			desc: "a field is invalid", err: authclient.Invalid{Field: authclient.FieldPassword, Reason: "too_short"},
			wantHTTP: http.StatusBadRequest, wantCode: CodeInvalidRequest, wantField: "password",
		},
		{
			desc: "a rejection names no field", err: authclient.Invalid{},
			wantHTTP: http.StatusBadRequest, wantCode: CodeInvalidRequest,
		},
		{
			desc: "the email is taken", err: authclient.ErrEmailTaken,
			wantHTTP: http.StatusConflict, wantCode: CodeEmailTaken, wantField: "email",
		},
		{
			desc: "the credentials are not valid", err: authclient.ErrUnauthorized,
			wantHTTP: http.StatusUnauthorized, wantCode: CodeUnauthorized,
		},
		{
			desc: "the service is unreachable", err: authclient.ErrUnavailable,
			wantHTTP: http.StatusServiceUnavailable, wantCode: CodeServiceUnavailable,
		},
		{
			desc: "the service failed", err: authclient.ErrInternal,
			wantHTTP: http.StatusInternalServerError, wantCode: CodeInternalError,
		},
	} {
		operations := &stubOperations{err: tc.err}
		response := cabberRequest(t, operations, http.MethodPost, pathCabberSession, body)

		if response.Code != tc.wantHTTP {
			t.Errorf("%s: status = %d, want %d", tc.desc, response.Code, tc.wantHTTP)
		}
		detail := decodeError(t, response.Body.Bytes())
		if detail.Code != tc.wantCode {
			t.Errorf("%s: code = %q, want %q", tc.desc, detail.Code, tc.wantCode)
		}
		if detail.Field != tc.wantField {
			t.Errorf("%s: field = %q, want %q", tc.desc, detail.Field, tc.wantField)
		}
		if detail.Message == "" {
			t.Errorf("%s: message is empty", tc.desc)
		}
	}
}

// TestCabberBodyIsClosedBeforeTheService keeps the contract of additionalProperties: false and the
// size limit in front of the dependency: nothing of a body the gateway cannot name is ever sent on.
func TestCabberBodyIsClosedBeforeTheService(t *testing.T) {
	for _, tc := range []struct {
		desc string
		body string
	}{
		{"empty", ""},
		{"not an object", `"just a string"`},
		{"broken json", `{"email":"`},
		{"an unknown property", `{"email":"` + cabberEmail + `","password":"` + cabberPassword + `","role":"admin"}`},
		{"a property in the wrong type", `{"email":42,"password":"` + cabberPassword + `"}`},
		{"two objects in one body", `{"email":"a@b","password":"1234"}{"email":"c@d","password":"5678"}`},
		{"a body over the limit", `{"email":"` + cabberEmail + `","password":"` + strings.Repeat("1", maxCabberBody) + `"}`},
	} {
		operations := &stubOperations{}
		response := cabberRequest(t, operations, http.MethodPost, pathCabberSession, tc.body)

		if response.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", tc.desc, response.Code)
		}
		if detail := decodeError(t, response.Body.Bytes()); detail.Code != CodeInvalidRequest {
			t.Errorf("%s: code = %q, want %q", tc.desc, detail.Code, CodeInvalidRequest)
		}
		if operations.calls != 0 {
			t.Errorf("%s: the service was called %d times, want none", tc.desc, operations.calls)
		}
	}
}

// TestCabberFailureNeverRepeatsRequestData is SC-002 on the paths where a client types the data:
// the answer of a rejection holds field names and fixed texts only.
func TestCabberFailureNeverRepeatsRequestData(t *testing.T) {
	operations := &stubOperations{err: authclient.ErrEmailTaken}
	response := cabberRequest(t, operations, http.MethodPost, pathCabbers,
		`{"name":"Пётр Первый","email":"petr@example.com","password":"0987fedcba"}`)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", response.Code)
	}
	for _, sent := range []string{"Пётр Первый", "petr@example.com", "0987fedcba"} {
		if strings.Contains(response.Body.String(), sent) {
			t.Errorf("the answer repeats %q: %s", sent, response.Body)
		}
	}
}

// TestCabberRequestsAreCounted guards R-11: exactly one outcome per request, on the operation that
// was asked for, and the health-check counters stay untouched.
func TestCabberRequestsAreCounted(t *testing.T) {
	metrics := NewMetrics()
	operations := &stubOperations{session: authclient.Session{AccessToken: "a", ExpiresAt: time.Now()}}
	handler := NewRouter(func() bool { return true }, zerolog.Nop(), metrics, operations)

	for range 2 {
		serveWith(handler, http.MethodPost, pathCabberSession, `{"email":"a@b","password":"1234"}`)
	}
	operations.err = authclient.ErrUnauthorized
	serveWith(handler, http.MethodPost, pathCabberSession, `{"email":"a@b","password":"1234"}`)
	// A body that never reached the service is still a request to the operation.
	serveWith(handler, http.MethodPost, pathCabberSession, `{"unexpected":true}`)
	operations.err = nil
	serveWith(handler, http.MethodPost, pathCabbers, `{"name":"Иван","email":"a@b","password":"1234"}`)
	// A header the gateway cannot read is a request to session deletion, and a counted one.
	deleteSessionRequest(t, handler, "")
	serveDelete := deleteSessionRequest(t, handler, "Bearer opaque-access")
	if serveDelete.Code != http.StatusNoContent {
		t.Fatalf("session deletion = %d, want 204", serveDelete.Code)
	}

	exported := scrape(t, metrics)
	for _, expected := range []string{
		`cabby_gateway_cabber_requests_total{operation="create_session",outcome="success"} 2`,
		`cabby_gateway_cabber_requests_total{operation="create_session",outcome="unauthorized"} 1`,
		`cabby_gateway_cabber_requests_total{operation="create_session",outcome="rejected"} 1`,
		`cabby_gateway_cabber_requests_total{operation="register",outcome="success"} 1`,
		`cabby_gateway_cabber_requests_total{operation="delete_session",outcome="success"} 1`,
		`cabby_gateway_cabber_requests_total{operation="delete_session",outcome="unauthorized"} 1`,
		`cabby_gateway_cabber_dependency_duration_seconds_count{operation="create_session"} 3`,
		`cabby_gateway_cabber_dependency_duration_seconds_count{operation="register"} 1`,
		// Only the session deletion that reached the service timed a dependency.
		`cabby_gateway_cabber_dependency_duration_seconds_count{operation="delete_session"} 1`,
	} {
		if !strings.Contains(exported, expected) {
			t.Errorf("missing %q in\n%s", expected, exported)
		}
	}
	assertMetric(t, metrics, "success", 0)
	assertMetric(t, metrics, "failure", 0)
}

// TestDeleteSessionAnswersNoContentForTheAccessItClosed is FR-019 on the public port: the exit of a live
// access succeeds with 204 and no body, and the credential it carries is what reaches the service.
func TestDeleteSessionAnswersNoContentForTheAccessItClosed(t *testing.T) {
	operations := &stubOperations{}
	response := deleteSessionRequest(t, routerWith(operations, io.Discard), "Bearer "+cabberAccess)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", response.Code, response.Body)
	}
	if response.Body.Len() != 0 {
		t.Errorf("204 carries a body: %q", response.Body.String())
	}
	if operations.calls != 1 || operations.sawAccess != cabberAccess {
		t.Errorf("the service saw %d calls with %q, want one with %q", operations.calls, operations.sawAccess, cabberAccess)
	}
}

// TestDeleteSessionFailuresFollowTheTable is the exit read as data-model §6: a dead access is the single
// 401 of the contract, an unreachable service stays 503, and no failure repeats the credential.
func TestDeleteSessionFailuresFollowTheTable(t *testing.T) {
	for _, tc := range []struct {
		desc     string
		err      error
		wantHTTP int
		wantCode ErrorCode
	}{
		{"the access is not valid", authclient.ErrUnauthorized, http.StatusUnauthorized, CodeUnauthorized},
		{"the access of another cabber", authclient.ErrUnauthorized, http.StatusUnauthorized, CodeUnauthorized},
		{"the service is unreachable", authclient.ErrUnavailable, http.StatusServiceUnavailable, CodeServiceUnavailable},
		{"the service failed", authclient.ErrInternal, http.StatusInternalServerError, CodeInternalError},
	} {
		operations := &stubOperations{err: tc.err}
		response := deleteSessionRequest(t, routerWith(operations, io.Discard), "Bearer "+cabberAccess)

		if response.Code != tc.wantHTTP {
			t.Errorf("%s: status = %d, want %d", tc.desc, response.Code, tc.wantHTTP)
		}
		if detail := decodeError(t, response.Body.Bytes()); detail.Code != tc.wantCode {
			t.Errorf("%s: code = %q, want %q", tc.desc, detail.Code, tc.wantCode)
		}
	}
}

// TestDeleteSessionKeepsTheAccessOutOfEverySink is SC-002 and FR-004 on the one path where a client sends
// a live credential: the answer, the log of the request and the exported metrics never carry it, nor
// the digest of it.
func TestDeleteSessionKeepsTheAccessOutOfEverySink(t *testing.T) {
	var logged bytes.Buffer
	metrics := NewMetrics()
	operations := &stubOperations{err: authclient.ErrUnauthorized}
	handler := NewRouter(func() bool { return true }, zerolog.New(&logged), metrics, operations)

	response := deleteSessionRequest(t, handler, "Bearer "+cabberAccess)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
	digest := sha256.Sum256([]byte(cabberAccess))
	secrets := []string{
		cabberAccess,
		fmt.Sprintf("%x", digest),
		base64.RawURLEncoding.EncodeToString(digest[:]),
	}
	for _, sink := range []struct {
		name, text string
	}{
		{"the answer", response.Body.String()},
		{"the log", logged.String()},
		{"the metrics", scrape(t, metrics)},
	} {
		for _, secret := range secrets {
			if strings.Contains(sink.text, secret) {
				t.Errorf("%s repeats the access: %s", sink.name, sink.text)
			}
		}
	}
}

// deleteSessionRequest sends one DELETE to the session path of a router, with the header the test chose.
func deleteSessionRequest(t *testing.T, handler http.Handler, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, pathCabberSession, nil)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	handler.ServeHTTP(response, request)
	return response
}

// routerWith is a router for a test that chooses the port and, for the leak guards, the sink of the
// log.
func routerWith(operations authclient.Operations, logOut io.Writer) http.Handler {
	return NewRouter(func() bool { return true }, zerolog.New(logOut), NewMetrics(), operations)
}

// cabberRequest sends one request to a router built over the given port and records the answer.
func cabberRequest(t *testing.T, operations authclient.Operations, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	handler := NewRouter(func() bool { return true }, zerolog.Nop(), NewMetrics(), operations)
	return serveWith(handler, method, path, body)
}

func serveWith(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, request)
	return response
}

func scrape(t *testing.T, metrics *Metrics) string {
	t.Helper()
	exported := serveWith(metrics.Handler(), http.MethodGet, "/metrics", "")
	if exported.Code != http.StatusOK {
		t.Fatalf("GET /metrics = %d", exported.Code)
	}
	return exported.Body.String()
}
