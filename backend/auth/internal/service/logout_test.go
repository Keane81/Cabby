package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/auth/internal/repo"
	"github.com/Keane81/Cabby/backend/auth/internal/token"
	"github.com/rs/zerolog"
)

// logoutFixture is one account with the accesses the test hands out. The clock is a variable
// rather than a constant because a logout is judged by what it left behind: a second attempt has
// to be refused at a moment the first one did not write.
type logoutFixture struct {
	service  *Service
	cabbers  *fakeCabbers
	sessions *fakeSessions
	moment   time.Time
}

func newLogoutFixture(t *testing.T) *logoutFixture {
	t.Helper()
	cabbers, sessions := newFakeCabbers(), newFakeSessions()
	fixture := &logoutFixture{cabbers: cabbers, sessions: sessions, moment: fixedNow}
	fixture.service = New(cabbers, sessions, cheap, zerolog.Nop(), func() time.Time { return fixture.moment })
	if _, err := fixture.service.Register(context.Background(), testName, testEmail, testPlain); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return fixture
}

// enter opens one access of the current moment, as a sign-in would.
func (f *logoutFixture) enter(t *testing.T) string {
	t.Helper()
	issued, err := f.service.Login(context.Background(), testEmail, testPlain)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	return issued.Token
}

// stored reads back the row behind an access, the way the next request will.
func (f *logoutFixture) stored(t *testing.T, access string) repo.Session {
	t.Helper()
	session, found, err := f.sessions.GetByDigest(context.Background(), token.Digest(access))
	if err != nil || !found {
		t.Fatalf("the access has no row: %v, %v", found, err)
	}
	return session
}

// TestLogoutRevokesTheAccessItWasGivenAndNoOther is FR-019 with FR-014: two devices of one cabber,
// one exit, and only the presenting device loses its access (US3 scenario 4, FR-020).
func TestLogoutRevokesTheAccessItWasGivenAndNoOther(t *testing.T) {
	ctx := context.Background()
	fixture := newLogoutFixture(t)
	presented, alongside := fixture.enter(t), fixture.enter(t)

	if err := fixture.service.Logout(ctx, presented); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := fixture.service.RequireCabber(ctx, presented); !errors.Is(err, ErrInvalidAccess) {
		t.Errorf("the revoked access still opens an operation: %v, want ErrInvalidAccess", err)
	}
	owner, err := fixture.service.RequireCabber(ctx, alongside)
	if err != nil {
		t.Errorf("the access alongside stopped working: %v", err)
	}
	if owner == "" {
		t.Error("the access alongside resolved to no owner")
	}
	if row := fixture.stored(t, presented); !row.RevokedAt.Equal(fixture.moment) {
		t.Errorf("revoked_at = %v, want %v", row.RevokedAt, fixture.moment)
	}
	if row := fixture.stored(t, alongside); !row.RevokedAt.IsZero() {
		t.Errorf("the access alongside carries a revoke: %v", row.RevokedAt)
	}
}

// TestSecondLogoutIsRefusedWithoutAChange is FR-021 and SC-007: the exit of an access that is
// already gone is refused like any request without a confirmed access, and it neither revokes a
// second row nor moves the stamp of the first.
func TestSecondLogoutIsRefusedWithoutAChange(t *testing.T) {
	ctx := context.Background()
	fixture := newLogoutFixture(t)
	access := fixture.enter(t)
	other := fixture.enter(t)

	if err := fixture.service.Logout(ctx, access); err != nil {
		t.Fatalf("first Logout: %v", err)
	}
	revokedAt := fixture.stored(t, access).RevokedAt
	fixture.moment = fixture.moment.Add(time.Hour)

	err := fixture.service.Logout(ctx, access)
	if !errors.Is(err, ErrInvalidAccess) {
		t.Fatalf("second Logout = %v, want ErrInvalidAccess", err)
	}
	// The refusal reads as the refusal of an absent access: one error value, so no client can ask
	// which of the two states it is in (FR-016, SC-003).
	if err != ErrInvalidAccess {
		t.Errorf("the refusal is a wrapped error: %v", err)
	}
	if err := fixture.service.Logout(ctx, "an access nobody issued"); err != ErrInvalidAccess {
		t.Errorf("an unknown access = %v, want the same ErrInvalidAccess", err)
	}
	if row := fixture.stored(t, access); !row.RevokedAt.Equal(revokedAt) {
		t.Errorf("the second logout moved revoked_at to %v, want %v", row.RevokedAt, revokedAt)
	}
	// No revoke was even asked of the storage: the check before it already refused the request.
	if len(fixture.sessions.revoked) != 1 {
		t.Errorf("the storage revoked %d times, want once", len(fixture.sessions.revoked))
	}
	if row := fixture.stored(t, other); !row.RevokedAt.IsZero() {
		t.Errorf("the refused logout revoked the other access: %v", row.RevokedAt)
	}
}

