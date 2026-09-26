// Package service holds the case logic of the auth service: registration, entry into the
// system and revocation of an access. It owns the business rules and never the SQL, which
// stays behind repo, nor the transport shape, which stays in grpcserver.
package service

import (
	"time"

	"github.com/Keane81/Cabby/backend/auth/internal/password"
	"github.com/Keane81/Cabby/backend/auth/internal/repo"
	"github.com/rs/zerolog"
)

// Limits are the input boundaries of FR-006 and research R-08. Lengths count Unicode
// characters, never bytes.
const (
	MinNameLength     = 1
	MaxNameLength     = 64
	MinPasswordLength = 4
	MaxPasswordLength = 16
	MaxEmailLength    = 254
)

// Now is the injected clock. Every rule that reads time — both access limits and the
// reporting window — goes through it, so boundaries are tested with a substituted value
// (research R-10).
type Now func() time.Time

// Service carries the dependencies shared by all cases.
type Service struct {
	cabbers  repo.CabberRepository
	sessions repo.SessionRepository
	params   password.Params
	logger   zerolog.Logger

	// now is a field rather than a private copy of a constructor argument so a test can move the
	// clock without rebuilding the service (R-10).
	now Now
}

// New builds the service. params is password.Default in production and a cheap value in
// tests; now is time.Now in production.
func New(
	cabbers repo.CabberRepository,
	sessions repo.SessionRepository,
	params password.Params,
	logger zerolog.Logger,
	now Now,
) *Service {
	return &Service{
		cabbers:  cabbers,
		sessions: sessions,
		params:   params,
		logger:   logger,
		now:      now,
	}
}
