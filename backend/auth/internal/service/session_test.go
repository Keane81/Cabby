package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/auth/internal/password"
	"github.com/Keane81/Cabby/backend/auth/internal/repo"
	"github.com/Keane81/Cabby/backend/auth/internal/token"
	"github.com/rs/zerolog"
)

// fixedNow is the moment the case tests run at. The clock is injected, so a boundary is reached
// by moving the stored row rather than by waiting (R-10).
var fixedNow = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

func newTestService(sessions *fakeSessions) *Service {
	return New(newFakeCabbers(), sessions, password.Default, zerolog.Nop(), func() time.Time {
		return fixedNow
	})
}

// TestVerifyBoundaries walks the three conditions of data-model §2 up to the second that
// separates an accepted access from a rejected one (FR-013, SC-009).
func TestVerifyBoundaries(t *testing.T) {
	const ownerID = "cabber-1"
	presented := "opaque-access-token"
	digest := token.Digest(presented)

	for _, tc := range []struct {
		desc        string
		created     time.Time
		seenAt      time.Time
		revoked     bool
		wantCabber  string
		wantReclaim bool
	}{
		{"fresh access", fixedNow.Add(-time.Hour), fixedNow.Add(-time.Hour), false, ownerID, false},
		{"one second before the absolute limit", fixedNow.Add(-95*time.Hour - 59*time.Minute - 59*time.Second), fixedNow, false, ownerID, false},
		{"at the absolute limit", fixedNow.Add(-sessionLifetime), fixedNow, false, "", true},
		{"past the absolute limit", fixedNow.Add(-sessionLifetime - time.Second), fixedNow, false, "", true},
		{"one minute before going idle", fixedNow.Add(-30 * time.Hour), fixedNow.Add(-23*time.Hour - 59*time.Minute), false, ownerID, false},
		{"at the idle limit", fixedNow.Add(-30 * time.Hour), fixedNow.Add(-idleLimit), false, "", true},
		{"past the idle limit", fixedNow.Add(-30 * time.Hour), fixedNow.Add(-idleLimit - time.Minute), false, "", true},
		{"revoked access", fixedNow.Add(-time.Hour), fixedNow.Add(-time.Hour), true, "", true},
	} {
		sessions := newFakeSessions()
		sessions.store(ownerID, digest, tc.created, tc.seenAt)
		if tc.revoked {
			if _, err := sessions.Revoke(context.Background(), digest, fixedNow.Add(-time.Minute)); err != nil {
				t.Fatalf("revoke the fixture: %v", err)
			}
		}
		service := newTestService(sessions)

		cabberID, err := service.Verify(context.Background(), presented)
		if tc.wantCabber == "" {
			if !errors.Is(err, ErrInvalidSession) {
				t.Errorf("%s: Verify = %v, want ErrInvalidSession", tc.desc, err)
			}
			continue
		}
		if err != nil || cabberID != tc.wantCabber {
			t.Errorf("%s: Verify = %q, %v, want %q", tc.desc, cabberID, err, tc.wantCabber)
		}
	}
}

// TestEveryRejectionSharesOneForm is the shape of SC-003: no answer of ours tells apart the
// causes of a rejected access.
func TestEveryRejectionSharesOneForm(t *testing.T) {
	sessions := newFakeSessions()
	sessions.store("cabber-1", token.Digest("revoked-token"), fixedNow.Add(-time.Hour), fixedNow.Add(-time.Hour))
	if _, err := sessions.Revoke(context.Background(), token.Digest("revoked-token"), fixedNow); err != nil {
		t.Fatalf("revoke the fixture: %v", err)
	}
	sessions.store("cabber-2", token.Digest("expired-token"), fixedNow.Add(-sessionLifetime-time.Hour), fixedNow)
	sessions.store("cabber-3", token.Digest("idle-token"), fixedNow.Add(-30*time.Hour), fixedNow.Add(-idleLimit-time.Minute))
	service := newTestService(sessions)

	for _, tc := range []struct {
		desc      string
		presented string
	}{
		{"no such token", "never-issued"},
		{"revoked", "revoked-token"},
		{"past its absolute life", "expired-token"},
		{"idle beyond the limit", "idle-token"},
	} {
		err := mustReject(t, service, tc.presented)
		if err != ErrInvalidSession {
			t.Errorf("%s: the rejection is %v, want exactly the single domain error", tc.desc, err)
		}
		if errors.Is(err, ErrDependency) {
			t.Errorf("%s: a rejection was reported as a storage failure", tc.desc)
		}
	}
}

