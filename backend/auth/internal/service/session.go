package service

import (
	"context"
	"time"

	"github.com/Keane81/Cabby/backend/auth/internal/repo"
	"github.com/Keane81/Cabby/backend/auth/internal/token"
)

// Boundaries of a confirmed access (FR-013, R-10): an absolute lifetime counted from creation
// and an idle window counted from the last confirmed use.
const (
	sessionLifetime = 96 * time.Hour
	idleLimit       = 24 * time.Hour
)

// Verify confirms the session presented as an opaque token and names its owner (FR-015). It is the
// way a protected operation learns who is asking: no argument of it can name a subject the session
// does not confirm (consequence R-03). The token itself never travels to the storage as a secret:
// only its digest is looked up.
//
// Every rejection — no such access, revoked, past the absolute limit, past the idle window —
// answers with the same ErrInvalidSession, so an answer cannot be used to tell the states apart
// (FR-016, SC-003).
func (s *Service) Verify(ctx context.Context, presented string) (string, error) {
	now := s.now()
	session, found, err := s.sessions.GetByDigest(ctx, token.Digest(presented))
	if err != nil {
		return "", ErrDependency
	}
	if !found || !active(session, now) {
		return "", ErrInvalidSession
	}
	// Touch keeps the reporting window of R-10 and skips a revoked row; either way the session
	// stays valid for this request.
	if _, err := s.sessions.Touch(ctx, session.ID, now); err != nil {
		return "", ErrDependency
	}
	return session.CabberID, nil
}

// active is the single invariant of data-model §2, read with the injected clock. Both limits are
// strict: a session is dead at the moment it reaches them.
func active(session repo.Session, now time.Time) bool {
	return session.RevokedAt.IsZero() &&
		session.ExpiresAt.After(now) &&
		session.LastSeenAt.Add(idleLimit).After(now)
}
