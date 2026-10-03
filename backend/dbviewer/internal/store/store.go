// Package store is the only place that talks to PostgreSQL. Every statement runs in a READ ONLY
// transaction with a statement timeout, so the viewer cannot change a database even if a bug in the
// query builder asked it to.
package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Keane81/Cabby/backend/dbviewer/internal/catalog"
	"github.com/Keane81/Cabby/backend/dbviewer/internal/query"
)

// Failures a caller maps to a response. None of them carries the text of the database error.
var (
	ErrUnknownTable = errors.New("store: unknown database or table")
	ErrUnavailable  = errors.New("store: database unavailable")
	ErrTimeout      = errors.New("store: query timed out")
	ErrQuery        = errors.New("store: query failed")
)

const (
	// DefaultQueryTimeout bounds every statement (plan.md R-05).
	DefaultQueryTimeout = 5 * time.Second
	// MaxConnections bounds the pool of each database, so the viewer cannot starve the service.
	MaxConnections = 4
	connectTimeout = 3 * time.Second
	schemaTTL      = 30 * time.Second
	// exactCountLimit is the estimated size up to which a whole table is counted exactly.
	exactCountLimit = 100000
)

// Pool is the part of *pgxpool.Pool the store uses.
type Pool interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// Source names one database and the pool that reaches it.
type Source struct {
	Name string
	Pool Pool
}

// NewPool opens a lazily connecting pool for a DSN. The error never repeats the DSN.
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("store: the database address is not usable")
	}
	cfg.MaxConns = MaxConnections
	cfg.ConnConfig.ConnectTimeout = connectTimeout
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, errors.New("store: the database address is not usable")
	}
	return pool, nil
}

type database struct {
	name   string
	pool   Pool
	schema *catalog.Cache
}

// Store serves the databases it was built with, in the order it was given them.
type Store struct {
	dbs          []*database
	queryTimeout time.Duration
}

// New builds a store over the given sources.
func New(sources []Source, queryTimeout time.Duration) *Store {
	s := &Store{queryTimeout: queryTimeout}
	for _, src := range sources {
		s.dbs = append(s.dbs, &database{name: src.Name, pool: src.Pool, schema: catalog.NewCache(schemaTTL, time.Now)})
	}
	return s
}

func (s *Store) find(name string) *database {
	for _, d := range s.dbs {
		if d.name == name {
			return d
		}
	}
	return nil
}

// Masked is the cell of a sensitive column: the value itself is never read.
type Masked struct {
	Masked bool `json:"masked"`
}

// Total is the number of rows a query matches.
type Total struct {
	Value int64  `json:"value"`
	Kind  string `json:"kind"` // exact, estimate or atLeast
}

// Page is one page of rows.
type Page struct {
	Columns  []string `json:"columns"`
	Rows     [][]any  `json:"rows"`
	Total    Total    `json:"total"`
	Page     int      `json:"page"`
	PageSize int      `json:"pageSize"`
}

// Databases describes every database with its tables, statistics and sizes. A database that cannot
// be reached is reported as unavailable; the others are unaffected. refresh drops the cached schema.
func (s *Store) Databases(ctx context.Context, refresh bool) []catalog.Database {
	out := make([]catalog.Database, 0, len(s.dbs))
	for _, d := range s.dbs {
		info := catalog.Database{Name: d.name, Tables: []catalog.Table{}}
		err := s.read(ctx, d, func(tx pgx.Tx) error {
			tables, err := s.schema(ctx, d, tx, refresh)
			if err != nil {
				return err
			}
			info.Tables = append([]catalog.Table(nil), tables...)
			if err := fillStats(ctx, tx, info.Tables); err != nil {
				return err
			}
			return tx.QueryRow(ctx, "select pg_database_size(current_database())").Scan(&info.SizeBytes)
		})
		if err != nil {
			out = append(out, catalog.Database{Name: d.name, Tables: []catalog.Table{}, Error: message(err)})
			continue
		}
		info.Available = true
		out = append(out, info)
	}
	return out
}

func message(err error) string {
	switch {
	case errors.Is(err, ErrUnavailable):
		return "database unavailable"
	case errors.Is(err, ErrTimeout):
		return "query timed out"
	default:
		return "query failed"
	}
}

// Rows returns one page of a table. Invalid conditions come back as *query.Error.
func (s *Store) Rows(ctx context.Context, dbName, tableName string, p query.Params) (Page, error) {
	d := s.find(dbName)
	if d == nil {
		return Page{}, ErrUnknownTable
	}
	var page Page
	err := s.read(ctx, d, func(tx pgx.Tx) error {
		tables, err := s.schema(ctx, d, tx, false)
		if err != nil {
			return err
		}
		table, ok := findTable(tables, tableName)
		if !ok {
			return ErrUnknownTable
		}
		built, err := query.Page(table, p)
		if err != nil {
			return err
		}
		rows, err := queryRows(ctx, tx, built, table)
		if err != nil {
			return err
		}
		total, err := countRows(ctx, tx, table, p)
		if err != nil {
			return err
		}
		page = Page{Rows: rows, Total: total, Page: p.Page, PageSize: p.PageSize}
		for _, c := range table.Columns {
			page.Columns = append(page.Columns, c.Name)
		}
		return nil
	})
	return page, err
}

