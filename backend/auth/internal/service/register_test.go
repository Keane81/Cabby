package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Keane81/Cabby/backend/auth/internal/repo"
)

const (
	testName   = "Иван"
	testEmail  = "ivan@example.com"
	testPlain  = "1234"
	testSecret = "secret-address@example.com"
	decoyName  = "Друг"
	decoyPlain = "5678"
)

func TestRegisterReturnsTheAccountAndNoAccess(t *testing.T) {
	cabbers, sessions := newFakeCabbers(), newFakeSessions()
	service := newCaseService(cabbers, sessions)

	created, err := service.Register(context.Background(), "  "+testName+"  ", "Ivan@Example.com ", testPlain)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if created.ID == "" {
		t.Fatal("registration returned an account without an identifier")
	}
	if created.Email != testEmail {
		t.Errorf("email = %q, want the canonical %q", created.Email, testEmail)
	}

	stored, found := cabbers.stored[testEmail]
	if !found {
		t.Fatalf("no account stored under %q", testEmail)
	}
	if stored.Name != testName {
		t.Errorf("stored name = %q, want %q", stored.Name, testName)
	}
	if stored.PasswordHash == "" || stored.PasswordHash == testPlain {
		t.Errorf("stored hash = %q, want a derivation of the password", stored.PasswordHash)
	}
	// Registration never opens an access (FR-010): the cabber signs in separately.
	if len(sessions.created) != 0 {
		t.Errorf("registration created %d accesses, want none", len(sessions.created))
	}
}

// TestRegisterKeepsOneAccountPerCanonicalAddress covers FR-009 and the canonicalisation rule of
// data-model §1: two spellings of an address are one account, and the second one is refused.
func TestRegisterKeepsOneAccountPerCanonicalAddress(t *testing.T) {
	cabbers, sessions := newFakeCabbers(), newFakeSessions()
	service := newCaseService(cabbers, sessions)
	ctx := context.Background()

	if _, err := service.Register(ctx, testName, "Ivan@Example.com ", testPlain); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if _, err := service.Register(ctx, decoyName, testEmail, decoyPlain); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("second Register error = %v, want ErrEmailTaken", err)
	}
	if len(cabbers.stored) != 1 {
		t.Fatalf("stored accounts = %d, want one", len(cabbers.stored))
	}
}

// TestTakenEmailLeavesTheStoredAccountUntouched is the other half of FR-009: a refusal must not
// change the existing account and must not reveal anything of it.
func TestTakenEmailLeavesTheStoredAccountUntouched(t *testing.T) {
	cabbers, sessions := newFakeCabbers(), newFakeSessions()
	service := newCaseService(cabbers, sessions)
	ctx := context.Background()

	if _, err := service.Register(ctx, testName, testEmail, testPlain); err != nil {
		t.Fatalf("Register: %v", err)
	}
	before := cabbers.stored[testEmail]

	_, taken := service.Register(ctx, decoyName, testEmail, decoyPlain)
	if !errors.Is(taken, ErrEmailTaken) {
		t.Fatalf("Register error = %v, want ErrEmailTaken", taken)
	}

	after := cabbers.stored[testEmail]
	if after.ID != before.ID || after.Name != before.Name || after.Email != before.Email ||
		after.PasswordHash != before.PasswordHash {
		t.Errorf("refused registration changed the account: %+v became %+v", before, after)
	}
	if strings.Contains(taken.Error(), before.PasswordHash) || strings.Contains(taken.Error(), before.Name) {
		t.Errorf("refusal carries attributes of the stored account: %v", taken)
	}
}

func TestRegisterRejectsInvalidRequestWithoutStoringAnything(t *testing.T) {
	cabbers, sessions := newFakeCabbers(), newFakeSessions()
	service := newCaseService(cabbers, sessions)
	ctx := context.Background()

	for _, tc := range []struct {
		desc        string
		name, email string
		plain       string
		wantField   Field
		wantReason  Reason
	}{
		{"blank name", "   ", testEmail, testPlain, FieldName, ReasonEmpty},
		{"name over the limit", strings.Repeat("И", MaxNameLength+1), testEmail, testPlain, FieldName, ReasonTooLong},
		{"address without a separator", testName, "ivan.example.com", testPlain, FieldEmail, ReasonInvalidFormat},
		{"short password", testName, testEmail, "123", FieldPassword, ReasonTooShort},
	} {
		_, err := service.Register(ctx, tc.name, tc.email, tc.plain)

		var defect Validation
		if !errors.As(err, &defect) {
			t.Errorf("%s: error = %v, want a Validation defect", tc.desc, err)
			continue
		}
		if defect.Field != tc.wantField || defect.Reason != tc.wantReason {
			t.Errorf("%s: defect = %s/%s, want %s/%s", tc.desc, defect.Field, defect.Reason, tc.wantField, tc.wantReason)
		}
	}
	// FR-008: a rejected request leaves no partial record behind.
	if len(cabbers.stored) != 0 {
		t.Errorf("stored accounts = %d, want none after rejections", len(cabbers.stored))
	}
}

func TestRegisterReportsStorageAsDependency(t *testing.T) {
	cabbers := newFakeCabbers()
	cabbers.createErr = errors.New("auth: insert cabber: conn closed")
	service := newCaseService(cabbers, newFakeSessions())

	_, err := service.Register(context.Background(), testName, testEmail, testPlain)
	if !errors.Is(err, ErrDependency) {
		t.Fatalf("Register error = %v, want ErrDependency", err)
	}
	if errors.Is(err, ErrEmailTaken) {
		t.Fatal("a lost connection must not be reported as a taken email")
	}
}

// TestRegistrationFailureCarriesNoRequestData guards FR-004 at the case boundary: the transport
// repeats these texts to a client, so a defect message must hold field names only.
func TestRegistrationFailureCarriesNoRequestData(t *testing.T) {
	cabbers := newFakeCabbers()
	cabbers.createErr = errors.New("auth: insert cabber: " + testSecret)
	service := newCaseService(cabbers, newFakeSessions())

	_, defect := service.Register(context.Background(), testName, testEmail, strings.Repeat("1", MaxPasswordLength+1))
	assertMentionsNothing(t, "invalid registration", defect)

	_, failure := service.Register(context.Background(), testName, testEmail, testPlain)
	assertMentionsNothing(t, "storage failure", failure)
	if !errors.Is(failure, ErrDependency) {
		t.Fatalf("Register error = %v, want ErrDependency", failure)
	}
}

// assertMentionsNothing checks the two values a cabber typed and the address of a stored account
// never reach an error text.
func assertMentionsNothing(t *testing.T, desc string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: error is nil", desc)
	}
	for _, leak := range []string{testEmail, testSecret, decoyPlain, strings.Repeat("1", MaxPasswordLength+1)} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("%s error leaks %q: %v", desc, leak, err)
		}
	}
}

// TestRegisterPassesCanonicalValuesToStorage keeps the repository honest: the row it receives is
// already canonical, which is what the check constraints of 0001_cabber require.
func TestRegisterPassesCanonicalValuesToStorage(t *testing.T) {
	cabbers, sessions := newFakeCabbers(), newFakeSessions()
	service := newCaseService(cabbers, sessions)

	if _, err := service.Register(context.Background(), "  "+testName+" ", " IVAN@EXAMPLE.COM", testPlain); err != nil {
		t.Fatalf("Register: %v", err)
	}
	var stored repo.Cabber
	for _, row := range cabbers.stored {
		stored = row
	}
	if stored.Email != testEmail || stored.Name != testName {
		t.Errorf("stored %+v, want email %q and name %q", stored, testEmail, testName)
	}
}
