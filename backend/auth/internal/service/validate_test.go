package service

import (
	"strings"
	"testing"
)

const (
	validName  = "Иван"
	validEmail = "ivan@example.com"
	validPlain = "1234"
)

// TestValidateRegistrationAccepts covers SC-005 and SC-010: every address with a single `@`
// between non-empty parts is accepted, however weak it looks, and a password of an acceptable
// length is accepted whatever it contains and without any warning.
func TestValidateRegistrationAccepts(t *testing.T) {
	for _, tc := range []struct {
		desc, name, email, plain string
	}{
		{"ordinary", validName, validEmail, validPlain},
		{"address with no domain syntax", validName, "a@b", validPlain},
		{"address with a host name", validName, "ivan@localhost", validPlain},
		{"address of punctuation", validName, "??@!!", validPlain},
		{"shortest name", "И", validEmail, validPlain},
		{"longest name", strings.Repeat("И", MaxNameLength), validEmail, validPlain},
		{"longest address", validName, strings.Repeat("a", MaxEmailLength-6) + "@b.com", validPlain},
		{"shortest password", validName, validEmail, "1234"},
		{"longest password", validName, validEmail, strings.Repeat("a", MaxPasswordLength)},
		{"password of spaces", validName, validEmail, "    "},
		{"password of any composition", validName, validEmail, "Пароль!1"},
	} {
		if err := ValidateRegistration(tc.name, tc.email, tc.plain); err != nil {
			t.Errorf("%s: ValidateRegistration = %v, want nil", tc.desc, err)
		}
	}
}

// TestValidateRegistrationRejects pins the reason of every rejection: field and cause only, never
// a value from the request (FR-004, FR-006).
func TestValidateRegistrationRejects(t *testing.T) {
	for _, tc := range []struct {
		desc, name, email, plain string
		want                     Validation
	}{
		{"empty name", "", validEmail, validPlain, Validation{FieldName, ReasonEmpty}},
		{"name too long", strings.Repeat("И", MaxNameLength+1), validEmail, validPlain, Validation{FieldName, ReasonTooLong}},
		{"empty address", validName, "", validPlain, Validation{FieldEmail, ReasonEmpty}},
		{"address without @", validName, "ivan.example.com", validPlain, Validation{FieldEmail, ReasonInvalidFormat}},
		{"empty local part", validName, "@example.com", validPlain, Validation{FieldEmail, ReasonInvalidFormat}},
		{"empty domain part", validName, "ivan@", validPlain, Validation{FieldEmail, ReasonInvalidFormat}},
		{"two separators", validName, "ivan@corp@example.com", validPlain, Validation{FieldEmail, ReasonInvalidFormat}},
		{"address too long", validName, strings.Repeat("a", MaxEmailLength) + "@b", validPlain, Validation{FieldEmail, ReasonTooLong}},
		{"empty password", validName, validEmail, "", Validation{FieldPassword, ReasonEmpty}},
		{"password below the bound", validName, validEmail, "123", Validation{FieldPassword, ReasonTooShort}},
		{"password above the bound", validName, validEmail, strings.Repeat("a", MaxPasswordLength+1), Validation{FieldPassword, ReasonTooLong}},
	} {
		got := ValidateRegistration(tc.name, tc.email, tc.plain)
		if got != tc.want {
			t.Errorf("%s: ValidateRegistration = %v, want %v", tc.desc, got, tc.want)
		}
	}
}

// TestValidateRegistrationReportsTheFirstDefect fixes the order name → email → password the
// contract promises in ErrorField (FR-006, contracts/auth.proto).
func TestValidateRegistrationReportsTheFirstDefect(t *testing.T) {
	tooLong := strings.Repeat("a", MaxPasswordLength+1)
	for _, tc := range []struct {
		desc, name, email, plain string
		want                     Field
	}{
		{"name over email and password", strings.Repeat("И", MaxNameLength+1), "no-separator", tooLong, FieldName},
		{"email over password", validName, "no-separator", tooLong, FieldEmail},
	} {
		got, ok := ValidateRegistration(tc.name, tc.email, tc.plain).(Validation)
		if !ok || got.Field != tc.want {
			t.Errorf("%s: ValidateRegistration = %v, want a defect of %q", tc.desc, got, tc.want)
		}
	}
}

