package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/Keane81/Cabby/backend/cabby-gateway/internal/requestid"
)

// maxCabberBody bounds one request body before it is parsed. The three short fields the contract
// accepts cannot bring a legitimate request anywhere near it, so the limit only stops a client
// that sends something else (FR-024).
const maxCabberBody = 4 << 10

// The bodies of data-model §5. A field the contract does not define is rejected rather than
// ignored, which is what additionalProperties: false promises a client.
type (
	cabberRegistration struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	cabberAnswer struct {
		CabberID string `json:"cabber_id"`
		Email    string `json:"email"`
	}
	cabberSessionRequest struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	cabberSessionAnswer struct {
		AccessToken string `json:"access_token"`
		ExpiresAt   string `json:"expires_at"`
	}
)

// cabberHandler answers one operation and reports the status the client read, so the dispatcher
// can count the same answer the client saw.
type cabberHandler func(http.ResponseWriter, *http.Request) int

// registerCabber answers POST /cabbers (FR-005). The rules of what a valid account is belong to
// the auth service: the gateway passes the data on and reports the answer.
func (rt *Router) registerCabber(w http.ResponseWriter, r *http.Request) int {
	var body cabberRegistration
	if !decodeCabberBody(w, r, &body) {
		return http.StatusBadRequest
	}

	started := time.Now()
	created, err := rt.operations.RegisterCabber(r.Context(), body.Name, body.Email, body.Password)
	rt.dependency(operationRegister, started)
	if err != nil {
		return writeAuthFailure(w, err)
	}
	return writeAnswer(w, http.StatusCreated, cabberAnswer{
		CabberID: created.ID,
		Email:    created.Email,
	})
}

// signInCabber answers POST /cabber/session (FR-011, FR-013).
func (rt *Router) signInCabber(w http.ResponseWriter, r *http.Request) int {
	var body cabberSessionRequest
	if !decodeCabberBody(w, r, &body) {
		return http.StatusBadRequest
	}

	started := time.Now()
	session, err := rt.operations.CreateCabberSession(r.Context(), body.Email, body.Password)
	rt.dependency(operationLogin, started)
	if err != nil {
		return writeAuthFailure(w, err)
	}
	return writeAnswer(w, http.StatusCreated, cabberSessionAnswer{
		AccessToken: session.AccessToken,
		ExpiresAt:   session.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

// signOutCabber answers DELETE /cabber/session (FR-019). The access comes out of the header and
// nothing else: neither the body nor the URI names the account the exit reaches, so a request cannot
// claim to sign somebody else out (R-03). A success has no body — 204 says the access is gone, and
// the client that made it so does not need it repeated back.
func (rt *Router) signOutCabber(w http.ResponseWriter, r *http.Request) int {
	access, named := bearerToken(w, r)
	if !named {
		return http.StatusUnauthorized
	}

	started := time.Now()
	err := rt.operations.DeleteCabberSession(r.Context(), access)
	rt.dependency(operationLogout, started)
	if err != nil {
		return writeAuthFailure(w, err)
	}
	w.WriteHeader(http.StatusNoContent)
	return http.StatusNoContent
}

// counted records the answer of one cabber operation. A request a body never reached the auth
// service for is still a request to the operation, so the counter covers every path.
func (rt *Router) counted(operation string, w http.ResponseWriter, r *http.Request, handler cabberHandler) {
	// The identifier is minted here rather than in the gRPC client: a request the body already
	// rejected never reaches that client, and its line still has to carry the value the search of
	// the log is made by.
	request := r.WithContext(requestid.Into(r.Context(), requestid.New()))
	status := handler(w, request)

	event := rt.logger.Info()
	if status >= http.StatusBadRequest {
		event = rt.logger.Warn()
	}
	// The shape of the line is the one auth writes, and no value of the request is in it (FR-004).
	event.Str("operation", operation).
		Str("request_id", requestid.From(request.Context())).
		Int("status", status).
		Msg("cabber request")

	if rt.observer != nil {
		rt.observer.observeCabber(operation, status)
	}
}

// dependency times the call to the auth service alone: R-11 splits the delay of a request between
// the HTTP layer of the gateway, the gRPC call and the storage behind it.
func (rt *Router) dependency(operation string, started time.Time) {
	if rt.observer != nil {
		rt.observer.observeDependency(operation, started)
	}
}

// decodeCabberBody reads the body of a cabber operation. It fails closed: an absent, oversized,
// malformed body or one carrying an unknown property is a 400 with no field, because no single
// field of the request is at fault.
func decodeCabberBody(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCabberBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "the request body is not a valid object")
		return false
	}
	// A second object in the same stream would otherwise be dropped without a word.
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "the request body is not a valid object")
		return false
	}
	return true
}

// writeAnswer sends a JSON body of an operation and returns the status it wrote.
func writeAnswer(w http.ResponseWriter, status int, answer any) int {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(answer)
	return status
}
