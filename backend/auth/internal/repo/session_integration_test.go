//go:build integration

package repo_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/auth/internal/repo"
	"github.com/jackc/pgx/v5/pgxpool"
)

// owner creates the account a session row hangs off; the email is unique per run so repeated
// calls do not collide.
func owner(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	id, err := repo.NewCabbers(pool).Create(ctx, repo.Cabber{
		Name:         "Иван",
		Email:        fmt.Sprintf("session-%d@example.com", time.Now().UnixNano()),
		PasswordHash: fakeHash("1234"),
	})
	if err != nil {
		t.Fatalf("Create cabber: %v", err)
	}
	return id
}

// sessionFor returns a live access of owner with the given digest.
func sessionFor(cabberID string, digest []byte) repo.Session {
	created := now()
	return repo.Session{
		TokenHash:  digest,
		CabberID:   cabberID,
		CreatedAt:  created,
		ExpiresAt:  created.Add(96 * time.Hour),
		LastSeenAt: created,
	}
}

// TestRevokingOneAccessLeavesTheOtherAlone covers the independence invariant of data-model §2.
func TestRevokingOneAccessLeavesTheOtherAlone(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	cabbers := repo.NewCabbers(pool)
	sessions := repo.NewSessions(pool)

	id, err := cabbers.Create(ctx, repo.Cabber{
		Name: "Иван", Email: "two-sessions@example.com", PasswordHash: fakeHash("1234"),
	})
	if err != nil {
		t.Fatalf("Create cabber: %v", err)
	}
	created := now()
	first, second := []byte("first-digest"), []byte("second-digest")
	for _, digest := range [][]byte{first, second} {
		if err := sessions.Create(ctx, repo.Session{
			TokenHash: digest, CabberID: id, CreatedAt: created,
			ExpiresAt: created.Add(96 * time.Hour), LastSeenAt: created,
		}); err != nil {
			t.Fatalf("Create session: %v", err)
		}
	}

	revokedAt := created.Add(time.Hour)
	if _, err := sessions.Revoke(ctx, first, revokedAt); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	revoked, found, err := sessions.GetByDigest(ctx, first)
	if err != nil || !found {
		t.Fatalf("GetByDigest(first) = %v, %v, want a row", found, err)
	}
	if !revoked.RevokedAt.Equal(revokedAt) {
		t.Fatalf("revoked_at = %v, want %v", revoked.RevokedAt, revokedAt)
	}
	untouched, found, err := sessions.GetByDigest(ctx, second)
	if err != nil || !found {
		t.Fatalf("GetByDigest(second) = %v, %v, want a row", found, err)
	}
	if !untouched.RevokedAt.IsZero() {
		t.Fatalf("the second access was revoked as well: %v", untouched.RevokedAt)
	}
}

// TestSecondRevokeReportsNothing covers FR-021 at the SQL level: no rows, therefore no
// success a client could mistake for a fresh session deletion.
func TestSecondRevokeReportsNothing(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	sessions := repo.NewSessions(pool)
	digest := []byte("repeated-revoke-digest")
	if err := sessions.Create(ctx, sessionFor(owner(t, ctx, pool), digest)); err != nil {
		t.Fatalf("Create session: %v", err)
	}

	first, err := sessions.Revoke(ctx, digest, now())
	if err != nil || !first {
		t.Fatalf("first Revoke = %v, %v, want true", first, err)
	}

	stored, _, err := sessions.GetByDigest(ctx, digest)
	if err != nil {
		t.Fatalf("GetByDigest: %v", err)
	}
	later, err := sessions.Revoke(ctx, digest, stored.RevokedAt.Add(time.Hour))
	if err != nil || later {
		t.Fatalf("second Revoke = %v, %v, want false", later, err)
	}
	after, _, err := sessions.GetByDigest(ctx, digest)
	if err != nil {
		t.Fatalf("GetByDigest after second Revoke: %v", err)
	}
	if !after.RevokedAt.Equal(stored.RevokedAt) {
		t.Fatalf("repeated revoke moved revoked_at from %v to %v", stored.RevokedAt, after.RevokedAt)
	}

	if revoked, err := sessions.Revoke(ctx, []byte("never-issued-digest"), now()); err != nil || revoked {
		t.Fatalf("Revoke of an unknown digest = %v, %v, want false and no error", revoked, err)
	}
}

