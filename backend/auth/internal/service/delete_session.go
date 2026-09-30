package service

import (
	"context"

	"github.com/Keane81/Cabby/backend/auth/internal/token"
)

// DeleteSession revokes the session the request was carried out under and nothing else (FR-019). A cabber
// signed in on two devices keeps the other access working (FR-014, US3 scenario 4).
//
// The revoke decides the answer rather than the check before it: a session another request revoked
// in the meantime leaves this one with nothing to do, and calling that a success would report a
// change that never happened (FR-021). Such a request is refused exactly like any request without a
// confirmed access, and changes no state (SC-007).
func (s *Service) DeleteSession(ctx context.Context, presented string) error {
	owner, err := s.Verify(ctx, presented)
	if err != nil {
		return err
	}
	revoked, err := s.sessions.Revoke(ctx, token.Digest(presented), s.now())
	if err != nil {
		return ErrDependency
	}
	if !revoked {
		return ErrInvalidSession
	}
	s.logger.Info().Str("operation", "delete_session").Str("cabber_id", owner).Msg("session revoked")
	return nil
}
