//go:build integration

// Package itest is the shared setup of the integration tests: a connection to a throwaway
// PostgreSQL and a few seeded tables named dbv_it_*. It is compiled only with the integration tag.
package itest

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Secret values seeded into sensitive columns; tests search every response for them.
const (
	SecretHash  = "SECRET-PASSWORD-HASH-9f3a"
	SecretToken = "SECRET-TOKEN-HASH-77b1"
	Rows        = 1000
)

// DSN returns the DSN of the database named by name ("auth" or "location"); the location address
// falls back to the auth one so a single throwaway database is enough. The test is skipped when no
// DSN is given.
func DSN(t *testing.T, name string) string {
	t.Helper()
	auth := os.Getenv("CABBY_DBVIEWER_AUTH_DB_URL")
	if auth == "" {
		t.Skip("CABBY_DBVIEWER_AUTH_DB_URL is not set")
	}
	if name == "location" {
		if loc := os.Getenv("CABBY_DBVIEWER_LOCATION_DB_URL"); loc != "" {
			return loc
		}
	}
	return auth
}

// Pool opens a pool to the named database, closed when the test ends.
func Pool(t *testing.T, name string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), DSN(t, name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// Seed creates dbv_it_cabber (1000 rows, a secret password_hash), dbv_it_session (a secret
// bytea token_hash) and dbv_it_empty, and drops them when the test ends.
func Seed(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	drop := `drop table if exists dbv_it_cabber, dbv_it_session, dbv_it_empty, dbv_it_big`
	if _, err := pool.Exec(ctx, drop); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), drop) })
	setup := []string{
		`create table dbv_it_cabber (
			id uuid primary key default gen_random_uuid(),
			name text not null,
			email text not null,
			password_hash text not null,
			age integer not null,
			created_at timestamptz not null,
			active boolean not null,
			note text
		)`,
		`insert into dbv_it_cabber (name, email, password_hash, age, created_at, active, note)
		 select 'Cabber ' || n, 'user' || n || '@example.com', '` + SecretHash + `',
		        18 + (n % 5), timestamptz '2026-01-01 00:00:00+00' + (n || ' hours')::interval, n % 2 = 0,
		        case when n % 10 = 0 then null else 'note ' || n end
		 from generate_series(1, ` + "1000" + `) n`,
		`create table dbv_it_session (
			id uuid primary key default gen_random_uuid(),
			token_hash bytea not null,
			expires_at timestamptz not null
		)`,
		`insert into dbv_it_session (token_hash, expires_at)
		 select convert_to('` + SecretToken + `', 'UTF8'), now() + (n || ' minutes')::interval from generate_series(1, 25) n`,
		`create table dbv_it_empty (id bigint generated always as identity primary key, v text)`,
		`analyze dbv_it_cabber, dbv_it_session, dbv_it_empty`,
	}
	for _, stmt := range setup {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
}
