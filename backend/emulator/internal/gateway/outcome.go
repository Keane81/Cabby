package gateway

import (
	"context"
	"errors"
	"net"
)

// Kind is the classified result of one call. It is the only thing a call returns besides a
// token: no response body, status text or request value is carried out of this package, so
// nothing secret can reach a log line through an error (FR-015).
type Kind int

const (
	// OK is the status the contract names for the operation (201 or 204).
	OK Kind = iota
	// Unauthorized is 401: the session is unknown, expired or revoked, or the credentials are wrong.
	Unauthorized
	// InvalidRequest is 400. For the emulator it means a defect in the emulator itself.
	InvalidRequest
	// Conflict is 409: the email is already claimed.
	Conflict
	// Unavailable is a 5xx answer.
	Unavailable
	// Timeout is a call that did not finish in its own deadline.
	Timeout
	// Network is a call that failed before a status was received.
	Network
	// Unexpected is an answer the contract does not describe for the operation.
	Unexpected
	// Canceled is a call stopped because the run is being stopped; it is not an error of the system.
	Canceled
)

var kindNames = [...]string{
	OK: "ok", Unauthorized: "unauthorized", InvalidRequest: "invalid_request", Conflict: "conflict",
	Unavailable: "unavailable", Timeout: "timeout", Network: "network", Unexpected: "unexpected",
	Canceled: "canceled",
}

func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return "unknown"
}

// Retryable reports whether a failed registration or login is worth another attempt: only the
// failures a recovering system can cure (research.md R-04).
func (k Kind) Retryable() bool {
	return k == Unavailable || k == Timeout || k == Network
}

func classifyStatus(status, want int) Kind {
	switch {
	case status == want:
		return OK
	case status == 400:
		return InvalidRequest
	case status == 401:
		return Unauthorized
	case status == 409:
		return Conflict
	case status >= 500 && status <= 599:
		return Unavailable
	default:
		return Unexpected
	}
}

// classifyError separates a stop of the run from a deadline and from a transport failure.
func classifyError(parent context.Context, err error) Kind {
	if parent.Err() != nil {
		return Canceled
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return Timeout
	}
	return Network
}
