//go:build integration

// Requires a reachable PostgreSQL: `CABBY_LOCATION_DB_URL=postgres://... make -C backend/location
// test-integration`. Without the variable every case skips itself so `make test` stays green.
package migrations_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/location/migrations"
	"github.com/Keane81/Cabby/backend/platform/migrate"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestUpAppliesOnceAndSecondRunIsNoOp(t *testing.T) {
	ctx := context.Background()
	pool, schema := testPool(ctx, t)
	migrator := migrate.New(migrations.LockKey)
	scripts, err := migrate.Load(migrations.FS)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := migrator.Up(ctx, pool, scripts); err != nil {
		t.Fatalf("first Up: %v", err)
	}
	for _, table := range []string{"cabber_location", "schema_migration"} {
		if !tableExists(ctx, t, pool, schema, table) {
			t.Fatalf("table %q missing after Up", table)
		}
	}
	if got := appliedVersions(ctx, t, pool); got != 1 {
		t.Fatalf("applied rows after first Up = %d, want 1", got)
	}

	if err := migrator.Up(ctx, pool, scripts); err != nil {
		t.Fatalf("second Up: %v", err)
	}
	if got := appliedVersions(ctx, t, pool); got != 1 {
		t.Fatalf("applied rows after second Up = %d, want 1 (re-run must be a no-op)", got)
	}
}

func TestDownRestoresEmptySchema(t *testing.T) {
	ctx := context.Background()
	pool, schema := testPool(ctx, t)
	migrator := migrate.New(migrations.LockKey)
	scripts, err := migrate.Load(migrations.FS)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := migrator.Up(ctx, pool, scripts); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if err := migrator.Down(ctx, pool, scripts); err != nil {
		t.Fatalf("Down: %v", err)
	}
	for _, table := range []string{"cabber_location"} {
		if tableExists(ctx, t, pool, schema, table) {
			t.Fatalf("table %q survived Down", table)
		}
	}
	if got := appliedVersions(ctx, t, pool); got != 0 {
		t.Fatalf("applied rows after Down = %d, want 0", got)
	}
}

// testPool returns a pool whose session works inside a private schema, so parallel runs of
// the same migration do not observe each other's objects.
func testPool(ctx context.Context, t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	dsn := os.Getenv("CABBY_LOCATION_DB_URL")
	if dsn == "" {
		t.Skip("CABBY_LOCATION_DB_URL is not set")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse CABBY_LOCATION_DB_URL: %v", err)
	}
	schema := fmt.Sprintf("migrate_test_%d", time.Now().UnixNano())
	config.ConnConfig.RuntimeParams["search_path"] = schema

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx,
		"create schema "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("create schema %s: %v", schema, err)
	}
	t.Cleanup(func() {
		_, err := pool.Exec(context.Background(),
			"drop schema if exists "+pgx.Identifier{schema}.Sanitize()+" cascade")
		if err != nil {
			t.Errorf("drop schema %s: %v", schema, err)
		}
	})
	return pool, schema
}

func tableExists(ctx context.Context, t *testing.T, pool *pgxpool.Pool, schema, table string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx,
		`select exists (
			select 1 from information_schema.tables
			where table_schema = $1 and table_name = $2
		)`, schema, table).Scan(&exists); err != nil {
		t.Fatalf("check table %q: %v", table, err)
	}
	return exists
}

func appliedVersions(ctx context.Context, t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `select count(*) from schema_migration`).Scan(&count); err != nil {
		t.Fatalf("count applied migrations: %v", err)
	}
	return count
}
