package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/Keane81/Cabby/backend/auth/internal/password"
	"github.com/Keane81/Cabby/backend/auth/internal/repo"
)

// Cabber is the account a registration created. It holds no credential of its own: an account is
// active from the first second, and reaching it takes a separate sign-in (FR-010).
type Cabber struct {
	ID    string
	Email string
}

// Register creates an account for the data a cabber supplies.
//
// The values are canonicalised before they are checked, because both the limits of FR-006 and the
// constraints of 0001_cabber apply to the canonical form the account keeps. A rejected request
// stops before storage is touched, so it cannot leave a half-written account behind (FR-008).
func (s *Service) Register(ctx context.Context, name, email, plain string) (Cabber, error) {
	canonicalName := CanonicalName(name)
	canonicalEmail := CanonicalEmail(email)
	if err := ValidateRegistration(canonicalName, canonicalEmail, plain); err != nil {
		return Cabber{}, err
	}

	hash, err := password.Hash(ctx, plain, s.params)
	if err != nil {
		return Cabber{}, fmt.Errorf("service: derive password hash: %w", err)
	}

	id, err := s.cabbers.Create(ctx, repo.Cabber{
		Name:         canonicalName,
		Email:        canonicalEmail,
		PasswordHash: hash,
	})
	if err != nil {
		// The unique index is what settles two registrations racing for one address, and the
		// account that won it stays unmentioned in the refusal (FR-009).
		if errors.Is(err, repo.ErrEmailTaken) {
			return Cabber{}, ErrEmailTaken
		}
		return Cabber{}, ErrDependency
	}
	s.logger.Info().Str("operation", "register").Str("cabber_id", id).Msg("cabber registered")

	return Cabber{ID: id, Email: canonicalEmail}, nil
}
