package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Keane81/Cabby/backend/auth/internal/password"
	"github.com/Keane81/Cabby/backend/auth/internal/repo"
	"github.com/Keane81/Cabby/backend/auth/internal/token"
)

// decoyHash is the PHC string a sign-in is checked against when no account owns the address. It
// costs exactly what password.Default costs, so the answer time of an unknown address and of a
// wrong password are the same measurement (SC-006). The address that could ever match it does not
// exist, and the value is not a credential.
const decoyHash = "$argon2id$v=19$m=19456,t=2,p=1$GgJa8tpwCm0//z0qlOBdkg$VFFXu+0Nudxelnlkvbb8ez9wR2Df4Mj/H1z1dLEw8kg"

// Session is a confirmed access: the credential handed to the cabber and its absolute limit. The
// token exists only here and in the response — storage keeps its digest (FR-003, R-04).
type Session struct {
	Token     string
	ExpiresAt time.Time
}

// Login checks credentials and opens a new access for the cabber who owns them.
//
// Each sign-in opens a session of its own: a cabber on two devices revokes one of them without
// touching the other (FR-014). Both failures — no such account and a wrong password — give back
// ErrInvalidSession and nothing else, so a refusal cannot tell the two apart (FR-012).
func (s *Service) Login(ctx context.Context, email, plain string) (Session, error) {
	canonicalEmail := CanonicalEmail(email)
	if err := ValidateCredentials(canonicalEmail, plain); err != nil {
		return Session{}, err
	}

	cabber, err := s.cabbers.FindByEmail(ctx, canonicalEmail)
	switch {
	case errors.Is(err, repo.ErrCabberNotFound):
		if _, verifyErr := password.Verify(ctx, decoyHash, plain); verifyErr != nil {
			return Session{}, fmt.Errorf("service: check the decoy hash: %w", verifyErr)
		}
		return Session{}, ErrInvalidSession
	case err != nil:
		return Session{}, ErrDependency
	}

	matches, err := password.Verify(ctx, cabber.PasswordHash, plain)
	if err != nil {
		return Session{}, fmt.Errorf("service: verify password: %w", err)
	}
	if !matches {
		return Session{}, ErrInvalidSession
	}

	issued, err := token.New()
	if err != nil {
		return Session{}, fmt.Errorf("service: issue a session: %w", err)
	}
	now := s.now()
	expiresAt := now.Add(sessionLifetime)
	if err := s.sessions.Create(ctx, repo.Session{
		TokenHash:  token.Digest(issued),
		CabberID:   cabber.ID,
		CreatedAt:  now,
		ExpiresAt:  expiresAt,
		LastSeenAt: now,
	}); err != nil {
		return Session{}, ErrDependency
	}

	return Session{Token: issued, ExpiresAt: expiresAt}, nil
}
