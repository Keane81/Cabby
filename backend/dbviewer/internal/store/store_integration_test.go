//go:build integration

package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Keane81/Cabby/backend/dbviewer/internal/itest"
	"github.com/Keane81/Cabby/backend/dbviewer/internal/query"
)

func newStore(t *testing.T, timeout time.Duration) *Store {
	t.Helper()
	pool := itest.Pool(t, "auth")
	itest.Seed(t, pool)
	return New([]Source{{Name: "auth", Pool: pool}, {Name: "location", Pool: itest.Pool(t, "location")}}, timeout)
}

func idOf(t *testing.T, row []any) string {
	t.Helper()
	id, ok := row[0].(string)
	if !ok {
		t.Fatalf("id cell = %#v", row[0])
	}
	return id
}

func TestPagesHaveNoGapsOrRepeats(t *testing.T) {
	s := newStore(t, DefaultQueryTimeout)
	// age has five distinct values, so only the primary-key tie-break keeps pages disjoint.
	for _, sort := range []string{"", "age", "created_at"} {
		seen := map[string]bool{}
		for page := 1; ; page++ {
			res, err := s.Rows(context.Background(), "auth", "dbv_it_cabber", query.Params{Page: page, PageSize: 70, Sort: sort, Dir: "desc"})
			if err != nil {
				t.Fatal(err)
			}
			if page == 1 && (res.Total.Value != itest.Rows || res.Total.Kind != "exact") {
				t.Fatalf("total = %+v", res.Total)
			}
			if len(res.Rows) == 0 {
				break
			}
			for _, row := range res.Rows {
				id := idOf(t, row)
				if seen[id] {
					t.Fatalf("sort=%q: row %s on two pages", sort, id)
				}
				seen[id] = true
			}
		}
		if len(seen) != itest.Rows {
			t.Fatalf("sort=%q: %d rows seen, want %d", sort, len(seen), itest.Rows)
		}
	}
}

func TestEmptyTable(t *testing.T) {
	s := newStore(t, DefaultQueryTimeout)
	res, err := s.Rows(context.Background(), "auth", "dbv_it_empty", query.Params{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 0 || res.Total.Value != 0 || len(res.Columns) != 2 {
		t.Fatalf("empty table page = %+v", res)
	}
}

func TestUnknownTableAndDatabase(t *testing.T) {
	s := newStore(t, DefaultQueryTimeout)
	for _, c := range [][2]string{{"auth", "nope"}, {"nope", "dbv_it_cabber"}, {"auth", "pg_class"}} {
		if _, err := s.Rows(context.Background(), c[0], c[1], query.Params{Page: 1, PageSize: 5}); !errors.Is(err, ErrUnknownTable) {
			t.Errorf("Rows(%v) error = %v, want ErrUnknownTable", c, err)
		}
	}
}

func TestSensitiveCellsAreMaskedAndNullsKept(t *testing.T) {
	s := newStore(t, DefaultQueryTimeout)
	res, err := s.Rows(context.Background(), "auth", "dbv_it_cabber", query.Params{Page: 1, PageSize: 20, Sort: "age"})
	if err != nil {
		t.Fatal(err)
	}
	var hashCol, noteCol = -1, -1
	for i, c := range res.Columns {
		switch c {
		case "password_hash":
			hashCol = i
		case "note":
			noteCol = i
		}
	}
	sawNull := false
	for _, row := range res.Rows {
		if m, ok := row[hashCol].(Masked); !ok || !m.Masked {
			t.Fatalf("password_hash cell = %#v", row[hashCol])
		}
		if row[noteCol] == nil {
			sawNull = true
		}
	}
	if !sawNull {
		t.Error("no NULL note on the first page; NULL must stay nil")
	}
}

func TestLargeTableOpensFastAndCountsAsEstimate(t *testing.T) {
	pool := itest.Pool(t, "auth")
	itest.Seed(t, pool)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `create table dbv_it_big (id bigint generated always as identity primary key, v double precision not null, label text not null)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into dbv_it_big (v, label) select random(), 'row ' || n from generate_series(1, 300000) n`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `analyze dbv_it_big`); err != nil {
		t.Fatal(err)
	}
	s := New([]Source{{Name: "auth", Pool: pool}}, DefaultQueryTimeout)

	started := time.Now()
	res, err := s.Rows(ctx, "auth", "dbv_it_big", query.Params{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(started); took > 2*time.Second {
		t.Errorf("first page took %s, want ≤ 2s (SC-002)", took)
	}
	if res.Total.Kind != "estimate" || res.Total.Value < 250000 || len(res.Rows) != 50 {
		t.Errorf("big table page: total=%+v rows=%d", res.Total, len(res.Rows))
	}

	started = time.Now()
	res, err = s.Rows(ctx, "auth", "dbv_it_big", query.Params{Page: 3, PageSize: 50, Sort: "v", Dir: "desc", Q: "row 12"})
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("search+sort took %s, want ≤ 5s (SC-003)", took)
	}
	if res.Total.Kind != "atLeast" || res.Total.Value != query.CountCap {
		t.Errorf("capped total = %+v", res.Total)
	}
}

func TestUnavailableDatabaseDoesNotAffectTheOther(t *testing.T) {
	pool := itest.Pool(t, "auth")
	itest.Seed(t, pool)
	dead, err := NewPool(context.Background(), "postgres://x:y@127.0.0.1:1/none?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dead.Close)
	s := New([]Source{{Name: "auth", Pool: dead}, {Name: "location", Pool: pool}}, DefaultQueryTimeout)

	dbs := s.Databases(context.Background(), true)
	if dbs[0].Available || dbs[0].Error == "" {
		t.Errorf("dead database = %+v", dbs[0])
	}
	if !dbs[1].Available || len(dbs[1].Tables) == 0 {
		t.Errorf("live database = %+v", dbs[1])
	}
	if _, err := s.Rows(context.Background(), "auth", "dbv_it_cabber", query.Params{Page: 1, PageSize: 5}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Rows on dead database error = %v", err)
	}
	if _, err := s.Rows(context.Background(), "location", "dbv_it_cabber", query.Params{Page: 1, PageSize: 5}); err != nil {
		t.Errorf("Rows on live database error = %v", err)
	}
}

func TestWritesAreRejectedInReadOnlyTransaction(t *testing.T) {
	s := newStore(t, DefaultQueryTimeout)
	d := s.find("auth")
	for _, stmt := range []string{
		`insert into dbv_it_empty (v) values ('x')`,
		`delete from dbv_it_cabber`,
		`update dbv_it_cabber set name = 'x'`,
		`drop table dbv_it_empty`,
		`create table dbv_it_new (id int)`,
	} {
		err := s.read(context.Background(), d, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), stmt)
			return err
		})
		if !errors.Is(err, ErrQuery) {
			t.Errorf("%q: error = %v, want a rejected statement", stmt, err)
		}
	}
	var n int
	if err := itest.Pool(t, "auth").QueryRow(context.Background(), `select count(*) from dbv_it_cabber`).Scan(&n); err != nil || n != itest.Rows {
		t.Errorf("rows after attempted writes = %d, %v", n, err)
	}
}

