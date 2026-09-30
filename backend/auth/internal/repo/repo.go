// Package repo is the only place where the auth service speaks SQL. The case layer depends
// on the interfaces below, so every business rule stays testable without PostgreSQL.
package repo

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors of the storage layer.
var (
	// ErrEmailTaken reports a cabber with the same canonical email already exists.
	ErrEmailTaken = errors.New("repo: email is already taken")
	// ErrCabberNotFound reports an email that no account owns.
	ErrCabberNotFound = errors.New("repo: cabber not found")
)

// Cabber is a row of the cabber table.
type Cabber struct {
	ID           string
	Name         string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

// Session is a row of the cabber_session table. RevokedAt is the zero time while the session
// is live; the table stores NULL there.
type Session struct {
	ID         string
	TokenHash  []byte
	CabberID   string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
	RevokedAt  time.Time
}

// CabberRepository owns account records. v1 never updates or deletes a cabber (FR-026, FR-028).
type CabberRepository interface {
	// Create inserts an account and returns the identifier assigned by the database.
	Create(ctx context.Context, cabber Cabber) (string, error)
	// FindByEmail looks a cabber up by its canonical email.
	FindByEmail(ctx context.Context, email string) (Cabber, error)
}

// SessionRepository owns confirmed access records.
type SessionRepository interface {
	// Create stores a freshly issued access.
	Create(ctx context.Context, session Session) error
	// GetByDigest reads the row holding the SHA-256 digest of a presented access.
	GetByDigest(ctx context.Context, digest []byte) (Session, bool, error)
	// Touch moves last_seen_at forward, but only if the stored value is older than the idle
	// reporting window; the result reports whether a row was written.
	Touch(ctx context.Context, id string, seenAt time.Time) (bool, error)
	// Revoke stamps revoked_at on a still-active access. A false result means the session was
	// already revoked or unknown, which keeps a second logout from reporting success (FR-021).
	Revoke(ctx context.Context, digest []byte, revokedAt time.Time) (bool, error)
	// PurgeExpired deletes sessions whose absolute limit or revocation lies before the cutoff
	// (R-10) and reports how many rows went. A live access never matches, and no statement of the
	// storage deletes an account: cabber rows are kept for the lifetime of the system (FR-028).
	PurgeExpired(ctx context.Context, before time.Time) (int64, error)
}
