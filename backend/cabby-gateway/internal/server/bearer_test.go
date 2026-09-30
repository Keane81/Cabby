package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Keane81/Cabby/backend/cabby-gateway/internal/authclient"
)

// bearerRoute is the shape of an account operation as this phase tests it: the access is read from
// the header, and whatever the port says about it is what the client reads. The operation that
// drives it is the exit of an access, so the port method here is the one it will use.
func bearerRoute(operations authclient.Operations) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		access, named := bearerToken(w, r)
		if !named {
			return
		}
		if err := operations.DeleteCabberSession(r.Context(), access); err != nil {
			writeAuthFailure(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func bearerAnswer(t *testing.T, authorization string, err error) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, pathCabberSession, nil)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	bearerRoute(&stubOperations{err: err}).ServeHTTP(response, request)
	return response
}

// TestAccessOfAnotherAccountIsRejectedLikeAnUnknownOne is SC-003 on the path where a client brings
// a credential: an absent header, a foreign scheme, an empty token and a live access the service
// does not accept all answer with the same bytes as a revoked one (FR-016).
func TestAccessOfAnotherAccountIsRejectedLikeAnUnknownOne(t *testing.T) {
	const secretAccess = "an-access-of-another-cabber"
	expired := bearerAnswer(t, "Bearer an-access-nobody-holds", authclient.ErrUnauthorized)
	if expired.Code != http.StatusUnauthorized {
		t.Fatalf("reference answer = %d, want 401", expired.Code)
	}

	for _, header := range []string{
		"",
		"Basic dXNlcjpwYXNz",
		"Bearer",
		"Bearer ",
		"Bearer " + strings.Repeat(" ", 1),
		"Token " + secretAccess,
	} {
		response := bearerAnswer(t, header, authclient.ErrUnauthorized)
		if response.Code != http.StatusUnauthorized {
			t.Errorf("%q: status = %d, want 401", header, response.Code)
		}
		if got, want := response.Body.String(), expired.Body.String(); got != want {
			t.Errorf("%q: body = %s, want the answer of an expired access %s", header, got, want)
		}
		if got := response.Header().Get("Content-Type"); got != "application/json" {
			t.Errorf("%q: content type = %q", header, got)
		}
	}

	foreign := bearerAnswer(t, "Bearer "+secretAccess, authclient.ErrUnauthorized)
	if got, want := foreign.Body.String(), expired.Body.String(); got != want {
		t.Errorf("a foreign access answered %s, want %s", got, want)
	}
	if strings.Contains(foreign.Body.String(), secretAccess) {
		t.Errorf("the answer repeats the access it refused: %s", foreign.Body)
	}
}

// TestBearerHeaderIsReadBeforeTheService is FR-016 on the gateway side: a request that brings no
// credential to confirm never reaches the auth service, so no dependency can report on it.
func TestBearerHeaderIsReadBeforeTheService(t *testing.T) {
	for _, header := range []string{"", "Basic dXNlcjpwYXNz", "Bearer", "Bearer "} {
		operations := &stubOperations{}
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodDelete, pathCabberSession, nil)
		if header != "" {
			request.Header.Set("Authorization", header)
		}
		bearerRoute(operations).ServeHTTP(response, request)

		if response.Code != http.StatusUnauthorized {
			t.Errorf("%q: status = %d, want 401", header, response.Code)
		}
		if operations.calls != 0 {
			t.Errorf("%q: the service was called %d times, want none", header, operations.calls)
		}
	}
}

// TestBearerAccessTravelsToTheOperation unchanged: the gateway adds nothing to it, splits nothing
// off it, and the scheme is not part of the credential (R-03).
func TestBearerAccessTravelsToTheOperation(t *testing.T) {
	const access = "opaque-access-token"
	for _, header := range []string{"Bearer " + access, "bearer " + access, "BEARER " + access} {
		operations := &stubOperations{}
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodDelete, pathCabberSession, nil)
		request.Header.Set("Authorization", header)
		bearerRoute(operations).ServeHTTP(response, request)

		if response.Code != http.StatusNoContent {
			t.Errorf("%q: status = %d, want 204: %s", header, response.Code, response.Body)
		}
		if operations.sawAccess != access {
			t.Errorf("%q: the service saw %q, want %q", header, operations.sawAccess, access)
		}
	}
}

// TestBearerDependencyStaysSeparable keeps a rejection of the dependency out of the access
// failure: an unreachable service is not a demand to create a session again (edge case «Отказ хранилища»).
func TestBearerDependencyStaysSeparable(t *testing.T) {
	operations := &stubOperations{err: authclient.ErrUnavailable}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, pathCabberSession, nil)
	request.Header.Set("Authorization", "Bearer opaque-access")
	bearerRoute(operations).ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", response.Code, response.Body)
	}
	if detail := decodeError(t, response.Body.Bytes()); detail.Code != CodeServiceUnavailable {
		t.Errorf("code = %q, want %q", detail.Code, CodeServiceUnavailable)
	}
	if operations.calls != 1 {
		t.Errorf("calls = %d, want one", operations.calls)
	}
}
