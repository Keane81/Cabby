package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rs/zerolog"

	"github.com/Keane81/Cabby/backend/dbviewer/internal/catalog"
	"github.com/Keane81/Cabby/backend/dbviewer/internal/query"
	"github.com/Keane81/Cabby/backend/dbviewer/internal/store"
)

// fakeBackend validates requests with the real query builder against a fixed table, so the
// request-error paths run end to end without a database.
type fakeBackend struct {
	table catalog.Table
	got   query.Params
}

func (f *fakeBackend) Databases(context.Context, bool) []catalog.Database {
	return []catalog.Database{{Name: "auth", Available: true, Tables: []catalog.Table{f.table}}}
}

func (f *fakeBackend) Rows(_ context.Context, db, table string, p query.Params) (store.Page, error) {
	if db != "auth" || table != f.table.Name {
		return store.Page{}, store.ErrUnknownTable
	}
	f.got = p
	if _, err := query.Page(f.table, p); err != nil {
		return store.Page{}, err
	}
	return store.Page{Columns: []string{"id"}, Rows: [][]any{}, Total: store.Total{Kind: "exact"}, Page: p.Page, PageSize: p.PageSize}, nil
}

func newTestServer(b Backend) (http.Handler, *strings.Builder) {
	logs := &strings.Builder{}
	page := fstest.MapFS{"index.html": {Data: []byte("<html>viewer</html>")}}
	return New(b, page, zerolog.New(logs)), logs
}

func testBackend() *fakeBackend {
	return &fakeBackend{table: catalog.Table{
		Name: "cabber", PrimaryKey: []string{"id"},
		Columns: []catalog.Column{
			catalog.Classify("id", "uuid"), catalog.Classify("email", "text"),
			catalog.Classify("age", "integer"), catalog.Classify("password_hash", "text"),
		},
	}}
}

func do(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct{ Code, Message string }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not an error: %v", rec.Body.String(), err)
	}
	return body.Error.Code
}

func TestRowsErrors(t *testing.T) {
	h, _ := newTestServer(testBackend())
	base := "/api/databases/auth/tables/cabber/rows"
	tests := []struct {
		name, target string
		status       int
		code         string
	}{
		{"unknown table", "/api/databases/auth/tables/nope/rows", 404, "unknown_table"},
		{"unknown database", "/api/databases/nope/tables/cabber/rows", 404, "unknown_table"},
		{"page zero", base + "?page=0", 400, "invalid_page"},
		{"page text", base + "?page=x", 400, "invalid_page"},
		{"page size too big", base + "?pageSize=201", 400, "invalid_page"},
		{"page size zero", base + "?pageSize=0", 400, "invalid_page"},
		{"filter on secret", base + "?filter=" + url.QueryEscape("password_hash:eq:x"), 400, "invalid_filter"},
		{"filter nonnumeric", base + "?filter=" + url.QueryEscape("age:eq:abc"), 400, "invalid_filter"},
		{"filter unknown op", base + "?filter=" + url.QueryEscape("age:like:1"), 400, "invalid_filter"},
		{"filter malformed", base + "?filter=age", 400, "invalid_filter"},
		{"search too long", base + "?q=" + strings.Repeat("a", 201), 400, "invalid_filter"},
		{"sort on secret", base + "?sort=password_hash", 400, "invalid_sort"},
		{"bad direction", base + "?sort=email&dir=up", 400, "invalid_sort"},
		{"unknown api path", "/api/other", 404, "not_found"},
	}
	for _, tc := range tests {
		rec := do(h, http.MethodGet, tc.target)
		if rec.Code != tc.status || errorCode(t, rec) != tc.code {
			t.Errorf("%s: status %d code %q, want %d %q", tc.name, rec.Code, errorCode(t, rec), tc.status, tc.code)
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: missing no-store", tc.name)
		}
	}
}

func TestOnlyGetIsAllowed(t *testing.T) {
	h, _ := newTestServer(testBackend())
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		for _, target := range []string{"/api/databases", "/api/databases/auth/tables/cabber/rows", "/healthz", "/"} {
			rec := do(h, method, target)
			if rec.Code != http.StatusMethodNotAllowed || errorCode(t, rec) != "method_not_allowed" {
				t.Errorf("%s %s: status %d", method, target, rec.Code)
			}
		}
	}
}

func TestRowsParsesParameters(t *testing.T) {
	b := testBackend()
	h, _ := newTestServer(b)
	target := "/api/databases/auth/tables/cabber/rows?q=ann&page=3&pageSize=25&sort=email&dir=desc" +
		"&filter=" + url.QueryEscape("age:gte:18") + "&filter=" + url.QueryEscape("email:contains:a:b")
	if rec := do(h, http.MethodGet, target); rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	want := query.Params{Q: "ann", Page: 3, PageSize: 25, Sort: "email", Dir: "desc", Filters: []query.Filter{
		{Column: "age", Op: "gte", Value: "18"}, {Column: "email", Op: "contains", Value: "a:b"},
	}}
	if b.got.Q != want.Q || b.got.Page != want.Page || b.got.PageSize != want.PageSize || b.got.Sort != want.Sort ||
		b.got.Dir != want.Dir || len(b.got.Filters) != 2 || b.got.Filters[1] != want.Filters[1] || b.got.Filters[0] != want.Filters[0] {
		t.Errorf("params = %+v, want %+v", b.got, want)
	}
	if rec := do(h, http.MethodGet, "/api/databases/auth/tables/cabber/rows"); rec.Code != 200 || b.got.Page != 1 || b.got.PageSize != query.DefaultPageSize {
		t.Errorf("defaults: status %d params %+v", rec.Code, b.got)
	}
}

func TestStaticPageAndHealth(t *testing.T) {
	h, _ := newTestServer(testBackend())
	if rec := do(h, http.MethodGet, "/"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "viewer") {
		t.Errorf("page: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, http.MethodGet, "/healthz"); rec.Code != 200 {
		t.Errorf("healthz: %d", rec.Code)
	}
}

func TestLogsCarryNoRequestValues(t *testing.T) {
	h, logs := newTestServer(testBackend())
	do(h, http.MethodGet, "/api/databases/auth/tables/cabber/rows?q=SEARCHSECRET&filter="+url.QueryEscape("email:eq:FILTERSECRET"))
	do(h, http.MethodGet, "/api/databases/auth/tables/cabber/rows?filter="+url.QueryEscape("password_hash:eq:BADFILTERSECRET"))
	out := logs.String()
	if out == "" {
		t.Fatal("no log lines")
	}
	for _, secret := range []string{"SEARCHSECRET", "FILTERSECRET", "BADFILTERSECRET", "password_hash"} {
		if strings.Contains(out, secret) {
			t.Errorf("log contains %q: %s", secret, out)
		}
	}
	for _, field := range []string{`"operation":"rows"`, `"table":"cabber"`, `"duration_ms"`, `"request_id"`, `"error_class":"invalid_filter"`} {
		if !strings.Contains(out, field) {
			t.Errorf("log lacks %s: %s", field, out)
		}
	}
}