func TestStatementTimeout(t *testing.T) {
	s := newStore(t, 150*time.Millisecond)
	d := s.find("auth")
	err := s.read(context.Background(), d, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `select pg_sleep(2)`)
		return err
	})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("error = %v, want ErrTimeout", err)
	}
}

func TestSearchFiltersAndSort(t *testing.T) {
	s := newStore(t, DefaultQueryTimeout)
	ctx := context.Background()

	res, err := s.Rows(ctx, "auth", "dbv_it_cabber", query.Params{Page: 1, PageSize: 50, Q: "user42@"})
	if err != nil || res.Total.Value != 1 || res.Total.Kind != "exact" {
		t.Fatalf("search by email part: %+v, %v", res.Total, err)
	}
	// Search matches the uuid as text too.
	id := idOf(t, res.Rows[0])
	if res, err = s.Rows(ctx, "auth", "dbv_it_cabber", query.Params{Page: 1, PageSize: 50, Q: id[:13]}); err != nil || res.Total.Value != 1 {
		t.Fatalf("search by uuid part: %+v, %v", res.Total, err)
	}
	// LIKE metacharacters are literal.
	if res, err = s.Rows(ctx, "auth", "dbv_it_cabber", query.Params{Page: 1, PageSize: 50, Q: "%"}); err != nil || res.Total.Value != 0 {
		t.Fatalf("search for %%: %+v, %v", res.Total, err)
	}

	// age = 18 (n%5==0) and active (n even) and created in January: n in 1..744 hours.
	filters := []query.Filter{
		{Column: "age", Op: "eq", Value: "18"},
		{Column: "active", Op: "eq", Value: "true"},
		{Column: "created_at", Op: "gte", Value: "2026-01-01T00:00:00Z"},
		{Column: "created_at", Op: "lte", Value: "2026-01-31T23:59:59Z"},
	}
	want := 0
	for n := 1; n <= 1000; n++ {
		if n%5 == 0 && n%2 == 0 && n <= 744 {
			want++
		}
	}
	res, err = s.Rows(ctx, "auth", "dbv_it_cabber", query.Params{Page: 1, PageSize: 200, Filters: filters})
	if err != nil || res.Total.Value != int64(want) {
		t.Fatalf("combined filters: total %+v want %d, err %v", res.Total, want, err)
	}

	res, err = s.Rows(ctx, "auth", "dbv_it_cabber", query.Params{Page: 1, PageSize: 200, Filters: []query.Filter{{Column: "note", Op: "is_null"}}})
	if err != nil || res.Total.Value != 100 {
		t.Fatalf("is_null: %+v, %v", res.Total, err)
	}

	// Sort covers the whole table, not the page: the first row ascending is the oldest, descending the newest.
	first := func(dir string) string {
		r, err := s.Rows(ctx, "auth", "dbv_it_cabber", query.Params{Page: 1, PageSize: 1, Sort: "created_at", Dir: dir})
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprint(r.Rows[0][1])
	}
	if got := first("asc"); got != "Cabber 1" {
		t.Errorf("oldest = %s", got)
	}
	if got := first("desc"); got != "Cabber 1000" {
		t.Errorf("newest = %s", got)
	}

	// Conditions that cannot be satisfied are reported as a request error, not a database failure.
	_, err = s.Rows(ctx, "auth", "dbv_it_cabber", query.Params{Page: 1, PageSize: 5, Filters: []query.Filter{{Column: "password_hash", Op: "eq", Value: "x"}}})
	var qe *query.Error
	if !errors.As(err, &qe) {
		t.Errorf("filter on sensitive column error = %v", err)
	}
}

