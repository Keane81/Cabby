package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Keane81/Cabby/backend/cabby-gateway/internal/authclient"
)

// ErrorCode is a stable, machine-readable failure category exposed to external
// clients in the unified error envelope. New codes are additive (minor) changes;
// clients must tolerate unknown codes.
type ErrorCode string

const (
	// CodeUnknownOperation marks a request to a path absent from the contract.
	CodeUnknownOperation ErrorCode = "unknown_operation"
	// CodeMethodNotAllowed marks an unsupported method for a known path.
	CodeMethodNotAllowed ErrorCode = "method_not_allowed"
	// CodeInvalidRequest marks a field the auth service rejected (FR-006).
	CodeInvalidRequest ErrorCode = "invalid_request"
	// CodeUnauthorized marks missing, invalid or revoked credentials and access
	// (FR-012, FR-016, FR-021).
	CodeUnauthorized ErrorCode = "unauthorized"
	// CodeEmailTaken marks an address that already belongs to an account (FR-009).
	CodeEmailTaken ErrorCode = "email_taken"
	// CodeServiceUnavailable marks the auth service or its storage being unreachable (R-09).
	CodeServiceUnavailable ErrorCode = "service_unavailable"
	// CodeInternalError is the fixed category of a failure we cannot describe to a client.
	// The enum of spec 002 had no code for a 500, and data-model §6 promises a response
	// without details; this is that code (CHK010).
	CodeInternalError ErrorCode = "internal_error"
)

type errorEnvelope struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	// Field names the request field a rejection refers to and is present only when the
	// contract has one (data-model §5).
	Field string `json:"field,omitempty"`
}

// writeError sends the unified error envelope as JSON. Messages are fixed per
// category and never echo request data, secrets, or internal details. The status
// is returned so a caller can count the answer it gave.
func writeError(w http.ResponseWriter, status int, code ErrorCode, message string) int {
	return writeErrorOnField(w, status, code, message, "")
}

// writeErrorOnField is writeError for the categories that name a request field.
func writeErrorOnField(w http.ResponseWriter, status int, code ErrorCode, message, field string) int {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorEnvelope{Error: errorDetail{Code: code, Message: message, Field: field}})
	return status
}

// writeAuthFailure answers a request the auth service refused, following the table of
// data-model §6. The domain error decides the status and the category; nothing of it, and
// nothing the service said, is written into the message (FR-004, FR-016).
func writeAuthFailure(w http.ResponseWriter, err error) int {
	var invalid authclient.Invalid
	switch {
	case errors.As(err, &invalid) && invalid.Field != "":
		return writeErrorOnField(w, http.StatusBadRequest, CodeInvalidRequest,
			"the request field is invalid", invalid.Field)
	case errors.As(err, &invalid):
		return writeError(w, http.StatusBadRequest, CodeInvalidRequest,
			"the request is invalid")
	case errors.Is(err, authclient.ErrEmailTaken):
		return writeErrorOnField(w, http.StatusConflict, CodeEmailTaken,
			"this email is already registered", string(authclient.FieldEmail))
	case errors.Is(err, authclient.ErrUnauthorized):
		return writeInvalidAccess(w)
	case errors.Is(err, authclient.ErrUnavailable):
		return writeError(w, http.StatusServiceUnavailable, CodeServiceUnavailable,
			"the operation cannot be completed right now")
	default:
		return writeError(w, http.StatusInternalServerError, CodeInternalError,
			"the request could not be processed")
	}
}

// msgInvalidAccess is the one text every rejection of a credential or an access answers with,
// from a header the gateway cannot read to an access the service does not know (FR-016).
const msgInvalidAccess = "the credentials or the access are not valid"

// writeInvalidAccess answers a request whose access cannot be honoured. A client reads the same
// status, code and message whatever the reason was, so the answer cannot be used to tell the
// states of an access apart (FR-016, FR-018, SC-003).
func writeInvalidAccess(w http.ResponseWriter) int {
	return writeError(w, http.StatusUnauthorized, CodeUnauthorized, msgInvalidAccess)
}

func writeUnknownOperation(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, CodeUnknownOperation,
		"requested operation is not part of the published contract")
}

func writeMethodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeError(w, http.StatusMethodNotAllowed, CodeMethodNotAllowed,
		"method not allowed for this operation")
}
