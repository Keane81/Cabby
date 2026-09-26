//go:build integration

package repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/auth/internal/repo"
)

// TestLogoutFlowTouchesOnlyThePresentedAccess runs the statements of a logout in the order the case
// issues them — read, report, revoke — against PostgreSQL, and then reads the two rows the way the
// next request would. Only the presented access changes state: the other one of the same cabber is
// still live, still unread as revoked, and reports its own timestamps (FR-014, FR-019).
func TestLogoutFlowTouchesOnlyThePresentedAccess(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	sessions := repo.NewSessions(pool)
	cabberID := owner(t, ctx, pool)

	presented := []byte("logout-flow-presented")
	alongside := []byte("logout-flow-alongside")
	for _, digest := range [][]byte{presented, alongside} {
		if err := sessions.Create(ctx, sessionFor(cabberID, digest)); err != nil {
			t.Fatalf("Create session: %v", err)
		}
	}
	before, found, err := sessions.GetByDigest(ctx, alongside)
	if err != nil || !found {
		t.Fatalf("GetByDigest(alongside) = %v, %v, want a row", found, err)
	}

	live, found, err := sessions.GetByDigest(ctx, presented)
	if err != nil || !found {
		t.Fatalf("GetByDigest(presented) = %v, %v, want a row", found, err)
	}
	if _, err := sessions.Touch(ctx, live.ID, live.LastSeenAt.Add(61*time.Second)); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	revokedAt := now()
	revoked, err := sessions.Revoke(ctx, presented, revokedAt)
	if err != nil || !revoked {
		t.Fatalf("Revoke(presented) = %v, %v, want true", revoked, err)
	}

	after, found, err := sessions.GetByDigest(ctx, presented)
	if err != nil || !found {
		t.Fatalf("a revoked access stopped being readable: %v, %v", found, err)
	}
	if after.ID != live.ID {
		t.Errorf("the revoke landed on %s, issued for %s", after.ID, live.ID)
	}
	if !after.RevokedAt.Equal(revokedAt) {
		t.Errorf("revoked_at = %v, want %v", after.RevokedAt, revokedAt)
	}

	// A second logout reads the same row and revokes nothing: FR-021 forbids the false success a
	// client could mistake for a fresh exit.
	again, err := sessions.Revoke(ctx, presented, revokedAt.Add(time.Minute))
	if err != nil || again {
		t.Errorf("second Revoke(presented) = %v, %v, want false and no error", again, err)
	}
	kept, _, err := sessions.GetByDigest(ctx, presented)
	if err != nil {
		t.Fatalf("GetByDigest(presented): %v", err)
	}
	if !kept.RevokedAt.Equal(revokedAt) {
		t.Errorf("the second revoke moved revoked_at to %v, want %v", kept.RevokedAt, revokedAt)
	}

	untouched, found, err := sessions.GetByDigest(ctx, alongside)
	if err != nil || !found {
		t.Fatalf("GetByDigest(alongside) = %v, %v, want a row", found, err)
	}
	if !untouched.RevokedAt.IsZero() {
		t.Errorf("the access alongside was revoked too: %v", untouched.RevokedAt)
	}
	if !untouched.LastSeenAt.Equal(before.LastSeenAt) {
		t.Errorf("the access alongside was written: %v → %v", before.LastSeenAt, untouched.LastSeenAt)
	}
}
