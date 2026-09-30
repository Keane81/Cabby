package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Keane81/Cabby/backend/auth/internal/password"
	"github.com/Keane81/Cabby/backend/auth/internal/repo"
	"github.com/Keane81/Cabby/backend/auth/internal/token"
)

func TestLoginOpensAnAccessOfTheInjectedMoment(t *testing.T) {
	ctx := context.Background()
	cabbers, sessions := newFakeCabbers(), newFakeSessions()
	service := newCaseService(cabbers, sessions)
	account, err := service.Register(ctx, testName, testEmail, testPlain)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	access, err := service.Login(ctx, " IVAN@EXAMPLE.COM ", testPlain)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if access.Token == "" {
		t.Fatal("login returned an empty access")
	}
	if want := fixedNow.Add(sessionLifetime); !access.ExpiresAt.Equal(want) {
		t.Errorf("expires_at = %s, want %s", access.ExpiresAt, want)
	}

	stored := sessions.created[0]
	if stored.CabberID != account.ID {
		t.Errorf("session belongs to %q, want %q", stored.CabberID, account.ID)
	}
	if stored.CreatedAt != fixedNow || stored.LastSeenAt != fixedNow {
		t.Errorf("session timestamps %s/%s, want both %s", stored.CreatedAt, stored.LastSeenAt, fixedNow)
	}
	if !stored.ExpiresAt.Equal(fixedNow.Add(sessionLifetime)) {
		t.Errorf("session expires at %s, want %s", stored.ExpiresAt, fixedNow.Add(sessionLifetime))
	}
	// Only the digest of a session reaches storage (R-04).
	if string(stored.TokenHash) == access.Token {
		t.Error("the session itself was stored instead of its digest")
	}
	if !token.Equal(stored.TokenHash, token.Digest(access.Token)) {
		t.Error("the stored digest does not match the issued access")
	}
	if _, err := service.Verify(ctx, access.Token); err != nil {
		t.Fatalf("Verify of a freshly issued access: %v", err)
	}
}

// TestLoginRejectsBothCausesWithTheSameAnswer is SC-006: neither of the two failures may be
// distinguishable, so an attacker cannot list registered addresses (FR-012).
func TestLoginRejectsBothCausesWithTheSameAnswer(t *testing.T) {
	ctx := context.Background()
	cabbers, sessions := newFakeCabbers(), newFakeSessions()
	service := newCaseService(cabbers, sessions)
	if _, err := service.Register(ctx, testName, testEmail, testPlain); err != nil {
		t.Fatalf("Register: %v", err)
	}

	unknown, err := service.Login(ctx, "nobody@example.com", testPlain)
	if !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("unknown email error = %v, want ErrInvalidSession", err)
	}
	wrong, err := service.Login(ctx, testEmail, decoyPlain)
	if !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("wrong password error = %v, want ErrInvalidSession", err)
	}
	if unknown != (Session{}) || wrong != (Session{}) {
		t.Error("a rejected sign-in returned a session")
	}
	// A rejection must not spend a credential: no row was written for either cause.
	if len(sessions.created) != 0 {
		t.Errorf("rejected sign-ins created %d sessions, want none", len(sessions.created))
	}
}

// TestDecoyIsACostlyVerifiableHash guards the mechanism behind SC-006: a sign-in for an address
// nobody owns still runs a derivation, and that derivation has to be parseable and as expensive
// as the one a real account gets, or the answer time reports which emails are registered.
func TestDecoyIsACostlyVerifiableHash(t *testing.T) {
	if _, err := password.Verify(context.Background(), decoyHash, "any password"); err != nil {
		t.Fatalf("Verify against the decoy: %v", err)
	}
	want := fmt.Sprintf("m=%d,t=%d,p=%d", password.Default.Memory, password.Default.Time, password.Default.Threads)
	if !strings.Contains(decoyHash, want) {
		t.Errorf("decoy hash does not carry the production parameters %q: %s", want, decoyHash)
	}
}

// TestLoginIssuesIndependentAccesses is FR-014: two sign-ins of one cabber give two credentials
// that expire and revoke separately.
func TestLoginIssuesIndependentAccesses(t *testing.T) {
	ctx := context.Background()
	cabbers, sessions := newFakeCabbers(), newFakeSessions()
	service := newCaseService(cabbers, sessions)
	if _, err := service.Register(ctx, testName, testEmail, testPlain); err != nil {
		t.Fatalf("Register: %v", err)
	}

	first, err := service.Login(ctx, testEmail, testPlain)
	if err != nil {
		t.Fatalf("first Login: %v", err)
	}
	second, err := service.Login(ctx, testEmail, testPlain)
	if err != nil {
		t.Fatalf("second Login: %v", err)
	}
	if first.Token == second.Token {
		t.Fatal("two sign-ins issued the same access")
	}
	if len(sessions.created) != 2 {
		t.Fatalf("stored sessions = %d, want two", len(sessions.created))
	}
	if token.Equal(sessions.created[0].TokenHash, sessions.created[1].TokenHash) {
		t.Error("two sign-ins stored the same digest")
	}
}