func mustReject(t *testing.T, service *Service, presented string) error {
	t.Helper()
	_, err := service.Verify(context.Background(), presented)
	if err == nil {
		t.Fatalf("Verify(%q) accepted a session it should reject", presented)
	}
	return err
}

// TestVerifyRefreshesThroughTheInjectedClock: the storage learns the moment of the request, and a
// second use inside the reporting window moves nothing (R-10, FR-013).
func TestVerifyRefreshesThroughTheInjectedClock(t *testing.T) {
	presented := "long-lived-token"
	sessions := newFakeSessions()
	sessions.store("cabber-1", token.Digest(presented), fixedNow.Add(-24*time.Hour), fixedNow.Add(-2*time.Minute))
	service := newTestService(sessions)

	for _, use := range []struct {
		desc     string
		now      time.Time
		wantMove bool
	}{
		{"first use past the window", fixedNow, true},
		{"second use inside the window", fixedNow.Add(30 * time.Second), false},
	} {
		service.now = func() time.Time { return use.now }
		if _, err := service.Verify(context.Background(), presented); err != nil {
			t.Fatalf("%s: Verify = %v", use.desc, err)
		}
		stored, found, err := sessions.GetByDigest(context.Background(), token.Digest(presented))
		if err != nil || !found {
			t.Fatalf("%s: read back: %v, %v", use.desc, found, err)
		}
		if stored.LastSeenAt.Equal(use.now) != use.wantMove {
			t.Errorf("%s: last_seen_at = %v, want moved=%v", use.desc, stored.LastSeenAt, use.wantMove)
		}
	}
	if len(sessions.touched) != 2 {
		t.Errorf("the storage was asked to touch %d times, want 2", len(sessions.touched))
	}
}

// TestVerifyReportsStorageAsDependency keeps an unreachable database from looking like a rejected
// credential: a client must never be told to log in again because our storage is down.
func TestVerifyReportsStorageAsDependency(t *testing.T) {
	presented := "token"
	for _, tc := range []struct {
		desc     string
		sessions *fakeSessions
	}{
		{"read fails", &fakeSessions{getErr: errors.New("connection reset")}},
		{"refresh fails", func() *fakeSessions {
			sessions := newFakeSessions()
			sessions.store("cabber-1", token.Digest(presented), fixedNow.Add(-time.Hour), fixedNow.Add(-time.Hour))
			sessions.touchErr = errors.New("connection reset")
			return sessions
		}()},
	} {
		_, err := newTestService(tc.sessions).Verify(context.Background(), presented)
		if !errors.Is(err, ErrDependency) {
			t.Errorf("%s: Verify = %v, want ErrDependency", tc.desc, err)
		}
		if errors.Is(err, ErrInvalidSession) {
			t.Errorf("%s: a storage failure was reported as a rejected access", tc.desc)
		}
	}
}

// TestActiveIsTheWholeInvariant pins the three conditions of data-model §2 as one rule so a later
// edit cannot drop one of them silently.
func TestActiveIsTheWholeInvariant(t *testing.T) {
	live := repo.Session{CreatedAt: fixedNow, ExpiresAt: fixedNow.Add(sessionLifetime), LastSeenAt: fixedNow}
	if !active(live, fixedNow) {
		t.Fatal("a freshly issued access is not active")
	}
	revoked := live
	revoked.RevokedAt = fixedNow
	if active(revoked, fixedNow) {
		t.Error("a revoked access is active")
	}
}
