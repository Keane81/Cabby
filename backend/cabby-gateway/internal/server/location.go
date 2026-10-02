package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Keane81/Cabby/backend/cabby-gateway/internal/authclient"
	"github.com/Keane81/Cabby/backend/cabby-gateway/internal/locationclient"
)

// maxLocationBody bounds one body of the location operation before it is parsed. Two numbers cannot
// come near it, so the limit only stops a client that sends something else (spec 004 plan.md).
const maxLocationBody = 1 << 10

// The bodies of the operation. The coordinates are read raw so a missing value, a null and a
// string — which a float64 field would blur into zero or one error — each name the field at fault.
type (
	locationRequest struct {
		Latitude  json.RawMessage `json:"latitude"`
		Longitude json.RawMessage `json:"longitude"`
	}
	locationReceipt struct {
		ReceivedAt string `json:"received_at"`
	}
)

// receiptLayout is RFC 3339 with the microseconds the storage keeps and no trailing zeros.
const receiptLayout = "2006-01-02T15:04:05.999999Z07:00"

// recordCabberLocation answers POST /cabber/location (spec 004 FR-001). The cabber is the owner of the
// access in the header and nobody else: the body names no cabber, and a property that tried to is an
// unknown one and rejects the body (FR-009). The access is confirmed before the body is looked at, so
// an anonymous client learns nothing about what a valid body is.
func (rt *Router) recordCabberLocation(w http.ResponseWriter, r *http.Request) int {
	access, named := bearerToken(w, r)
	if !named {
		return http.StatusUnauthorized
	}

	started := time.Now()
	cabberID, err := rt.operations.VerifyCabberSession(r.Context(), access)
	rt.dependency(operationVerifySession, started)
	if err != nil {
		return writeAuthFailure(w, err)
	}

	var body locationRequest
	if !decodeBody(w, r, &body, maxLocationBody) {
		return http.StatusBadRequest
	}
	latitude, ok := coordinate(body.Latitude)
	if !ok {
		return writeInvalidCoordinate(w, locationclient.FieldLatitude)
	}
	longitude, ok := coordinate(body.Longitude)
	if !ok {
		return writeInvalidCoordinate(w, locationclient.FieldLongitude)
	}

	started = time.Now()
	receivedAt, err := rt.locations.RecordCabberLocation(r.Context(), cabberID, latitude, longitude)
	rt.dependency(operationRecordLocation, started)
	if err != nil {
		return writeLocationFailure(w, err)
	}
	return writeAnswer(w, http.StatusCreated, locationReceipt{ReceivedAt: receivedAt.UTC().Format(receiptLayout)})
}

// coordinate reads a JSON number. Absent, null, a string, a boolean, an object and a number beyond
// the range of a float64 are all refused: the ranges of the degrees are location's to judge.
func coordinate(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, false
	}
	return value, true
}

func writeInvalidCoordinate(w http.ResponseWriter, field string) int {
	return writeErrorOnField(w, http.StatusBadRequest, CodeInvalidRequest, "the request field is invalid", field)
}

// writeLocationFailure answers a record the location service did not accept, following the table of
// data-model §4. Nothing of what the service said, and no coordinate, is written into the message.
func writeLocationFailure(w http.ResponseWriter, err error) int {
	var invalid locationclient.Invalid
	switch {
	case errors.As(err, &invalid) && invalid.Field != "":
		return writeInvalidCoordinate(w, invalid.Field)
	case errors.As(err, &invalid):
		return writeError(w, http.StatusBadRequest, CodeInvalidRequest, "the request is invalid")
	case errors.Is(err, locationclient.ErrUnavailable), errors.Is(err, authclient.ErrUnavailable):
		return writeError(w, http.StatusServiceUnavailable, CodeServiceUnavailable,
			"the operation cannot be completed right now")
	default:
		return writeError(w, http.StatusInternalServerError, CodeInternalError,
			"the request could not be processed")
	}
}