func TestLoginReportsStorageAsDependency(t *testing.T) {
	for _, tc := range []struct {
		desc  string
		guard func(cabbers *fakeCabbers, sessions *fakeSessions, hash string)
	}{
		{"unreadable account", func(cabbers *fakeCabbers, _ *fakeSessions, hash string) {
			cabbers.findErr = fmt.Errorf("auth: select cabber: %s", hash)
		}},
		{"unwritable access", func(_ *fakeCabbers, sessions *fakeSessions, hash string) {
			sessions.createErr = fmt.Errorf("auth: insert cabber_session: %s", hash)
		}},
	} {
		cabbers, sessions := newFakeCabbers(), newFakeSessions()
		service := newCaseService(cabbers, sessions)
		if _, err := service.Register(context.Background(), testName, testEmail, testPlain); err != nil {
			t.Fatalf("Register: %v", err)
		}
		stored := cabbers.stored[testEmail]
		tc.guard(cabbers, sessions, stored.PasswordHash)

		_, err := service.Login(context.Background(), testEmail, testPlain)
		if !errors.Is(err, ErrDependency) {
			t.Errorf("%s: Login error = %v, want ErrDependency", tc.desc, err)
			continue
		}
		if errors.Is(err, ErrInvalidSession) {
			t.Errorf("%s: a storage failure was reported as a rejection", tc.desc)
		}
		// The message of the database is not ours to repeat: it holds the stored hash above
		// (FR-004).
		for _, leak := range []string{stored.PasswordHash, testPlain, testEmail} {
			if strings.Contains(err.Error(), leak) {
				t.Errorf("%s error leaks a stored value: %v", tc.desc, err)
			}
		}
	}
}

// TestLoginGuardsOnlyPresenceAndSize keeps the login boundary narrow: the registration limits do
// not reapply here, so a client never gets a second rejection shape to tell apart (FR-012).
func TestLoginGuardsOnlyPresenceAndSize(t *testing.T) {
	cabbers, sessions := newFakeCabbers(), newFakeSessions()
	service := newCaseService(cabbers, sessions)
	ctx := context.Background()

	for _, tc := range []struct {
		desc       string
		email      string
		plain      string
		wantField  Field
		wantReason Reason
	}{
		{"absent email", "", testPlain, FieldEmail, ReasonEmpty},
		{"email over the limit", strings.Repeat("a", MaxEmailLength) + "@b", testPlain, FieldEmail, ReasonTooLong},
		{"absent password", testEmail, "", FieldPassword, ReasonEmpty},
		{"password over the limit", testEmail, strings.Repeat("1", MaxPasswordLength+1), FieldPassword, ReasonTooLong},
	} {
		_, err := service.Login(ctx, tc.email, tc.plain)

		var defect Validation
		if !errors.As(err, &defect) {
			t.Errorf("%s: Login error = %v, want a Validation defect", tc.desc, err)
			continue
		}
		if defect.Field != tc.wantField || defect.Reason != tc.wantReason {
			t.Errorf("%s: defect = %s/%s, want %s/%s", tc.desc, defect.Field, defect.Reason, tc.wantField, tc.wantReason)
		}
	}
	if len(sessions.created) != 0 || len(cabbers.stored) != 0 {
		t.Errorf("invalid sign-ins touched storage: %d sessions, %d accounts", len(sessions.created), len(cabbers.stored))
	}
}

// TestLoginOfAStoredCabberKeepsNoTraceOfTheCredential repeats the leak guard for the row the
// service reads back: the returned values of a rejection carry no account data.
func TestLoginOfAStoredCabberKeepsNoTraceOfTheCredential(t *testing.T) {
	cabbers, sessions := newFakeCabbers(), newFakeSessions()
	service := newCaseService(cabbers, sessions)
	ctx := context.Background()
	if _, err := service.Register(ctx, testName, testEmail, testPlain); err != nil {
		t.Fatalf("Register: %v", err)
	}
	var row repo.Cabber
	for _, stored := range cabbers.stored {
		row = stored
	}

	_, err := service.Login(ctx, testEmail, "wrong-one")
	if err == nil {
		t.Fatal("Login accepted a password nobody registered")
	}
	for _, leak := range []string{row.PasswordHash, row.Email, row.Name, testPlain} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("rejection leaks a stored value: %v", err)
		}
	}
	if _, err := service.Verify(ctx, "wrong-one"); !errors.Is(err, ErrInvalidSession) {
		t.Errorf("Verify error = %v, want ErrInvalidSession", err)
	}
}
