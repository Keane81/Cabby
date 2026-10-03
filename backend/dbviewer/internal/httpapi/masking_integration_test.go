//go:build integration

package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rs/zerolog"

	"github.com/Keane81/Cabby/backend/dbviewer/internal/itest"
	"github.com/Keane81/Cabby/backend/dbviewer/internal/store"
)

func TestNoResponseCarriesSecrets(t *testing.T) {
	pool := itest.Pool(t, "auth")
	itest.Seed(t, pool)
	s := store.New([]store.Source{{Name: "auth", Pool: pool}}, store.DefaultQueryTimeout)
	h := New(s, fstest.MapFS{}, zerolog.Nop())

	get := func(target string) (int, string) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec.Code, rec.Body.String()
	}
	secrets := []string{itest.SecretHash, itest.SecretToken, "c2VjcmV0", "U0VDUkVU"} // plain and base64 prefixes

	code, body := get("/api/databases?refresh=1")
	if code != 200 {
		t.Fatalf("databases: %d %s", code, body)
	}
	var list struct {
		Databases []struct {
			Tables []struct {
				Name    string
				Columns []struct {
					Name                                        string
					Sensitive, Searchable, Filterable, Sortable bool
				}
			}
		}
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatal(err)
	}
	flagged := map[string]bool{}
	var targets []string
	for _, table := range list.Databases[0].Tables {
		if !strings.HasPrefix(table.Name, "dbv_it_") {
			continue
		}
		for _, c := range table.Columns {
			if c.Sensitive {
				flagged[c.Name] = true
				if c.Searchable || c.Filterable || c.Sortable {
					t.Errorf("%s.%s is sensitive but allows operations", table.Name, c.Name)
				}
			}
		}
		base := "/api/databases/auth/tables/" + table.Name + "/rows"
		targets = append(targets, base, base+"?pageSize=200&sort=id&dir=desc", base+"?q=SECRET", base+"?q=hash")
	}
	if !flagged["password_hash"] || !flagged["token_hash"] {
		t.Fatalf("sensitive flags = %v", flagged)
	}

	for _, target := range targets {
		code, body := get(target)
		if code != 200 {
			t.Fatalf("%s: %d %s", target, code, body)
		}
		for _, secret := range secrets {
			if strings.Contains(body, secret) {
				t.Errorf("%s leaks %q", target, secret)
			}
		}
		if strings.Contains(target, "q=SECRET") && strings.Contains(target, "dbv_it_cabber") && !strings.Contains(body, `"rows":[]`) {
			t.Errorf("%s: a secret column took part in the search", target)
		}
	}

	// A masked cell is visible as such.
	_, body = get("/api/databases/auth/tables/dbv_it_cabber/rows?pageSize=1")
	if !strings.Contains(body, `{"masked":true}`) {
		t.Errorf("masked marker missing: %s", body)
	}

	// Direct attempts to filter, sort or search by a secret are refused.
	for _, bad := range []string{
		"filter=" + url.QueryEscape("password_hash:eq:"+itest.SecretHash),
		"filter=" + url.QueryEscape("token_hash:is_null"),
		"sort=password_hash",
	} {
		table := "dbv_it_cabber"
		if strings.Contains(bad, "token_hash") {
			table = "dbv_it_session"
		}
		if code, _ := get("/api/databases/auth/tables/" + table + "/rows?" + bad); code != 400 {
			t.Errorf("%s: status %d, want 400", bad, code)
		}
	}
}
