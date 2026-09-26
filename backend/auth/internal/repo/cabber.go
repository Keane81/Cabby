package repo

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Cabbers stores cabber records in PostgreSQL.
type Cabbers struct {
	pool *pgxpool.Pool
}

func NewCabbers(pool *pgxpool.Pool) *Cabbers {
	return &Cabbers{pool: pool}
}

// Create inserts an account. The unique index on email, not a prior read, is what resolves
// two racing registrations of the same address (data-model §7).
func (c *Cabbers) Create(ctx context.Context, cabber Cabber) (string, error) {
	var id string
	err := c.pool.QueryRow(ctx,
		`insert into cabber (name, email, password_hash)
		 values ($1, $2, $3)
		 returning id::text`,
		cabber.Name, cabber.Email, cabber.PasswordHash,
	).Scan(&id)
	if err != nil {
		if isEmailConflict(err) {
			return "", ErrEmailTaken
		}
		return "", err
	}
	return id, nil
}

// FindByEmail returns the account owning the canonical email or ErrCabberNotFound.
func (c *Cabbers) FindByEmail(ctx context.Context, email string) (Cabber, error) {
	var found Cabber
	err := c.pool.QueryRow(ctx,
		`select id::text, name, email, password_hash, created_at
		   from cabber
		  where email = $1`,
		email,
	).Scan(&found.ID, &found.Name, &found.Email, &found.PasswordHash, &found.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Cabber{}, ErrCabberNotFound
	}
	if err != nil {
		return Cabber{}, err
	}
	return found, nil
}

// isEmailConflict reports the unique violation of cabber_email_key. A check constraint or a
// different unique index would surface as an ordinary error, so the mapping stays narrow.
func isEmailConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "cabber_email_key"
}
