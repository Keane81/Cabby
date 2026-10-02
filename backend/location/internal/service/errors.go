package service

import (
	"errors"
	"fmt"
)

// Field names the part of a request a rejection refers to (contracts/location.proto, ErrorField).
type Field string

// Reason is the stable machine-readable cause of a rejection.
type Reason string

const (
	FieldLatitude  Field = "latitude"
	FieldLongitude Field = "longitude"

	// ReasonNotANumber marks a value that is NaN or infinite.
	ReasonNotANumber Reason = "not_a_number"
	// ReasonOutOfRange marks a finite value outside the bounds of the field.
	ReasonOutOfRange Reason = "out_of_range"
)

// Validation is the rejection of a request on one field. The value the client sent is never part
// of it (spec 004 FR-011): coordinates are personal data.
type Validation struct {
	Field  Field
	Reason Reason
}

func (v Validation) Error() string {
	return fmt.Sprintf("service: field %s is %s", v.Field, v.Reason)
}

var (
	// ErrDependency reports the storage being unreachable or failing; nothing was recorded.
	ErrDependency = errors.New("service: the location storage is unavailable")
	// ErrInvalidOwner reports a cabber identifier that is not a UUID. It is a defect of the caller
	// in the system, not of the client, so the transport answers INTERNAL.
	ErrInvalidOwner = errors.New("service: the cabber identifier is not valid")
)