func TestSizesAgreeWithPostgres(t *testing.T) {
	pool := itest.Pool(t, "auth")
	itest.Seed(t, pool)
	s := New([]Source{{Name: "auth", Pool: pool}}, DefaultQueryTimeout)
	ctx := context.Background()

	var direct int64
	if err := pool.QueryRow(ctx, `select pg_database_size(current_database())`).Scan(&direct); err != nil {
		t.Fatal(err)
	}
	db := s.Databases(ctx, true)[0]
	if diff := float64(db.SizeBytes-direct) / float64(direct); diff > 0.05 || diff < -0.05 {
		t.Errorf("database size %d vs %d (SC-004)", db.SizeBytes, direct)
	}
	var table int64
	if err := pool.QueryRow(ctx, `select pg_total_relation_size('dbv_it_cabber')`).Scan(&table); err != nil {
		t.Fatal(err)
	}
	var seen bool
	for _, tb := range db.Tables {
		if tb.Name == "dbv_it_cabber" {
			seen = true
			if tb.TotalBytes != table || tb.DataBytes <= 0 || tb.IndexBytes <= 0 || tb.DataBytes+tb.IndexBytes > tb.TotalBytes {
				t.Errorf("table sizes = %+v, direct total %d", tb, table)
			}
			if tb.EstimatedRows != itest.Rows || !tb.Exact {
				t.Errorf("rows = %d exact=%v", tb.EstimatedRows, tb.Exact)
			}
		}
	}
	if !seen {
		t.Fatal("dbv_it_cabber is missing from the schema")
	}

	if _, err := pool.Exec(ctx, `insert into dbv_it_cabber (name, email, password_hash, age, created_at, active)
		select 'x' || n, repeat('e', 100) || n, 'h', 1, now(), true from generate_series(1, 20000) n`); err != nil {
		t.Fatal(err)
	}
	if grown := s.Databases(ctx, false)[0]; grown.SizeBytes <= db.SizeBytes {
		t.Errorf("size did not grow: %d → %d", db.SizeBytes, grown.SizeBytes)
	}
}

func TestDataIsUnchangedByEveryOperation(t *testing.T) {
	pool := itest.Pool(t, "auth")
	itest.Seed(t, pool)
	s := New([]Source{{Name: "auth", Pool: pool}}, DefaultQueryTimeout)
	ctx := context.Background()
	checksum := func() string {
		var sum string
		err := pool.QueryRow(ctx, `select md5((select string_agg(c::text, '|' order by c.id) from dbv_it_cabber c)
			|| (select string_agg(x::text, '|' order by x.id) from dbv_it_session x))`).Scan(&sum)
		if err != nil {
			t.Fatal(err)
		}
		return sum
	}
	before := checksum()
	s.Databases(ctx, true)
	for _, p := range []query.Params{
		{Page: 1, PageSize: 50},
		{Page: 2, PageSize: 10, Sort: "email", Dir: "desc"},
		{Page: 1, PageSize: 50, Q: "user"},
		{Page: 1, PageSize: 50, Filters: []query.Filter{{Column: "age", Op: "gte", Value: "20"}}},
	} {
		if _, err := s.Rows(ctx, "auth", "dbv_it_cabber", p); err != nil {
			t.Fatal(err)
		}
	}
	if after := checksum(); after != before {
		t.Error("table contents changed")
	}
}
