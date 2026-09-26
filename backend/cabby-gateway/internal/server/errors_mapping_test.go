package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Keane81/Cabby/backend/cabby-gateway/internal/authclient"
)

// TestAuthFailureFollowsTheContractTable walks the table of data-model §6 row by row: one domain
// result of the port gives exactly one HTTP status, one error.code, one fixed message and, where
// the contract names it, one error.field.
func TestAuthFailureFollowsTheContractTable(t *testing.T) {
	for _, tc := range []struct {
		desc      string
		err       error
		wantHTTP  int
		wantCode  ErrorCode
		wantMsg   string
		wantField string
	}{
		{
			desc: "defect of name", err: authclient.Invalid{Field: authclient.FieldName, Reason: "too_long"},
			wantHTTP: http.StatusBadRequest, wantCode: CodeInvalidRequest,
			wantMsg: "the request field is invalid", wantField: "name",
		},
		{
			desc: "defect of email", err: authclient.Invalid{Field: authclient.FieldEmail, Reason: "invalid_format"},
			wantHTTP: http.StatusBadRequest, wantCode: CodeInvalidRequest,
			wantMsg: "the request field is invalid", wantField: "email",
		},
		{
			desc: "defect of password", err: authclient.Invalid{Field: authclient.FieldPassword, Reason: "too_short"},
			wantHTTP: http.StatusBadRequest, wantCode: CodeInvalidRequest,
			wantMsg: "the request field is invalid", wantField: "password",
		},
		{
			desc: "rejection with no field", err: authclient.Invalid{},
			wantHTTP: http.StatusBadRequest, wantCode: CodeInvalidRequest,
			wantMsg: "the request is invalid",
		},
		{
			desc: "taken email", err: authclient.ErrEmailTaken,
			wantHTTP: http.StatusConflict, wantCode: CodeEmailTaken,
			wantMsg: "this email is already registered", wantField: "email",
		},
		{
			desc: "rejected credentials", err: authclient.ErrUnauthorized,
			wantHTTP: http.StatusUnauthorized, wantCode: CodeUnauthorized,
			wantMsg: "the credentials or the access are not valid",
		},
		{
			desc: "auth service down", err: authclient.ErrUnavailable,
			wantHTTP: http.StatusServiceUnavailable, wantCode: CodeServiceUnavailable,
			wantMsg: "the operation cannot be completed right now",
		},
		{
			desc: "failure we cannot describe", err: errors.New("pq: connection reset"),
			wantHTTP: http.StatusInternalServerError, wantCode: CodeInternalError,
			wantMsg: "the request could not be processed",
		},
		{
			desc: "status of its own", err: errors.New("authclient: something"),
			wantHTTP: http.StatusInternalServerError, wantCode: CodeInternalError,
			wantMsg: "the request could not be processed",
		},
	} {
		recorder := httptest.NewRecorder()
		writeAuthFailure(recorder, tc.err)

		if recorder.Code != tc.wantHTTP {
			t.Errorf("%s: status = %d, want %d", tc.desc, recorder.Code, tc.wantHTTP)
		}
		if got := recorder.Header().Get("Content-Type"); got != "application/json" {
			t.Errorf("%s: content type = %q", tc.desc, got)
		}
		var envelope errorEnvelope
		if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("%s: cannot read the envelope: %v\n%s", tc.desc, err, recorder.Body)
		}
		if envelope.Error.Code != tc.wantCode || envelope.Error.Message != tc.wantMsg {
			t.Errorf("%s: envelope = %+v, want {%s %s}", tc.desc, envelope.Error, tc.wantCode, tc.wantMsg)
		}
		if envelope.Error.Field != tc.wantField {
			t.Errorf("%s: error.field = %q, want %q", tc.desc, envelope.Error.Field, tc.wantField)
		}
		if tc.wantField == "" && strings.Contains(recorder.Body.String(), `"field"`) {
			t.Errorf("%s: the envelope carries an empty field: %s", tc.desc, recorder.Body)
		}
	}
}

// TestAuthFailureCarriesNoReasonOfItsOwn: the reason of a validation defect and the text of a
// storage failure stay inside the service. Both would let a client learn more than the category
// (data-model §6, FR-004).
func TestAuthFailureCarriesNoReasonOfItsOwn(t *testing.T) {
	const secret = "ivan@example.com"
	for _, err := range []error{
		authclient.Invalid{Field: authclient.FieldEmail, Reason: secret},
		errors.New("read cabber " + secret + ": connection reset"),
	} {
		recorder := httptest.NewRecorder()
		writeAuthFailure(recorder, err)
		if body := recorder.Body.String(); strings.Contains(body, secret) {
			t.Errorf("%v: the answer repeats a value of the request: %s", err, body)
		}
	}
}
