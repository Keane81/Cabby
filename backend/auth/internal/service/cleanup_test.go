package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// deadSince is how long ago a session has to have died for the retention to let it go; the rows
// below are placed relative to it so a change of the constant moves the fixtures with it.
var deadSince = sessionRetention + 24*time.Hour

func TestPurgeTakesOnlyAccessesDeadPastTheRetention(t *testing.T) {
	ctx := context.Background()
	cabbers, sessions := newFakeCabbers(), newFakeSessions()
	moment := fixedNow
	service := New(cabbers, sessions, cheap, zerolog.Nop(), func() time.Time { return moment })

	account, err := service.Register(ctx, testName, testEmail, testPlain)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	owner := account.ID

	// One row per branch of the retention rule, plus the two it must not reach.
	longExpired := []byte("digest-expired-long-ago")
	sessions.store(owner, longExpired, moment.Add(-deadSince).Add(-sessionLifetime), moment)
	recentlyExpired := []byte("digest-expired-yesterday")
	sessions.store(owner, recentlyExpired, moment.Add(-24*time.Hour-sessionLifetime), moment)
	longRevoked := []byte("digest-revoked-long-ago")
	revoked := sessions.store(owner, longRevoked, moment.Add(-24*time.Hour), moment)
	revoked.RevokedAt = moment.Add(-deadSince)
	sessions.stored[string(longRevoked)] = revoked
	alive := []byte("digest-still-valid")
	sessions.store(owner, alive, moment.Add(-24*time.Hour), moment)

	deleted, err := service.PurgeDeadSessions(ctx)
	if err != nil {
		t.Fatalf("PurgeDeadSessions: %v", err)
	}
	if deleted != 2 {
		t.Errorf("purged %d rows, want the two dead past the retention", deleted)
	}
	for _, digest := range [][]byte{longExpired, longRevoked} {
		if _, found := sessions.stored[string(digest)]; found {
			t.Errorf("%s stayed, although it died %s ago", string(digest), deadSince)
		}
	}
	for _, digest := range [][]byte{recentlyExpired, alive} {
		if _, found := sessions.stored[string(digest)]; !found {
			t.Errorf("%s was purged too early", string(digest))
		}
	}
	// The cutoff is the injected clock minus the retention, which is the whole reason the case
	// takes its time from there (R-10).
	if cutoff := sessions.purged[0]; !cutoff.Equal(moment.Add(-sessionRetention)) {
		t.Errorf("cutoff = %s, want %s", cutoff, moment.Add(-sessionRetention))
	}
	// FR-028: an account outlives every access of its owner. The storage of a cabber has no delete
	// at all, so the only thing a purge could do to a row of cabber is leave it.
	if len(cabbers.stored) != 1 {
		t.Errorf("accounts left = %d, want the one registered", len(cabbers.stored))
	}

	// The same row leaves once the clock carries it past the retention: the purge is a rule about
	// now, not about the rows it saw last time.
	moment = moment.Add(7 * 24 * time.Hour)
	deleted, err = service.PurgeDeadSessions(ctx)
	if err != nil {
		t.Fatalf("second PurgeDeadSessions: %v", err)
	}
	if deleted != 1 {
		t.Errorf("purged %d rows on the moved clock, want the session that aged past the retention", deleted)
	}
	if _, found := sessions.stored[string(alive)]; !found {
		t.Error("the live access left with the dead ones")
	}
}

func TestPurgeReportsStorageAsDependency(t *testing.T) {
	cabbers, sessions := newFakeCabbers(), newFakeSessions()
	service := newCaseService(cabbers, sessions)
	sessions.purgeErr = errors.New("auth: delete cabber_session: conn lost")

	deleted, err := service.PurgeDeadSessions(context.Background())
	if !errors.Is(err, ErrDependency) {
		t.Fatalf("PurgeDeadSessions = %v, want ErrDependency", err)
	}
	if deleted != 0 {
		t.Errorf("a failed purge reported %d rows gone", deleted)
	}
	if errors.Is(err, ErrInvalidSession) {
		t.Error("a storage failure was reported as a rejection")
	}
}

func TestRunCleanupPurgesOnEveryTickAndStops(t *testing.T) {
	cabbers, sessions := newFakeCabbers(), newFakeSessions()
	service := New(cabbers, sessions, cheap, zerolog.Nop(), func() time.Time { return fixedNow })
	sessions.store("cabber-1", []byte("digest-for-the-ticker"),
		fixedNow.Add(-deadSince).Add(-sessionLifetime), fixedNow)

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		service.RunCleanup(ctx, 10*time.Millisecond)
	}()

	waitForPurges(t, sessions.purgeCalls, 2)
	cancel()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("RunCleanup kept running after its context was cancelled")
	}
	if _, found := sessions.stored["digest-for-the-ticker"]; found {
		t.Error("the ticker never took the row a direct call would take")
	}
	// Nothing runs behind the loop: a goroutine that outlived its context would keep writing.
	after := sessions.purgeCalls()
	time.Sleep(50 * time.Millisecond)
	if extra := sessions.purgeCalls() - after; extra != 0 {
		t.Errorf("the cleanup purged %d more times after the cancel", extra)
	}
}

// waitForPurges waits until the loop has been through the storage the given number of times.
func waitForPurges(t *testing.T, counted func() int, atLeast int) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		if counted() >= atLeast {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("the cleanup reached the storage %d times, want at least %d", counted(), atLeast)
}
