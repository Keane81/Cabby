package grpcserver

import (
	"context"
	"strconv"
	"time"

	"github.com/Keane81/Cabby/backend/auth/internal/password"
	"github.com/Keane81/Cabby/backend/auth/internal/repo"
)

// cheap keeps every derivation of a transport test at the smallest cost the parser accepts; R-07
// bounds the parameters of production accounts, not of the fixtures.
var cheap = password.Params{Memory: 16, Time: 1, Threads: 1, KeyLen: 32, SaltLen: 16}

// The two stand-ins below exist because the service fakes of internal/service are unexported: a
// transport test has to drive storage into each state the status mapping answers. They keep the
// rules the transport observes and no business rule of their own.

// memoryCabbers is a repo.CabberRepository over a map keyed by canonical email.
type memoryCabbers struct {
	rows      map[string]repo.Cabber
	nextID    int
	createErr error
	findErr   error
}

func newMemoryCabbers() *memoryCabbers {
	return &memoryCabbers{rows: map[string]repo.Cabber{}}
}

func (m *memoryCabbers) Create(_ context.Context, cabber repo.Cabber) (string, error) {
	if m.createErr != nil {
		return "", m.createErr
	}
	if _, taken := m.rows[cabber.Email]; taken {
		return "", repo.ErrEmailTaken
	}
	m.nextID++
	id := "cabber-" + strconv.Itoa(m.nextID)
	cabber.ID = id
	m.rows[cabber.Email] = cabber
	return id, nil
}

func (m *memoryCabbers) FindByEmail(_ context.Context, email string) (repo.Cabber, error) {
	if m.findErr != nil {
		return repo.Cabber{}, m.findErr
	}
	cabber, found := m.rows[email]
	if !found {
		return repo.Cabber{}, repo.ErrCabberNotFound
	}
	return cabber, nil
}

// memorySessions is a repo.SessionRepository keyed by the digest of a session.
type memorySessions struct {
	stored    map[string]repo.Session
	created   []repo.Session
	createErr error
	revokeErr error
	getErr    error
}

func newMemorySessions() *memorySessions {
	return &memorySessions{stored: map[string]repo.Session{}}
}

func (m *memorySessions) Create(_ context.Context, session repo.Session) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.created = append(m.created, session)
	m.stored[string(session.TokenHash)] = session
	return nil
}

func (m *memorySessions) GetByDigest(_ context.Context, digest []byte) (repo.Session, bool, error) {
	if m.getErr != nil {
		return repo.Session{}, false, m.getErr
	}
	session, found := m.stored[string(digest)]
	return session, found, nil
}

func (m *memorySessions) Touch(_ context.Context, id string, seenAt time.Time) (bool, error) {
	for digest, session := range m.stored {
		if session.ID != id || !session.RevokedAt.IsZero() || seenAt.Sub(session.LastSeenAt) < time.Minute {
			continue
		}
		session.LastSeenAt = seenAt
		m.stored[digest] = session
		return true, nil
	}
	return false, nil
}

func (m *memorySessions) Revoke(_ context.Context, digest []byte, revokedAt time.Time) (bool, error) {
	if m.revokeErr != nil {
		return false, m.revokeErr
	}
	session, found := m.stored[string(digest)]
	if !found || !session.RevokedAt.IsZero() {
		return false, nil
	}
	session.RevokedAt = revokedAt
	m.stored[string(digest)] = session
	return true, nil
}

// PurgeExpired honours the delete of repo.Sessions: only a session dead since before the cutoff
// leaves the map (R-10).
func (m *memorySessions) PurgeExpired(_ context.Context, before time.Time) (int64, error) {
	var deleted int64
	for digest, session := range m.stored {
		expired := session.ExpiresAt.Before(before)
		revoked := !session.RevokedAt.IsZero() && session.RevokedAt.Before(before)
		if !expired && !revoked {
			continue
		}
		delete(m.stored, digest)
		deleted++
	}
	return deleted, nil
}
