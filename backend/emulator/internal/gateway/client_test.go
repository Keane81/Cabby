package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return New(server.URL, 8)
}

// slowHandler outlasts every client deadline of the tests but is bounded itself: the server does
// not notice a vanished client while the handler is not reading the body, so waiting on the
// request context alone would hold httptest.Server.Close forever.
func slowHandler(w http.ResponseWriter, r *http.Request) {
	select {
	case <-r.Context().Done():
	case <-time.After(300 * time.Millisecond):
	}
}

func TestRegisterSendsContractBody(t *testing.T) {
	var got map[string]any
	var method, path string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"cabber_id":"1","email":"a@b.test","future_field":true}`)
	})
	if kind := c.Register(context.Background(), "emu-1", "a@b.test", "secret-pass"); kind != OK {
		t.Fatalf("Register = %s", kind)
	}
	if method != http.MethodPost || path != "/cabbers" {
		t.Errorf("request = %s %s", method, path)
	}
	if len(got) != 3 || got["name"] != "emu-1" || got["email"] != "a@b.test" || got["password"] != "secret-pass" {
		t.Errorf("body = %v, want exactly name, email, password", got)
	}
}

func TestLoginReturnsTokenAndIgnoresExtraFields(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cabber/session" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"access_token":"tok","expires_at":"2030-01-01T00:00:00Z","extra":1}`)
	})
	token, kind := c.Login(context.Background(), "a@b.test", "pw")
	if kind != OK || token != "tok" {
		t.Fatalf("Login = %q, %s", token, kind)
	}
}

func TestLoginWithoutTokenIsUnexpected(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{}`)
	})
	if _, kind := c.Login(context.Background(), "a@b.test", "pw"); kind != Unexpected {
		t.Fatalf("Login = %s, want unexpected", kind)
	}
}

func TestRecordLocationCarriesBearerAndPosition(t *testing.T) {
	var auth string
	var got map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
	})
	if kind := c.RecordLocation(context.Background(), "tok", 55.5, 37.25); kind != OK {
		t.Fatalf("RecordLocation = %s", kind)
	}
	if auth != "Bearer tok" {
		t.Errorf("Authorization = %q", auth)
	}
	if len(got) != 2 || got["latitude"] != 55.5 || got["longitude"] != 37.25 {
		t.Errorf("body = %v, want exactly latitude and longitude", got)
	}
}

func TestLogoutExpectsNoContent(t *testing.T) {
	var method, auth string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, auth = r.Method, r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	})
	if kind := c.Logout(context.Background(), "tok"); kind != OK {
		t.Fatalf("Logout = %s", kind)
	}
	if method != http.MethodDelete || auth != "Bearer tok" {
		t.Errorf("request = %s with %q", method, auth)
	}
}

func TestStatusToKind(t *testing.T) {
	cases := []struct {
		status int
		want   Kind
	}{
		{http.StatusCreated, OK},
		{http.StatusBadRequest, InvalidRequest},
		{http.StatusUnauthorized, Unauthorized},
		{http.StatusConflict, Conflict},
		{http.StatusInternalServerError, Unavailable},
		{http.StatusServiceUnavailable, Unavailable},
		{http.StatusBadGateway, Unavailable},
		{http.StatusOK, Unexpected},
		{http.StatusNotFound, Unexpected},
		{http.StatusTooManyRequests, Unexpected},
	}
	for _, tc := range cases {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, `{"error":{"code":"x","message":"y"}}`)
		})
		if got := c.RecordLocation(context.Background(), "tok", 1, 1); got != tc.want {
			t.Errorf("status %d -> %s, want %s", tc.status, got, tc.want)
		}
	}
}

func TestTimeoutIsClassified(t *testing.T) {
	c := newTestClient(t, slowHandler)
	c.locationTimeout = 50 * time.Millisecond
	if kind := c.RecordLocation(context.Background(), "tok", 1, 1); kind != Timeout {
		t.Fatalf("kind = %s, want timeout", kind)
	}
}

func TestCancellationIsNotAnError(t *testing.T) {
	c := newTestClient(t, slowHandler)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(30*time.Millisecond, cancel)
	if kind := c.RecordLocation(ctx, "tok", 1, 1); kind != Canceled {
		t.Fatalf("kind = %s, want canceled", kind)
	}
}

func TestConnectionRefusedIsNetwork(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close()
	c := New(url, 4)
	if kind := c.Register(context.Background(), "n", "e@x.test", "pw"); kind != Network {
		t.Fatalf("kind = %s, want network", kind)
	}
}

func TestRetryableKinds(t *testing.T) {
	retryable := map[Kind]bool{Unavailable: true, Timeout: true, Network: true}
	for k := OK; k <= Canceled; k++ {
		if k.Retryable() != retryable[k] {
			t.Errorf("%s.Retryable() = %v", k, k.Retryable())
		}
	}
}

func TestErrorKindsCarryNoRequestValues(t *testing.T) {
	// A Kind is a small integer; the only text it renders is its fixed name.
	for k := OK; k <= Canceled; k++ {
		if s := k.String(); s == "" || s == "unknown" {
			t.Errorf("kind %d renders as %q", k, s)
		}
	}
}

func TestHealth(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	})
	if kind := c.Health(context.Background()); kind != OK {
		t.Fatalf("Health = %s", kind)
	}
}
