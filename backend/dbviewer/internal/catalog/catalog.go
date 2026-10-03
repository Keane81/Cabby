package catalog

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// Table is one base table of the `public` schema with its columns and, when known, its statistics.
type Table struct {
	Name          string   `json:"name"`
	EstimatedRows int64    `json:"estimatedRows"`
	Exact         bool     `json:"exact"`
	DataBytes     int64    `json:"dataBytes"`
	IndexBytes    int64    `json:"indexBytes"`
	TotalBytes    int64    `json:"totalBytes"`
	PrimaryKey    []string `json:"primaryKey"`
	Columns       []Column `json:"columns"`
}

// Column returns the column with the given name; the viewer only builds SQL from names found here.
func (t Table) Column(name string) (Column, bool) {
	for _, c := range t.Columns {
		if c.Name == name {
			return c, true
		}
	}
	return Column{}, false
}

// Database is a database as the API shows it.
type Database struct {
	Name      string  `json:"name"`
	Available bool    `json:"available"`
	Error     string  `json:"error,omitempty"`
	SizeBytes int64   `json:"sizeBytes"`
	Tables    []Table `json:"tables"`
}

// Queryer is what Load needs from a transaction or connection.
type Queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

const columnsSQL = `
select c.table_name, c.column_name, c.data_type
from information_schema.columns c
join information_schema.tables t
  on t.table_schema = c.table_schema and t.table_name = c.table_name and t.table_type = 'BASE TABLE'
where c.table_schema = 'public'
order by c.table_name, c.ordinal_position`

const primaryKeySQL = `
select tc.table_name, kcu.column_name
from information_schema.table_constraints tc
join information_schema.key_column_usage kcu
  on kcu.constraint_name = tc.constraint_name
 and kcu.table_schema = tc.table_schema
 and kcu.table_name = tc.table_name
where tc.table_schema = 'public' and tc.constraint_type = 'PRIMARY KEY'
order by tc.table_name, kcu.ordinal_position`

// Load reads tables, columns and primary keys of the `public` schema. Tables come back ordered by
// name; statistics are left zero for the store to fill.
func Load(ctx context.Context, q Queryer) ([]Table, error) {
	rows, err := q.Query(ctx, columnsSQL)
	if err != nil {
		return nil, err
	}
	var tables []Table
	index := map[string]int{}
	for rows.Next() {
		var table, column, dataType string
		if err := rows.Scan(&table, &column, &dataType); err != nil {
			rows.Close()
			return nil, err
		}
		i, ok := index[table]
		if !ok {
			i = len(tables)
			index[table] = i
			tables = append(tables, Table{Name: table, PrimaryKey: []string{}})
		}
		tables[i].Columns = append(tables[i].Columns, Classify(column, dataType))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = q.Query(ctx, primaryKeySQL)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			rows.Close()
			return nil, err
		}
		if i, ok := index[table]; ok {
			tables[i].PrimaryKey = append(tables[i].PrimaryKey, column)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return tables, nil
}

// Cache keeps the schema of one database for a short time: the schema changes with migrations, not
// with every request.
type Cache struct {
	ttl time.Duration
	now func() time.Time

	mu       sync.Mutex
	tables   []Table
	loadedAt time.Time
}

// NewCache returns an empty cache whose entries live for ttl.
func NewCache(ttl time.Duration, now func() time.Time) *Cache {
	return &Cache{ttl: ttl, now: now}
}

// Get returns the cached schema, or nil when it is absent, stale, or force is set.
func (c *Cache) Get(force bool) []Table {
	c.mu.Lock()
	defer c.mu.Unlock()
	if force || c.tables == nil || c.now().Sub(c.loadedAt) >= c.ttl {
		return nil
	}
	return c.tables
}

// Put stores a freshly loaded schema.
func (c *Cache) Put(tables []Table) {
	if tables == nil {
		tables = []Table{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tables = tables
	c.loadedAt = c.now()
}
