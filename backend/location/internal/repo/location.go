package repo

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Locations is the PostgreSQL implementation of LocationRepository.
type Locations struct {
	pool *pgxpool.Pool
}

// NewLocations binds the repository to a pool.
func NewLocations(pool *pgxpool.Pool) *Locations {
	return &Locations{pool: pool}
}

// Insert adds one immutable row. The time of receipt is the caller's value, not now(): the service
// owns the clock so a test can move it (spec 004 FR-006).
func (l *Locations) Insert(ctx context.Context, location Location) error {
	_, err := l.pool.Exec(ctx,
		`insert into cabber_location (cabber_id, latitude, longitude, received_at)
		 values ($1::uuid, $2, $3, $4)`,
		location.CabberID, location.Latitude, location.Longitude, location.ReceivedAt,
	)
	return err
}