func findTable(tables []catalog.Table, name string) (catalog.Table, bool) {
	for _, t := range tables {
		if t.Name == name {
			return t, true
		}
	}
	return catalog.Table{}, false
}

func (s *Store) schema(ctx context.Context, d *database, tx pgx.Tx, force bool) ([]catalog.Table, error) {
	if tables := d.schema.Get(force); tables != nil {
		return tables, nil
	}
	tables, err := catalog.Load(ctx, tx)
	if err != nil {
		return nil, err
	}
	d.schema.Put(tables)
	return tables, nil
}

// fillStats adds row estimates and sizes to the tables of a schema. Small and never-analysed tables
// are counted exactly; large ones keep the planner estimate.
func fillStats(ctx context.Context, tx pgx.Tx, tables []catalog.Table) error {
	rows, err := tx.Query(ctx, `
select c.relname, c.reltuples::bigint, pg_relation_size(c.oid), pg_indexes_size(c.oid), pg_total_relation_size(c.oid)
from pg_class c join pg_namespace n on n.oid = c.relnamespace
where n.nspname = 'public' and c.relkind = 'r'`)
	if err != nil {
		return err
	}
	type stat struct{ tuples, data, index, total int64 }
	stats := map[string]stat{}
	for rows.Next() {
		var name string
		var st stat
		if err := rows.Scan(&name, &st.tuples, &st.data, &st.index, &st.total); err != nil {
			rows.Close()
			return err
		}
		stats[name] = st
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range tables {
		st := stats[tables[i].Name]
		tables[i].DataBytes, tables[i].IndexBytes, tables[i].TotalBytes = st.data, st.index, st.total
		if st.tuples >= 0 && st.tuples > exactCountLimit {
			tables[i].EstimatedRows = st.tuples
			continue
		}
		var n int64
		if err := tx.QueryRow(ctx, query.CountAll(tables[i]).SQL).Scan(&n); err != nil {
			return err
		}
		tables[i].EstimatedRows, tables[i].Exact = n, true
	}
	return nil
}

func countRows(ctx context.Context, tx pgx.Tx, table catalog.Table, p query.Params) (Total, error) {
	if !p.HasConditions() {
		var tuples int64
		err := tx.QueryRow(ctx, `select c.reltuples::bigint from pg_class c join pg_namespace n on n.oid = c.relnamespace
where n.nspname = 'public' and c.relname = $1 and c.relkind = 'r'`, table.Name).Scan(&tuples)
		if err != nil {
			return Total{}, err
		}
		if tuples > exactCountLimit {
			return Total{Value: tuples, Kind: "estimate"}, nil
		}
		var n int64
		if err := tx.QueryRow(ctx, query.CountAll(table).SQL).Scan(&n); err != nil {
			return Total{}, err
		}
		return Total{Value: n, Kind: "exact"}, nil
	}
	built, err := query.CountCapped(table, p)
	if err != nil {
		return Total{}, err
	}
	var n int64
	if err := tx.QueryRow(ctx, built.SQL, built.Args...).Scan(&n); err != nil {
		return Total{}, err
	}
	if n > query.CountCap {
		return Total{Value: query.CountCap, Kind: "atLeast"}, nil
	}
	return Total{Value: n, Kind: "exact"}, nil
}

func queryRows(ctx context.Context, tx pgx.Tx, built query.Built, table catalog.Table) ([][]any, error) {
	rows, err := tx.Query(ctx, built.SQL, built.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := [][]any{}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		for i, c := range table.Columns {
			if c.Sensitive {
				values[i] = Masked{Masked: true}
				continue
			}
			values[i] = jsonSafe(values[i])
		}
		out = append(out, values)
	}
	return out, rows.Err()
}

// jsonSafe turns the few values encoding/json refuses into text.
func jsonSafe(v any) any {
	if f, ok := v.(float64); ok && (math.IsNaN(f) || math.IsInf(f, 0)) {
		return fmt.Sprint(f)
	}
	return v
}

// read runs fn in a READ ONLY transaction under the statement timeout and classifies its failure.
func (s *Store) read(ctx context.Context, d *database, fn func(tx pgx.Tx) error) error {
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout+connectTimeout)
	defer cancel()
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return classify(err)
	}
	defer func() {
		rollbackCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		_ = tx.Rollback(rollbackCtx)
	}()
	if _, err := tx.Exec(ctx, fmt.Sprintf("set local statement_timeout = %d", s.queryTimeout.Milliseconds())); err != nil {
		return classify(err)
	}
	return classify(fn(tx))
}

// classify maps a driver error to one of the package errors. Errors the caller must see as they
// are (an invalid request, an unknown table) pass through.
func classify(err error) error {
	if err == nil {
		return nil
	}
	var qe *query.Error
	if errors.As(err, &qe) || errors.Is(err, ErrUnknownTable) {
		return err
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "57014" { // query_canceled: the statement timeout fired
			return ErrTimeout
		}
		return ErrQuery
	}
	var connErr *pgconn.ConnectError
	var netErr net.Error
	if errors.As(err, &connErr) || (errors.As(err, &netErr) && !errors.Is(err, context.DeadlineExceeded)) {
		return ErrUnavailable
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrTimeout
	}
	if errors.Is(err, context.Canceled) {
		return ErrUnavailable
	}
	return ErrQuery
}
