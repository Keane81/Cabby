package repo

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// idleReportWindow bounds how often an active access may write last_seen_at (R-10): one row
// per minute per cabber instead of one per request.
const idleReportWindow = 60 * time.Second

// Sessions stores confirmed access records in PostgreSQL. Only the digest of a presented
// access is written, so the table never yields a usable credential.
type Sessions struct {
	pool *pgxpool.Pool
}

func NewSessions(pool *pgxpool.Pool) *Sessions {
	return &Sessions{pool: pool}
}

// Create stores a freshly issued access. Timestamps come from the injected service clock, so
// tests with a substituted Now produce the same expiry as production.
func (s *Sessions) Create(ctx context.Context, session Session) error {
	_, err := s.pool.Exec(ctx,
		`insert into cabber_session (token_hash, cabber_id, created_at, expires_at, last_seen_at)
		 values ($1, $2::uuid, $3, $4, $5)`,
		session.TokenHash, session.CabberID, session.CreatedAt, session.ExpiresAt, session.LastSeenAt,
	)
	return err
}

func (s *Sessions) GetByDigest(ctx context.Context, digest []byte) (Session, bool, error) {
	var (
		found     Session
		revokedAt *time.Time
	)
	err := s.pool.QueryRow(ctx,
		`select id::text, token_hash, cabber_id::text, created_at, expires_at, last_seen_at, revoked_at
		   from cabber_session
		  where token_hash = $1`,
		digest,
	).Scan(&found.ID, &found.TokenHash, &found.CabberID,
		&found.CreatedAt, &found.ExpiresAt, &found.LastSeenAt, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	if revokedAt != nil {
		found.RevokedAt = *revokedAt
	}
	return found, true, nil
}

// Touch moves last_seen_at forward only past the reporting window. A revoked access is never
// refreshed, which keeps a revoke racing with a request from being undone.
func (s *Sessions) Touch(ctx context.Context, id string, seenAt time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`update cabber_session
		    set last_seen_at = $2
		  where id = $1::uuid
		    and revoked_at is null
		    and last_seen_at < $3`,
		id, seenAt, seenAt.Add(-idleReportWindow),
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// Revoke stamps revoked_at on a live access. The condition makes a second revoke of the same
// token report "nothing revoked" instead of a false success (FR-021).
func (s *Sessions) Revoke(ctx context.Context, digest []byte, revokedAt time.Time) (bool, error) {
	var id string
	err := s.pool.QueryRow(ctx,
		`update cabber_session
		    set revoked_at = $2
		  where token_hash = $1
		    and revoked_at is null
		  returning id::text`,
		digest, revokedAt,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// PurgeExpired removes the session rows that have been dead since before the cutoff: past their
// absolute limit, or revoked past it. A live access cannot match either branch, and no account row
// is named by this statement at all (FR-028, R-10).
func (s *Sessions) PurgeExpired(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`delete from cabber_session
		  where expires_at < $1
		     or (revoked_at is not null and revoked_at < $1)`,
		before,
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