// TestLoginAfterLogoutOpensAWorkingAccess is SC-004: the exit closes one access and leaves the
// account itself usable (US3 scenario 2).
func TestLoginAfterLogoutOpensAWorkingAccess(t *testing.T) {
	ctx := context.Background()
	fixture := newLogoutFixture(t)
	lost := fixture.enter(t)
	if err := fixture.service.Logout(ctx, lost); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	fresh := fixture.enter(t)
	if fresh == lost {
		t.Fatal("the new access is the revoked one")
	}
	if owner, err := fixture.service.RequireCabber(ctx, fresh); err != nil || owner == "" {
		t.Errorf("the access of a repeated sign-in does not work: %q, %v", owner, err)
	}
}

// TestLogoutOfAnAccessThatCannotBeUsed covers the forms a request arrives in: no credential, a
// credential nobody holds, and one past its absolute limit. All three are refused, and none of them
// reaches the revoke statement (FR-016, FR-021).
func TestLogoutOfAnAccessThatCannotBeUsed(t *testing.T) {
	ctx := context.Background()
	fixture := newLogoutFixture(t)
	dead := fixture.enter(t)
	fixture.moment = fixture.moment.Add(accessLifetime)

	for _, tc := range []struct {
		desc   string
		access string
	}{
		{"no credential", ""},
		{"an access nobody issued", "3j4P5k6L7m8N9o0P"},
		{"an access past its limit", dead},
	} {
		err := fixture.service.Logout(ctx, tc.access)
		if !errors.Is(err, ErrInvalidAccess) {
			t.Errorf("%s: Logout = %v, want ErrInvalidAccess", tc.desc, err)
		}
	}
	if len(fixture.sessions.revoked) != 0 {
		t.Errorf("a refused logout revoked %d times", len(fixture.sessions.revoked))
	}
	if row := fixture.stored(t, dead); !row.RevokedAt.IsZero() {
		t.Errorf("an expired access was stamped as revoked: %v", row.RevokedAt)
	}
}

// TestLogoutReportsStorageAsDependency keeps a lost connection out of the refusal: 503 must not read
// as «sign in again», or a client would discard a working credential (edge case «Отказ хранилища»).
func TestLogoutReportsStorageAsDependency(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		desc  string
		guard func(sessions *fakeSessions)
	}{
		{"the access cannot be read", func(sessions *fakeSessions) { sessions.getErr = errors.New("auth: select cabber_session: gone") }},
		{"the refresh cannot be written", func(sessions *fakeSessions) { sessions.touchErr = errors.New("auth: update cabber_session: gone") }},
		{"the revoke cannot be written", func(sessions *fakeSessions) { sessions.revokeErr = errors.New("auth: update cabber_session: gone") }},
	} {
		fixture := newLogoutFixture(t)
		access := fixture.enter(t)
		tc.guard(fixture.sessions)

		err := fixture.service.Logout(ctx, access)
		if !errors.Is(err, ErrDependency) {
			t.Errorf("%s: Logout = %v, want ErrDependency", tc.desc, err)
			continue
		}
		if errors.Is(err, ErrInvalidAccess) {
			t.Errorf("%s: a storage failure was reported as a rejection", tc.desc)
		}
	}
}

// TestLogoutRefusalCarriesNoCredential is FR-004 on the exit: the access a client handed over is a
// live credential, so neither a refusal nor a storage failure may repeat it — and a database message
// can quote the digest it was given.
func TestLogoutRefusalCarriesNoCredential(t *testing.T) {
	ctx := context.Background()
	fixture := newLogoutFixture(t)
	revoked := fixture.enter(t)
	if err := fixture.service.Logout(ctx, revoked); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	dead := fixture.stored(t, revoked)

	kept := fixture.enter(t)
	fixture.sessions.revokeErr = errors.New("auth: update cabber_session: " + string(token.Digest(kept)))

	for _, tc := range []struct {
		desc   string
		err    error
		secret string
	}{
		{"refusal of a revoked access", fixture.service.Logout(ctx, revoked), revoked},
		{"failure of the revoke", fixture.service.Logout(ctx, kept), kept},
	} {
		if tc.err == nil {
			t.Fatalf("%s: the logout was accepted", tc.desc)
		}
		for _, value := range []string{tc.secret, string(dead.TokenHash), testEmail, testName, dead.CabberID} {
			if strings.Contains(tc.err.Error(), value) {
				t.Errorf("%s repeats %q: %v", tc.desc, value, tc.err)
			}
		}
	}
}
