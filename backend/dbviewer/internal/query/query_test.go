package query

import (
	"errors"
	"strings"
	"testing"

	"github.com/Keane81/Cabby/backend/dbviewer/internal/catalog"
)

func testTable() catalog.Table {
	return catalog.Table{
		Name:       "cabber",
		PrimaryKey: []string{"id"},
		Columns: []catalog.Column{
			catalog.Classify("id", "uuid"),
			catalog.Classify("name", "text"),
			catalog.Classify("email", "text"),
			catalog.Classify("password_hash", "text"),
			catalog.Classify("age", "integer"),
			catalog.Classify("created_at", "timestamp with time zone"),
			catalog.Classify("active", "boolean"),
			catalog.Classify("blob", "bytea"),
		},
	}
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var qe *Error
	if !errors.As(err, &qe) || qe.Code != code {
		t.Fatalf("error = %v, want code %s", err, code)
	}
}

func TestPageStableOrderAndLimit(t *testing.T) {
	b, err := Page(testTable(), Params{Page: 3, PageSize: 50, Sort: "name", Dir: "desc"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.SQL, `ORDER BY "name" DESC NULLS LAST, "id" ASC`) {
		t.Errorf("order not stable: %s", b.SQL)
	}
	if !strings.HasSuffix(b.SQL, "LIMIT 50 OFFSET 100") {
		t.Errorf("paging wrong: %s", b.SQL)
	}
	if !strings.Contains(b.SQL, `FROM "public"."cabber"`) {
		t.Errorf("table not qualified: %s", b.SQL)
	}
}

func TestPageDefaultOrderIsPrimaryKey(t *testing.T) {
	b, err := Page(testTable(), Params{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.SQL, `ORDER BY "id" ASC`) {
		t.Errorf("default order: %s", b.SQL)
	}
	b, _ = Page(testTable(), Params{Page: 1, PageSize: 10, Sort: "id", Dir: "desc"})
	if strings.Count(b.SQL, `"id" ASC`) != 0 || !strings.Contains(b.SQL, `ORDER BY "id" DESC NULLS LAST`) {
		t.Errorf("duplicate tie-break: %s", b.SQL)
	}
}

func TestPageNeverReadsSensitiveColumns(t *testing.T) {
	b, err := Page(testTable(), Params{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{`"password_hash"`, `"blob"`} {
		if strings.Contains(strings.ReplaceAll(b.SQL, "AS "+name, ""), name) {
			t.Errorf("%s is read: %s", name, b.SQL)
		}
	}
	if !strings.Contains(b.SQL, `NULL::text AS "password_hash"`) {
		t.Errorf("sensitive column not replaced by a constant: %s", b.SQL)
	}
}

func TestPageRejectsBadPaging(t *testing.T) {
	for _, p := range []Params{{Page: 0, PageSize: 10}, {Page: 1, PageSize: 0}, {Page: 1, PageSize: MaxPageSize + 1}} {
		_, err := Page(testTable(), p)
		wantCode(t, err, CodeInvalidPage)
	}
}

func TestSortRejectsSensitiveAndUnknown(t *testing.T) {
	for _, col := range []string{"password_hash", "blob", "nope"} {
		_, err := Page(testTable(), Params{Page: 1, PageSize: 10, Sort: col})
		wantCode(t, err, CodeInvalidSort)
	}
	_, err := Page(testTable(), Params{Page: 1, PageSize: 10, Sort: "name", Dir: "sideways"})
	wantCode(t, err, CodeInvalidSort)
}

func TestParseFilter(t *testing.T) {
	f, err := ParseFilter("created_at:gte:2026-10-03T10:00:00Z")
	if err != nil || f.Column != "created_at" || f.Op != "gte" || f.Value != "2026-10-03T10:00:00Z" {
		t.Fatalf("ParseFilter = %+v, %v", f, err)
	}
	if f, err = ParseFilter("name:is_null"); err != nil || f.Op != "is_null" {
		t.Fatalf("ParseFilter without value = %+v, %v", f, err)
	}
	for _, bad := range []string{"", "name", ":eq:x"} {
		_, err := ParseFilter(bad)
		wantCode(t, err, CodeInvalidFilter)
	}
}

func TestFiltersAreParametersAndCombineWithAnd(t *testing.T) {
	evil := `'; drop table cabber;--`
	b, err := Page(testTable(), Params{Page: 1, PageSize: 10, Q: evil, Filters: []Filter{
		{Column: "name", Op: "eq", Value: evil},
		{Column: "age", Op: "gte", Value: "18"},
		{Column: "active", Op: "eq", Value: "true"},
		{Column: "id", Op: "eq", Value: "7b0e9f0a-1111-4222-8333-444455556666"},
		{Column: "created_at", Op: "lte", Value: "2026-10-03"},
		{Column: "email", Op: "contains", Value: "50%_off"},
		{Column: "name", Op: "is_null"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.SQL, "drop table") {
		t.Fatalf("a value reached the statement text: %s", b.SQL)
	}
	if strings.Count(b.SQL, " AND ") != 7 {
		t.Errorf("filters not joined by AND: %s", b.SQL)
	}
	if !strings.Contains(b.SQL, `"id"::text ILIKE $1`) || !strings.Contains(b.SQL, `"name"::text ILIKE $1`) {
		t.Errorf("search should cover text and uuid columns with one parameter: %s", b.SQL)
	}
	if strings.Contains(b.SQL, `"password_hash"::text`) || strings.Contains(b.SQL, `"blob"::text`) {
		t.Errorf("search covers a sensitive column: %s", b.SQL)
	}
	if got := b.Args[0]; got != `%'; drop table cabber;--%` {
		t.Errorf("search argument = %v", got)
	}
	var like string
	for _, a := range b.Args {
		if s, ok := a.(string); ok && strings.Contains(s, "off") {
			like = s
		}
	}
	if like != `%50\%\_off%` {
		t.Errorf("LIKE metacharacters not escaped: %q", like)
	}
}

func TestFilterRejections(t *testing.T) {
	bad := []Filter{
		{Column: "password_hash", Op: "eq", Value: "x"},
		{Column: "blob", Op: "is_null"},
		{Column: "nope", Op: "eq", Value: "x"},
		{Column: "age", Op: "eq", Value: "abc"},
		{Column: "age", Op: "eq", Value: "1.5"},
		{Column: "age", Op: "contains", Value: "1"},
		{Column: "name", Op: "gte", Value: "a"},
		{Column: "name", Op: "like", Value: "a"},
		{Column: "id", Op: "eq", Value: "not-a-uuid"},
		{Column: "active", Op: "eq", Value: "maybe"},
		{Column: "created_at", Op: "gte", Value: "yesterday"},
	}
	for _, f := range bad {
		_, err := Page(testTable(), Params{Page: 1, PageSize: 10, Filters: []Filter{f}})
		wantCode(t, err, CodeInvalidFilter)
	}
	_, err := Page(testTable(), Params{Page: 1, PageSize: 10, Q: strings.Repeat("a", MaxSearchLength+1)})
	wantCode(t, err, CodeInvalidFilter)
}

func TestCountCappedStopsEarly(t *testing.T) {
	b, err := CountCapped(testTable(), Params{Q: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.SQL, "LIMIT 10001") || !strings.HasPrefix(b.SQL, "SELECT count(*) FROM (SELECT 1 FROM") {
		t.Errorf("capped count: %s", b.SQL)
	}
	if CountAll(testTable()).SQL != `SELECT count(*) FROM "public"."cabber"` {
		t.Errorf("CountAll: %s", CountAll(testTable()).SQL)
	}
}

func TestIdentifiersAreQuoted(t *testing.T) {
	tbl := catalog.Table{Name: `we"ird`, Columns: []catalog.Column{catalog.Classify(`co"l`, "text")}}
	b, err := Page(tbl, Params{Page: 1, PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.SQL, `"we""ird"`) || !strings.Contains(b.SQL, `"co""l"`) {
		t.Errorf("identifiers not escaped: %s", b.SQL)
	}
}