// TestPurgeTakesOnlyAccessesDeadPastTheRetention is the delete of R-10 as PostgreSQL runs it: an
// access past its absolute limit and one revoked both for longer than the retention leave the table,
// and the account row they hang off stays (FR-028). The SQL is the only place the rule that no
// cabber row is ever deleted can be checked.
func TestPurgeTakesOnlyAccessesDeadPastTheRetention(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	sessions := repo.NewSessions(pool)
	cabberID := owner(t, ctx, pool)

	before := now()
	retention := 7 * 24 * time.Hour
	// sessionFor writes a session of its own moment, so the rows below name their own timestamps.
	insert := func(digest []byte, created time.Time) {
		if err := sessions.Create(ctx, repo.Session{
			TokenHash: digest, CabberID: cabberID, CreatedAt: created,
			ExpiresAt: created.Add(96 * time.Hour), LastSeenAt: created,
		}); err != nil {
			t.Fatalf("Create session for %s: %v", digest, err)
		}
	}
	expiredLongAgo := []byte("purge-expired-long-ago")
	expiredRecently := []byte("purge-expired-yesterday")
	revokedLongAgo := []byte("purge-revoked-long-ago")
	alive := []byte("purge-still-valid")
	insert(expiredLongAgo, before.Add(-retention-97*time.Hour))
	insert(expiredRecently, before.Add(-97*time.Hour))
	insert(revokedLongAgo, before.Add(-95*time.Hour))
	insert(alive, before.Add(-95*time.Hour))
	if _, err := sessions.Revoke(ctx, revokedLongAgo, before.Add(-retention-time.Hour)); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	deleted, err := sessions.PurgeExpired(ctx, before.Add(-retention))
	if err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if deleted != 2 {
		t.Errorf("purged %d rows, want the two dead past the retention", deleted)
	}
	for _, digest := range [][]byte{expiredLongAgo, revokedLongAgo} {
		if _, found, err := sessions.GetByDigest(ctx, digest); err != nil || found {
			t.Errorf("%s survived the purge: found=%v err=%v", digest, found, err)
		}
	}
	for _, digest := range [][]byte{expiredRecently, alive} {
		if _, found, err := sessions.GetByDigest(ctx, digest); err != nil || !found {
			t.Errorf("%s left although it is inside the retention or still live: found=%v err=%v", digest, found, err)
		}
	}

	var accountKept bool
	if err := pool.QueryRow(ctx,
		`select exists (select 1 from cabber where id = $1::uuid)`, cabberID).Scan(&accountKept); err != nil {
		t.Fatalf("read the account back: %v", err)
	}
	if !accountKept {
		t.Error("the purge deleted the account its access rows hang off")
	}
}

func TestTouchHonoursTheReportingWindow(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	sessions := repo.NewSessions(pool)
	cabberID := owner(t, ctx, pool)
	created := now()
	session := sessionFor(cabberID, []byte("touch-window-digest"))
	session.CreatedAt = created
	session.ExpiresAt = created.Add(96 * time.Hour)
	session.LastSeenAt = created
	if err := sessions.Create(ctx, session); err != nil {
		t.Fatalf("Create session: %v", err)
	}
	stored, _, err := sessions.GetByDigest(ctx, session.TokenHash)
	if err != nil {
		t.Fatalf("GetByDigest: %v", err)
	}

	// Inside the 60-second window: no write at all (R-10).
	written, err := sessions.Touch(ctx, stored.ID, stored.LastSeenAt.Add(30*time.Second))
	if err != nil {
		t.Fatalf("Touch inside the window: %v", err)
	}
	if written {
		t.Fatal("Touch wrote a row inside the reporting window")
	}
	unchanged, _, err := sessions.GetByDigest(ctx, session.TokenHash)
	if err != nil {
		t.Fatalf("GetByDigest: %v", err)
	}
	if !unchanged.LastSeenAt.Equal(stored.LastSeenAt) {
		t.Fatalf("last_seen_at moved inside the window: %v → %v", stored.LastSeenAt, unchanged.LastSeenAt)
	}

	// Past the window: exactly one write.
	seenAt := stored.LastSeenAt.Add(61 * time.Second)
	written, err = sessions.Touch(ctx, stored.ID, seenAt)
	if err != nil {
		t.Fatalf("Touch past the window: %v", err)
	}
	if !written {
		t.Fatal("Touch past the reporting window wrote nothing")
	}
	refreshed, _, err := sessions.GetByDigest(ctx, session.TokenHash)
	if err != nil {
		t.Fatalf("GetByDigest: %v", err)
	}
	if !refreshed.LastSeenAt.Equal(seenAt) {
		t.Fatalf("last_seen_at = %v, want %v", refreshed.LastSeenAt, seenAt)
	}
}

// TestTouchSkipsRevokedAccess keeps a revoke racing with an in-flight request from being undone.
func TestTouchSkipsRevokedAccess(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	sessions := repo.NewSessions(pool)
	session := sessionFor(owner(t, ctx, pool), []byte("revoked-touch-digest"))
	if err := sessions.Create(ctx, session); err != nil {
		t.Fatalf("Create session: %v", err)
	}
	if _, err := sessions.Revoke(ctx, session.TokenHash, now()); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	stored, _, err := sessions.GetByDigest(ctx, session.TokenHash)
	if err != nil {
		t.Fatalf("GetByDigest: %v", err)
	}
	written, err := sessions.Touch(ctx, stored.ID, stored.LastSeenAt.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("Touch of a revoked access: %v", err)
	}
	if written {
		t.Fatal("Touch refreshed last_seen_at on a revoked access")
	}
}