// TestValidateRegistrationCountsCharactersNotBytes is R-08: a Cyrillic name of 64 characters
// takes 128 bytes and stays valid.
func TestValidateRegistrationCountsCharactersNotBytes(t *testing.T) {
	cyrillic := strings.Repeat("И", MaxNameLength)
	if len(cyrillic) <= MaxNameLength {
		t.Fatalf("the fixture is not multibyte: %d bytes for %d characters", len(cyrillic), MaxNameLength)
	}
	if err := ValidateRegistration(cyrillic, validEmail, validPlain); err != nil {
		t.Errorf("a name of %d characters = %v, want nil", MaxNameLength, err)
	}
	if err := ValidateRegistration(cyrillic+"И", validEmail, validPlain); err == nil {
		t.Error("a name of 65 multibyte characters was accepted")
	}
}

func TestCanonicalCredentials(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"Ivan@Example.com", "ivan@example.com"},
		{"  ivan@example.com ", "ivan@example.com"},
		{"\tIVAN@EXAMPLE.COM\n", "ivan@example.com"},
		{"ivan@localhost", "ivan@localhost"},
	} {
		if got := CanonicalEmail(tc.raw); got != tc.want {
			t.Errorf("CanonicalEmail(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
	if got := CanonicalName("  Иван  "); got != "Иван" {
		t.Errorf("CanonicalName = %q, want %q", got, "Иван")
	}
}

// TestValidateCredentialsGuardsOnlyPresenceAndSize keeps the entry request out of the
// registration policy: a short password is not a validation defect, so it cannot become a second
// answer to tell apart from the single rejection of FR-012 (SC-006).
func TestValidateCredentialsGuardsOnlyPresenceAndSize(t *testing.T) {
	for _, tc := range []struct {
		desc, email, plain string
		want               error
	}{
		{"short password", validEmail, "1", nil},
		{"both present", validEmail, validPlain, nil},
		{"missing address", "", validPlain, Validation{FieldEmail, ReasonEmpty}},
		{"address too long", strings.Repeat("a", MaxEmailLength+1), validPlain, Validation{FieldEmail, ReasonTooLong}},
		{"missing password", validEmail, "", Validation{FieldPassword, ReasonEmpty}},
		{"password too long", validEmail, strings.Repeat("a", MaxPasswordLength+1), Validation{FieldPassword, ReasonTooLong}},
	} {
		got := ValidateCredentials(tc.email, tc.plain)
		if got != tc.want {
			t.Errorf("%s: ValidateCredentials = %v, want %v", tc.desc, got, tc.want)
		}
	}
}

// TestValidationMessageCarriesNoRequestData: the message names the field and the cause, never a
// value the account could be read out of (FR-004).
func TestValidationMessageCarriesNoRequestData(t *testing.T) {
	got, ok := ValidateRegistration(validName, validEmail, "123").(Validation)
	if !ok {
		t.Fatalf("a short password gave %T, want Validation", got)
	}
	if msg := got.Error(); strings.Contains(msg, validEmail) || strings.Contains(msg, "123") {
		t.Errorf("Validation.Error() = %q, contains request data", msg)
	}
}

// TestValidateRejectsNULInStoredFields keeps a value PostgreSQL cannot store from reaching it,
// where it would be reported as an unavailable dependency rather than an invalid field.
func TestValidateRejectsNULInStoredFields(t *testing.T) {
	want := Validation{FieldName, ReasonInvalidFormat}
	if got := ValidateRegistration("a\x00b", validEmail, validPlain); got != want {
		t.Fatalf("name with NUL: got %v, want %v", got, want)
	}
	want = Validation{FieldEmail, ReasonInvalidFormat}
	if got := ValidateRegistration(validName, "a\x00b@example.com", validPlain); got != want {
		t.Fatalf("registration email with NUL: got %v, want %v", got, want)
	}
	if got := ValidateCredentials("a\x00b@example.com", validPlain); got != want {
		t.Fatalf("session creation email with NUL: got %v, want %v", got, want)
	}
}
