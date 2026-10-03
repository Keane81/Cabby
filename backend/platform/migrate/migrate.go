// Package migrate applies the SQL pairs embedded in the binary to PostgreSQL. Every run
// holds a transaction-scoped advisory lock, so two replicas starting at once cannot apply
// the same migration twice. It is shared by the services that own a database; each of them
// passes the lock key of its own database to New.
package migrate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const table = "schema_migration"

// Migrator applies migrations to one database under the advisory lock key it was given.
type Migrator struct {
	lockKey int64
}

// New returns a Migrator. lockKey only has to be stable and not shared with another
// pg_advisory_lock user inside the same database.
func New(lockKey int64) *Migrator {
	return &Migrator{lockKey: lockKey}
}

// Migration is one `<version>_<name>.up.sql` / `.down.sql` pair.
type Migration struct {
	Version string
	Up      string
	Down    string
}

// Load reads the pairs from fsys ordered by version. A file named *.sql that does not
// parse as a pair is an error: a typo in a file name must not silently drop a migration.
func Load(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	up := map[string]string{}
	down := map[string]string{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		version, kind, ok := parse(name)
		if !ok {
			return nil, fmt.Errorf("migration file %q is not named <version>_<name>.up.sql or .down.sql", name)
		}
		script, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		if kind == "up" {
			up[version] = string(script)
			continue
		}
		down[version] = string(script)
	}
	versions := make([]string, 0, len(up))
	for version := range up {
		versions = append(versions, version)
	}
	sort.Strings(versions)
	migrations := make([]Migration, 0, len(versions))
	for _, version := range versions {
		script, ok := down[version]
		if !ok {
			return nil, fmt.Errorf("migration %s has no down script", version)
		}
		migrations = append(migrations, Migration{Version: version, Up: up[version], Down: script})
	}
	if len(migrations) == 0 {
		return nil, errors.New("no migrations embedded")
	}
	return migrations, nil
}

func parse(name string) (version, kind string, ok bool) {
	for _, pair := range []struct{ suffix, kind string }{{".up.sql", "up"}, {".down.sql", "down"}} {
		if !strings.HasSuffix(name, pair.suffix) {
			continue
		}
		stem := strings.TrimSuffix(name, pair.suffix)
		parts := strings.SplitN(stem, "_", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", false
		}
		return parts[0], pair.kind, true
	}
	return "", "", false
}

// Up applies every migration whose version is missing from schema_migration.
func (m *Migrator) Up(ctx context.Context, pool *pgxpool.Pool, migrations []Migration) error {
	return m.withSchemaLock(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `create table if not exists `+table+` (
			version    text primary key,
			applied_at timestamptz not null default now()
		)`); err != nil {
			return fmt.Errorf("create %s: %w", table, err)
		}
		applied, err := versions(ctx, tx)
		if err != nil {
			return err
		}
		for _, migration := range migrations {
			if applied[migration.Version] {
				continue
			}
			if _, err := tx.Exec(ctx, migration.Up); err != nil {
				return fmt.Errorf("apply migration %s: %w", migration.Version, err)
			}
			if _, err := tx.Exec(ctx,
				`insert into `+table+` (version) values ($1)`, migration.Version); err != nil {
				return fmt.Errorf("record migration %s: %w", migration.Version, err)
			}
		}
		return nil
	})
}

// UpWaiting retries Up while PostgreSQL is still starting: compose brings both containers
// up at once, so the first connection attempts are expected to fail.
func (m *Migrator) UpWaiting(ctx context.Context, pool *pgxpool.Pool, migrations []Migration, attempts int, delay time.Duration) error {
	var lastErr error
	for attempt := range attempts {
		if err := m.Up(ctx, pool, migrations); err != nil {
			lastErr = err
		} else {
			return nil
		}
		if attempt+1 == attempts {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return fmt.Errorf("database not ready after %d attempts: %w", attempts, lastErr)
}

// Down reverts applied migrations newest first, which is the path the readiness check of
// the down script takes.
func (m *Migrator) Down(ctx context.Context, pool *pgxpool.Pool, migrations []Migration) error {
	return m.withSchemaLock(ctx, pool, func(tx pgx.Tx) error {
		applied, err := versions(ctx, tx)
		if err != nil {
			return err
		}
		for i := len(migrations) - 1; i >= 0; i-- {
			migration := migrations[i]
			if !applied[migration.Version] {
				continue
			}
			if _, err := tx.Exec(ctx, migration.Down); err != nil {
				return fmt.Errorf("revert migration %s: %w", migration.Version, err)
			}
			if _, err := tx.Exec(ctx,
				`delete from `+table+` where version = $1`, migration.Version); err != nil {
				return fmt.Errorf("forget migration %s: %w", migration.Version, err)
			}
		}
		return nil
	})
}

func (m *Migrator) withSchemaLock(ctx context.Context, pool *pgxpool.Pool, apply func(pgx.Tx) error) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, m.lockKey); err != nil {
		return fmt.Errorf("take advisory lock: %w", err)
	}
	if err := apply(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func versions(ctx context.Context, tx pgx.Tx) (map[string]bool, error) {
	rows, err := tx.Query(ctx, `select version from `+table)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", table, err)
	}
	defer rows.Close()

	applied := map[string]bool{}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan %s: %w", table, err)
		}
		applied[version] = true
	}
	return applied, rows.Err()
}
