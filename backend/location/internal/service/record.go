package service

import (
	"context"
	"regexp"
	"time"

	"github.com/Keane81/Cabby/backend/location/internal/repo"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Record adds one record of the position of a cabber and returns the time it was received
// (FR-001, FR-005, FR-006). A rejected request leaves no trace in storage (FR-004). Nothing here
// limits how often a cabber may call (FR-007), and no coordinate is logged (FR-011).
func (s *Service) Record(ctx context.Context, cabberID string, latitude, longitude float64) (time.Time, error) {
	if !uuidPattern.MatchString(cabberID) {
		return time.Time{}, ErrInvalidOwner
	}
	latitude, longitude, err := Validate(latitude, longitude)
	if err != nil {
		return time.Time{}, err
	}
	// PostgreSQL keeps microseconds; the answer must equal what is stored.
	receivedAt := s.now().UTC().Truncate(time.Microsecond)
	if err := s.locations.Insert(ctx, repo.Location{
		CabberID: cabberID, Latitude: latitude, Longitude: longitude, ReceivedAt: receivedAt,
	}); err != nil {
		return time.Time{}, ErrDependency
	}
	s.logger.Info().Str("operation", "record_location").Str("cabber_id", cabberID).Msg("location recorded")
	return receivedAt, nil
}
