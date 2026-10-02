// Package service holds the case logic of the location service: the check of coordinates and the
// addition of a record. It owns the business rules and never the SQL, which stays behind repo, nor
// the transport shape, which stays in grpcserver.
package service

import (
	"time"

	"github.com/Keane81/Cabby/backend/location/internal/repo"
	"github.com/rs/zerolog"
)

// Now is the injected clock: the time of receipt of a record goes through it, so a test moves it
// instead of waiting (spec 004 FR-006).
type Now func() time.Time

// Service carries the dependencies of the cases.
type Service struct {
	locations repo.LocationRepository
	logger    zerolog.Logger
	now       Now
}

// New builds the service; now is time.Now in production.
func New(locations repo.LocationRepository, logger zerolog.Logger, now Now) *Service {
	return &Service{locations: locations, logger: logger, now: now}
}
