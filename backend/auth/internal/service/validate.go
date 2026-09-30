package service

import (
	"strings"
	"unicode/utf8"
)

// CanonicalName brings a name to the form it is stored in: the surrounding spaces go away,
// which is what the check constraint of the schema requires (data-model §1).
func CanonicalName(raw string) string {
	return strings.TrimSpace(raw)
}

// CanonicalEmail brings an address to the form it is stored and matched by: an account claimed
// through `Ivan@Example.com ` is the same account as `ivan@example.com` (FR-003).
func CanonicalEmail(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// ValidateRegistration reports the first defect of a registration whose fields have already been
// canonicalised, in the fixed order name → email → password (FR-006). The password is taken as
// presented: its case and its spaces are significant, and a password of an acceptable length is
// never accompanied by a judgement of its strength (FR-007).
func ValidateRegistration(name, email, plain string) error {
	if err := validateLength(FieldName, name, MinNameLength, MaxNameLength); err != nil {
		return err
	}
	if err := validateStorable(FieldName, name); err != nil {
		return err
	}
	if err := validateEmail(email); err != nil {
		return err
	}
	return validateLength(FieldPassword, plain, MinPasswordLength, MaxPasswordLength)
}

// ValidateCredentials guards the fields of an entry request. It checks only presence and the
// upper bound: the lower bounds belong to registration, and reapplying them here would give a
// client a second rejection to tell apart from the single one of FR-012 (SC-006).
func ValidateCredentials(email, plain string) error {
	if err := validateLength(FieldEmail, email, 0, MaxEmailLength); err != nil {
		return err
	}
	if err := validateStorable(FieldEmail, email); err != nil {
		return err
	}
	return validateLength(FieldPassword, plain, 0, MaxPasswordLength)
}

// validateEmail applies the minimal format of FR-006: at most MaxEmailLength characters, and
// exactly one `@` with a non-empty part on each side. Nothing else about an address is checked —
// no domain syntax, no deliverability — so `a@b` is a valid account (SC-010).
func validateEmail(email string) error {
	local, domain, hasAt := strings.Cut(email, "@")
	switch {
	case email == "":
		return Validation{FieldEmail, ReasonEmpty}
	case utf8.RuneCountInString(email) > MaxEmailLength:
		return Validation{FieldEmail, ReasonTooLong}
	case !hasAt || local == "" || domain == "" || strings.Contains(domain, "@"):
		return Validation{FieldEmail, ReasonInvalidFormat}
	}
	return validateStorable(FieldEmail, email)
}

// validateStorable rejects a NUL character: PostgreSQL text cannot hold one, so it would otherwise
// fail in storage and be reported as an unavailable dependency instead of an invalid field.
func validateStorable(field Field, value string) error {
	if strings.ContainsRune(value, 0) {
		return Validation{field, ReasonInvalidFormat}
	}
	return nil
}

// validateLength reports a value against its bounds, counting Unicode characters rather than
// bytes (R-08). min of 0 asks only for a non-empty value.
func validateLength(field Field, value string, min, max int) error {
	length := utf8.RuneCountInString(value)
	switch {
	case length == 0:
		return Validation{field, ReasonEmpty}
	case length < min:
		return Validation{field, ReasonTooShort}
	case length > max:
		return Validation{field, ReasonTooLong}
	}
	return nil
}
