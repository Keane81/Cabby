// Package httpapi serves the viewer: a small read-only JSON API and the embedded page.
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/Keane81/Cabby/backend/dbviewer/internal/catalog"
	"github.com/Keane81/Cabby/backend/dbviewer/internal/query"
	"github.com/Keane81/Cabby/backend/dbviewer/internal/store"
)

// Backend is what the API needs from the store.
type Backend interface {
	Databases(ctx context.Context, refresh bool) []catalog.Database
	Rows(ctx context.Context, db, table string, p query.Params) (store.Page, error)
}

// callInfo is what a handler tells the logging middleware about the call. It holds names only:
// never a value from the request, a search, a filter or the database.
type callInfo struct {
	operation  string
	table      string
	errorClass string
	status     int
}

type callKey struct{}

// New builds the handler: the API under /api, /healthz and the static page at the root.
func New(backend Backend, page fs.FS, logger zerolog.Logger) http.Handler {
	s := &server{backend: backend}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.get("health", s.health))
	mux.HandleFunc("/api/databases", s.get("list_databases", s.databases))
	mux.HandleFunc("/api/databases/{db}/tables/{table}/rows", s.get("rows", s.rows))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, errNotFound)
		setInfo(r, "not_found", errNotFound)
	})
	static := http.FileServerFS(page)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, errMethod)
			setInfo(r, "static", errMethod)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		static.ServeHTTP(w, r)
	})
	return logging(logger, mux)
}

type server struct{ backend Backend }

func setInfo(r *http.Request, operation string, e apiError) {
	if info, ok := r.Context().Value(callKey{}).(*callInfo); ok {
		info.operation, info.errorClass, info.status = operation, e.code, e.status
	}
}

// get restricts a handler to GET and records its operation name.
func (s *server) get(operation string, h func(http.ResponseWriter, *http.Request, *callInfo)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info, _ := r.Context().Value(callKey{}).(*callInfo)
		if info == nil {
			info = &callInfo{}
		}
		info.operation = operation
		if r.Method != http.MethodGet {
			writeError(w, errMethod)
			info.errorClass, info.status = errMethod.code, errMethod.status
			return
		}
		h(w, r, info)
	}
}

func (s *server) fail(w http.ResponseWriter, info *callInfo, e apiError) {
	writeError(w, e)
	info.errorClass, info.status = e.code, e.status
}

func (s *server) health(w http.ResponseWriter, _ *http.Request, _ *callInfo) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) databases(w http.ResponseWriter, r *http.Request, _ *callInfo) {
	refresh := r.URL.Query().Get("refresh") == "1"
	writeJSON(w, http.StatusOK, map[string]any{
		"refreshedAt": time.Now().UTC().Format(time.RFC3339),
		"databases":   s.backend.Databases(r.Context(), refresh),
	})
}

func (s *server) rows(w http.ResponseWriter, r *http.Request, info *callInfo) {
	info.table = r.PathValue("table")
	params, apiErr := parseParams(r)
	if apiErr != nil {
		s.fail(w, info, *apiErr)
		return
	}
	page, err := s.backend.Rows(r.Context(), r.PathValue("db"), r.PathValue("table"), params)
	if err != nil {
		s.fail(w, info, fromError(err))
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func parseParams(r *http.Request) (query.Params, *apiError) {
	q := r.URL.Query()
	p := query.Params{
		Q:        q.Get("q"),
		Sort:     q.Get("sort"),
		Dir:      q.Get("dir"),
		Page:     1,
		PageSize: query.DefaultPageSize,
	}
	var err error
	if v := q.Get("page"); v != "" {
		if p.Page, err = strconv.Atoi(v); err != nil || p.Page < 1 {
			return p, &errInvalidPage
		}
	}
	if v := q.Get("pageSize"); v != "" {
		if p.PageSize, err = strconv.Atoi(v); err != nil || p.PageSize < 1 || p.PageSize > query.MaxPageSize {
			return p, &errInvalidPage
		}
	}
	if len(q["filter"]) > query.MaxFilters {
		return p, &errInvalidFilter
	}
	for _, raw := range q["filter"] {
		f, err := query.ParseFilter(raw)
		if err != nil {
			return p, &errInvalidFilter
		}
		p.Filters = append(p.Filters, f)
	}
	return p, nil
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// logging writes one line per API call: operation, table, duration and, on failure, the error
// class. Static files are not logged. Nothing from the query string reaches the line (FR-019).
func logging(logger zerolog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := &callInfo{}
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		started := time.Now()
		next.ServeHTTP(sw, r.WithContext(context.WithValue(r.Context(), callKey{}, info)))
		if info.operation == "" || info.operation == "static" || info.operation == "health" {
			return
		}
		event := logger.Info()
		if sw.status >= 400 {
			event = logger.Warn().Str("error_class", info.errorClass)
		}
		event = event.Str("operation", info.operation).Str("request_id", newRequestID()).
			Int64("duration_ms", time.Since(started).Milliseconds())
		if info.table != "" {
			event = event.Str("table", info.table)
		}
		event.Msg("request")
	})
}

func newRequestID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
