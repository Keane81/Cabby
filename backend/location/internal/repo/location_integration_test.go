//go:build integration

// Requires a reachable PostgreSQL: `CABBY_LOCATION_DB_URL=postgres://... make -C backend/location
// test-integration`. Without the variable every case skips itself so `make test` stays green.
package repo_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/location/internal/migrate"
	"github.com/Keane81/Cabby/backend/location/internal/repo"
	"github.com/Keane81/Cabby/backend/location/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const cabberA = "0b6f1e1c-6a2c-4c3e-9d7a-0a1b2c3d4e5f"

// testPool returns a pool working inside a private schema with the real migrations applied, so
// runs do not observe each other's rows.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("CABBY_LOCATION_DB_URL")
	if dsn == "" {
		t.Skip("CABBY_LOCATION_DB_URL is not set")
	}
	ctx := context.Background()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse CABBY_LOCATION_DB_URL: %v", err)
	}
	schema := fmt.Sprintf("repo_test_%d", time.Now().UnixNano())
	config.ConnConfig.RuntimeParams["search_path"] = schema

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "create schema "+quoted); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "drop schema if exists "+quoted+" cascade"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
	})
	scripts, err := migrate.Load(migrations.FS)
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := migrate.Up(ctx, pool, scripts); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return pool
}

type row struct {
	id                  int64
	cabber              string
	latitude, longitude float64
	receivedAt          time.Time
}

func rows(t *testing.T, pool *pgxpool.Pool) []row {
	t.Helper()
	found, err := pool.Query(context.Background(),
		`select id, cabber_id::text, latitude, longitude, received_at from cabber_location order by received_at, id`)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	defer found.Close()
	var all []row
	for found.Next() {
		var r row
		if err := found.Scan(&r.id, &r.cabber, &r.latitude, &r.longitude, &r.receivedAt); err != nil {
			t.Fatalf("scan: %v", err)
		}
		all = append(all, r)
	}
	if err := found.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return all
}

// TestInsertsAddRowsAndChangeNoneOfThePrevious is FR-005 and SC-002 at the storage: N inserts give N
// rows, equal positions included, and the earlier ones are untouched.
func TestInsertsAddRowsAndChangeNoneOfThePrevious(t *testing.T) {
	pool := testPool(t)
	locations := repo.NewLocations(pool)
	moment := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

	for index := range 5 {
		latitude := 55.0
		if index == 4 {
			latitude = 56.0
		}
		if err := locations.Insert(context.Background(), repo.Location{
			CabberID: cabberA, Latitude: latitude, Longitude: 37.0, ReceivedAt: moment.Add(time.Duration(index) * time.Second),
		}); err != nil {
			t.Fatalf("Insert %d: %v", index, err)
		}
		got := rows(t, pool)
		if len(got) != index+1 {
			t.Fatalf("after insert %d: %d rows, want %d", index, len(got), index+1)
		}
		for previous := range index {
			if got[previous].latitude != 55.0 || !got[previous].receivedAt.Equal(moment.Add(time.Duration(previous)*time.Second)) {
				t.Fatalf("insert %d changed row %d: %+v", index, previous, got[previous])
			}
		}
	}
}

// TestRowsOrderByReceivedAtThenIDAndKeepTheTimeAsGiven covers data-model §1 invariant 2: the pair
// (received_at, id) orders a cabber's rows and the stored time is the one the service gave.
func TestRowsOrderByReceivedAtThenIDAndKeepTheTimeAsGiven(t *testing.T) {
	pool := testPool(t)
	locations := repo.NewLocations(pool)
	moment := time.Date(2026, 10, 1, 9, 0, 0, 123456000, time.UTC)

	for _, latitude := range []float64{1, 2, 3} {
		if err := locations.Insert(context.Background(), repo.Location{
			CabberID: cabberA, Latitude: latitude, Longitude: 0, ReceivedAt: moment,
		}); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}
	got := rows(t, pool)
	if len(got) != 3 {
		t.Fatalf("%d rows, want 3", len(got))
	}
	for index, want := range []float64{1, 2, 3} {
		if got[index].latitude != want {
			t.Errorf("row %d has latitude %v, want %v (equal times are told apart by id)", index, got[index].latitude, want)
		}
		if !got[index].receivedAt.Equal(moment) {
			t.Errorf("row %d received_at = %v, want %v", index, got[index].receivedAt, moment)
		}
	}
}

// TestTheDatabaseRejectsWhatTheServiceShouldNeverSend is the second line of defence of data-model §1
// invariant 3: the check constraints hold even if a defect lets a value past the service.
func TestTheDatabaseRejectsWhatTheServiceShouldNeverSend(t *testing.T) {
	pool := testPool(t)
	locations := repo.NewLocations(pool)

	for _, tc := range []struct {
		desc     string
		lat, lon float64
	}{
		{"latitude over", 90.0000001, 0},
		{"latitude under", -90.0000001, 0},
		{"longitude over", 0, 180.0000001},
		{"longitude under", 0, -180.0000001},
	} {
		err := locations.Insert(context.Background(), repo.Location{
			CabberID: cabberA, Latitude: tc.lat, Longitude: tc.lon, ReceivedAt: time.Now(),
		})
		if err == nil {
			t.Errorf("%s: the database accepted the row", tc.desc)
		}
	}
	if err := locations.Insert(context.Background(), repo.Location{
		CabberID: cabberA, Latitude: 90, Longitude: -180, ReceivedAt: time.Now(),
	}); err != nil {
		t.Errorf("the bounds themselves are valid: %v", err)
	}
	if got := len(rows(t, pool)); got != 1 {
		t.Fatalf("%d rows, want only the valid one", got)
	}
}

// TestConcurrentInsertsOfOneCabberKeepBothRows is the edge case of two devices sending at once: both
// records are stored whole, and no value of one is mixed with a value of the other.
func TestConcurrentInsertsOfOneCabberKeepBothRows(t *testing.T) {
	pool := testPool(t)
	locations := repo.NewLocations(pool)
	moment := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

	var wg sync.WaitGroup
	for _, pair := range [][2]float64{{10, 20}, {30, 40}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := locations.Insert(context.Background(), repo.Location{
				CabberID: cabberA, Latitude: pair[0], Longitude: pair[1], ReceivedAt: moment,
			}); err != nil {
				t.Errorf("Insert: %v", err)
			}
		}()
	}
	wg.Wait()

	got := rows(t, pool)
	if len(got) != 2 {
		t.Fatalf("%d rows, want 2", len(got))
	}
	for _, r := range got {
		if !(r.latitude == 10 && r.longitude == 20) && !(r.latitude == 30 && r.longitude == 40) {
			t.Errorf("row mixes two requests: %+v", r)
		}
	}
}

// TestACommittedRowSurvivesAReconnect is the storage half of FR-010: what Insert reported as done is
// there for another connection to read.
func TestACommittedRowSurvivesAReconnect(t *testing.T) {
	pool := testPool(t)
	if err := repo.NewLocations(pool).Insert(context.Background(), repo.Location{
		CabberID: cabberA, Latitude: 1, Longitude: 2, ReceivedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	pool.Reset()
	if got := len(rows(t, pool)); got != 1 {
		t.Fatalf("%d rows after the connections were reset, want 1", got)
	}
}
