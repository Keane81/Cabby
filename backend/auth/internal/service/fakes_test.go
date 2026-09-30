package service

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/Keane81/Cabby/backend/auth/internal/password"
	"github.com/Keane81/Cabby/backend/auth/internal/repo"
	"github.com/rs/zerolog"
)

// cheap is the least expensive set of Argon2id parameters the parser accepts. Registration and
// sign-in cases derive keys, and R-07 bounds the parameters of production accounts, not of the
// fixtures that read them back.
var cheap = password.Params{Memory: 16, Time: 1, Threads: 1, KeyLen: 32, SaltLen: 16}

// newCaseService builds the service for a case test: cheap derivations and the fixed clock of
// fixedNow, so every timestamp in a result is one the test chose.
func newCaseService(cabbers *fakeCabbers, sessions *fakeSessions) *Service {
	return New(cabbers, sessions, cheap, zerolog.Nop(), func() time.Time { return fixedNow })
}

// reportWindow mirrors the guard repo.Sessions.Touch applies to last_seen_at (R-10). The
// integration tests of the repository prove the SQL rule; the case tests only need a stand-in
// that honours it.
const reportWindow = 60 * time.Second

// fakeCabbers is the in-memory stand-in of repo.CabberRepository. It keeps the one rule the
// cases depend on: an email is claimed once.
type fakeCabbers struct {
	stored    map[string]repo.Cabber
	nextID    int
	findErr   error
	createErr error
}

func newFakeCabbers() *fakeCabbers {
	return &fakeCabbers{stored: map[string]repo.Cabber{}}
}

func (f *fakeCabbers) Create(_ context.Context, cabber repo.Cabber) (string, error) {
	if f.createErr != nil {
		return "", f.createErr
	}
	if _, taken := f.stored[cabber.Email]; taken {
		return "", repo.ErrEmailTaken
	}
	f.nextID++
	id := "cabber-" + strconv.Itoa(f.nextID)
	cabber.ID = id
	f.stored[cabber.Email] = cabber
	return id, nil
}

func (f *fakeCabbers) FindByEmail(_ context.Context, email string) (repo.Cabber, error) {
	if f.findErr != nil {
		return repo.Cabber{}, f.findErr
	}
	cabber, found := f.stored[email]
	if !found {
		return repo.Cabber{}, repo.ErrCabberNotFound
	}
	return cabber, nil
}

// fakeSessions is the in-memory stand-in of repo.SessionRepository. It records the moment each
// call reached the storage so a test can assert the injected clock travelled unchanged.
type fakeSessions struct {
	// mu guards the purge counters only: the cleanup loop is the one caller from another
	// goroutine, and every other method runs on the goroutine of its test.
	mu sync.Mutex

	stored  map[string]repo.Session
	created []repo.Session
	touched []time.Time
	revoked []time.Time
	purged  []time.Time

	getErr    error
	createErr error
	touchErr  error
	revokeErr error
	purgeErr  error
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{stored: map[string]repo.Session{}}
}

func (f *fakeSessions) Create(_ context.Context, session repo.Session) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created = append(f.created, session)
	f.stored[string(session.TokenHash)] = session
	return nil
}

func (f *fakeSessions) GetByDigest(_ context.Context, digest []byte) (repo.Session, bool, error) {
	if f.getErr != nil {
		return repo.Session{}, false, f.getErr
	}
	session, found := f.stored[string(digest)]
	return session, found, nil
}

func (f *fakeSessions) Touch(_ context.Context, id string, seenAt time.Time) (bool, error) {
	f.touched = append(f.touched, seenAt)
	if f.touchErr != nil {
		return false, f.touchErr
	}
	for digest, session := range f.stored {
		if session.ID != id {
			continue
		}
		if !session.RevokedAt.IsZero() || seenAt.Sub(session.LastSeenAt) < reportWindow {
			return false, nil
		}
		session.LastSeenAt = seenAt
		f.stored[digest] = session
		return true, nil
	}
	return false, nil
}

func (f *fakeSessions) Revoke(_ context.Context, digest []byte, revokedAt time.Time) (bool, error) {
	f.revoked = append(f.revoked, revokedAt)
	if f.revokeErr != nil {
		return false, f.revokeErr
	}
	session, found := f.stored[string(digest)]
	if !found || !session.RevokedAt.IsZero() {
		return false, nil
	}
	session.RevokedAt = revokedAt
	f.stored[string(digest)] = session
	return true, nil
}

func (f *fakeSessions) PurgeExpired(_ context.Context, before time.Time) (int64, error) {
	// The lock is for the background test of the cleanup loop, the only one that reaches this
	// method from another goroutine.
	f.mu.Lock()
	defer f.mu.Unlock()
	f.purged = append(f.purged, before)
	if f.purgeErr != nil {
		return 0, f.purgeErr
	}
	return f.purge(before), nil
}

// purgeCalls counts the visits to the purge under the lock a background test needs.
func (f *fakeSessions) purgeCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.purged)
}

// purge mirrors the delete of repo.Sessions.PurgeExpired: a session leaves the table once it has
// been past its absolute limit, or revoked, for longer than the cutoff. The integration test of the
// repository proves the SQL; the case tests only need a stand-in that honours it.
func (f *fakeSessions) purge(before time.Time) int64 {
	var deleted int64
	for digest, session := range f.stored {
		expired := session.ExpiresAt.Before(before)
		revoked := !session.RevokedAt.IsZero() && session.RevokedAt.Before(before)
		if !expired && !revoked {
			continue
		}
		delete(f.stored, digest)
		deleted++
	}
	return deleted
}

// store keeps a row of the given owner under the digest of a token, exactly as a successful
// login would have written it.
func (f *fakeSessions) store(cabberID string, digest []byte, created, seenAt time.Time) repo.Session {
	session := repo.Session{
		ID:         "session-" + strconv.Itoa(len(f.stored)+1),
		TokenHash:  digest,
		CabberID:   cabberID,
		CreatedAt:  created,
		ExpiresAt:  created.Add(sessionLifetime),
		LastSeenAt: seenAt,
	}
	f.stored[string(digest)] = session
	return session
}
