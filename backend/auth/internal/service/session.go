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
	accessLifetime = 96 * time.Hour
	idleLimit      = 24 * time.Hour
)

// Verify confirms the access presented as an opaque token and names its owner (FR-015). The
// token itself never travels to the storage as a secret: only its digest is looked up.
//
// Every rejection — no such access, revoked, past the absolute limit, past the idle window —
// answers with the same ErrInvalidAccess, so an answer cannot be used to tell the states apart
// (FR-016, SC-003).
func (s *Service) Verify(ctx context.Context, presented string) (string, error) {
	now := s.now()
	session, found, err := s.sessions.GetByDigest(ctx, token.Digest(presented))
	if err != nil {
		return "", ErrDependency
	}
	if !found || !active(session, now) {
		return "", ErrInvalidAccess
	}
	// Touch keeps the reporting window of R-10 and skips a revoked row; either way the access
	// stays valid for this request.
	if _, err := s.sessions.Touch(ctx, session.ID, now); err != nil {
		return "", ErrDependency
	}
	return session.CabberID, nil
}

// RequireCabber is the way a protected operation learns who is asking (FR-015). It is the only
// entry to Verify: a case takes the access from the request and gets back the account that owns
// it, so no argument of it can name a subject the access does not confirm (consequence R-03).
// A missing, expired, idle, revoked or foreign access answers with the same ErrInvalidAccess,
// which says nothing about any account (FR-016, FR-018).
func (s *Service) RequireCabber(ctx context.Context, presented string) (string, error) {
	return s.Verify(ctx, presented)
}

// active is the single invariant of data-model §2, read with the injected clock. Both limits are
// strict: an access is dead at the moment it reaches them.
func active(session repo.Session, now time.Time) bool {
	return session.RevokedAt.IsZero() &&
		session.ExpiresAt.After(now) &&
		session.LastSeenAt.Add(idleLimit).After(now)
}
