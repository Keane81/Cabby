// Package repo is the only place where the location service speaks SQL. The case layer depends
// on the interface below, so every business rule stays testable without PostgreSQL.
package repo

import (
	"context"
	"time"
)

// Location is a row of the cabber_location table: one accepted pair of coordinates of one cabber.
type Location struct {
	CabberID   string
	Latitude   float64
	Longitude  float64
	ReceivedAt time.Time
}

// LocationRepository owns the records of location. A record is added and never changed or removed
// (FR-005): the interface has no method that could do either.
type LocationRepository interface {
	// Insert stores one record. It returns only after the row is committed, so a success is never
	// reported for a record that could still be lost.
	Insert(ctx context.Context, location Location) error
}
