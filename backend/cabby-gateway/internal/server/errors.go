package server

import (
	"encoding/json"
	"net/http"
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
)

type errorEnvelope struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

// writeError sends the unified error envelope as JSON. Messages are fixed per
// category and never echo request data, secrets, or internal details.
func writeError(w http.ResponseWriter, status int, code ErrorCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorEnvelope{Error: errorDetail{Code: code, Message: message}})
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
