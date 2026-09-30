package service

import "errors"

// Domain failures of the cases. The transport maps these into status codes; nothing below
// this package leaks into a client-facing message.
var (
	// ErrEmailTaken reports the address is already claimed. The attributes of the existing
	// account are never part of the failure (FR-009).
	ErrEmailTaken = errors.New("service: email is taken")
	// ErrInvalidSession is the single rejection of a credential or a session: unknown email,
	// wrong password, missing, expired, idle or revoked token, and a second session deletion alike
	// (FR-012, FR-016, FR-021).
	ErrInvalidSession = errors.New("service: session is not valid")
	// ErrDependency reports storage being unreachable. It is deliberately distinguishable from a
	// rejection so a client never sees a false success (edge case «Отказ хранилища»). The cause is
	// never carried: a database message can quote a row value, and neither a returned error nor a
	// log may repeat one (FR-004, data-model §6). Which query failed and how often stays in
	// cabby_auth_repository_query_total.
	ErrDependency = errors.New("service: dependency is unavailable")
)

// Field is the name of a request field as it appears in the contract.
type Field string

const (
	FieldEmail    Field = "email"
	FieldPassword Field = "password"
	FieldName     Field = "name"
)

// Reason is a stable machine-readable cause of a validation defect.
type Reason string

const (
	ReasonEmpty         Reason = "empty"
	ReasonTooLong       Reason = "too_long"
	ReasonTooShort      Reason = "too_short"
	ReasonInvalidFormat Reason = "invalid_format"
)

// Validation reports one defect of a request. A request with several defects carries the
// first one in the fixed order name → email → password (FR-006, data-model §6).
type Validation struct {
	Field  Field
	Reason Reason
}

func (v Validation) Error() string {
	return "service: field " + string(v.Field) + " is " + string(v.Reason)
}
