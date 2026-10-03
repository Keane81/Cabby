// Package query builds the SELECT and COUNT statements of the viewer. Identifiers come only from
// the catalog, every user value travels as a parameter, and a column marked sensitive is never
// named in the SELECT list.
package query

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Keane81/Cabby/backend/dbviewer/internal/catalog"
)

// Limits shared with the API contract.
const (
	MaxPageSize     = 200
	DefaultPageSize = 50
	MaxSearchLength = 200
	MaxFilters      = 10
	// CountCap is how many matching rows are counted before the answer becomes "at least".
	CountCap = 10000
)

// Error codes of an invalid request; the HTTP layer turns them into fixed messages.
const (
	CodeInvalidFilter = "invalid_filter"
	CodeInvalidSort   = "invalid_sort"
	CodeInvalidPage   = "invalid_page"
)

// Error is a request the user can fix.
type Error struct{ Code string }

func (e *Error) Error() string { return "query: " + e.Code }

func invalid(code string) error { return &Error{Code: code} }

// Filter is one condition on a column.
type Filter struct {
	Column string
	Op     string
	Value  string
}

// Params is everything that shapes one page of rows.
type Params struct {
	Q        string
	Filters  []Filter
	Sort     string
	Dir      string // "asc" or "desc"; empty means asc
	Page     int
	PageSize int
}

// HasConditions reports whether the page is narrowed by search or filters.
func (p Params) HasConditions() bool { return p.Q != "" || len(p.Filters) > 0 }

// Built is a statement with its positional arguments.
type Built struct {
	SQL  string
	Args []any
}

// ParseFilter splits "column:op:value". The value may itself contain colons.
func ParseFilter(raw string) (Filter, error) {
	parts := strings.SplitN(raw, ":", 3)
	if len(parts) < 2 || parts[0] == "" {
		return Filter{}, invalid(CodeInvalidFilter)
	}
	f := Filter{Column: parts[0], Op: parts[1]}
	if len(parts) == 3 {
		f.Value = parts[2]
	}
	return f, nil
}

func tableName(t catalog.Table) string {
	return pgx.Identifier{"public", t.Name}.Sanitize()
}

func ident(name string) string { return pgx.Identifier{name}.Sanitize() }

// selectExpr is the SELECT-list expression of a column. Sensitive columns are replaced by a
// constant; types that the JSON encoding could mangle are cast to text.
func selectExpr(c catalog.Column) string {
	switch {
	case c.Sensitive:
		return "NULL::text"
	case c.Type == catalog.KindUUID || c.Type == catalog.KindOther:
		return ident(c.Name) + "::text"
	case c.DataType == "numeric":
		return ident(c.Name) + "::text"
	default:
		return ident(c.Name)
	}
}

// Page builds the statement for one page of rows.
func Page(t catalog.Table, p Params) (Built, error) {
	if p.Page < 1 || p.PageSize < 1 || p.PageSize > MaxPageSize {
		return Built{}, invalid(CodeInvalidPage)
	}
	where, args, err := where(t, p)
	if err != nil {
		return Built{}, err
	}
	order, err := orderBy(t, p)
	if err != nil {
		return Built{}, err
	}
	exprs := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		exprs[i] = selectExpr(c) + " AS " + ident(c.Name)
	}
	offset := int64(p.Page-1) * int64(p.PageSize)
	sql := "SELECT " + strings.Join(exprs, ", ") + " FROM " + tableName(t) + where + order +
		fmt.Sprintf(" LIMIT %d OFFSET %d", p.PageSize, offset)
	return Built{SQL: sql, Args: args}, nil
}

// CountCapped builds a statement that counts matching rows but stops after CountCap+1 of them.
func CountCapped(t catalog.Table, p Params) (Built, error) {
	where, args, err := where(t, p)
	if err != nil {
		return Built{}, err
	}
	sql := "SELECT count(*) FROM (SELECT 1 FROM " + tableName(t) + where +
		fmt.Sprintf(" LIMIT %d) AS counted", CountCap+1)
	return Built{SQL: sql, Args: args}, nil
}

// CountAll builds the exact count of a whole table.
func CountAll(t catalog.Table) Built {
	return Built{SQL: "SELECT count(*) FROM " + tableName(t)}
}

func orderBy(t catalog.Table, p Params) (string, error) {
	var parts []string
	used := map[string]bool{}
	if p.Sort != "" {
		c, ok := t.Column(p.Sort)
		if !ok || !c.Sortable {
			return "", invalid(CodeInvalidSort)
		}
		dir := "ASC"
		switch strings.ToLower(p.Dir) {
		case "", "asc":
		case "desc":
			dir = "DESC"
		default:
			return "", invalid(CodeInvalidSort)
		}
		parts = append(parts, ident(c.Name)+" "+dir+" NULLS LAST")
		used[c.Name] = true
	}
	// The primary key breaks ties, so a row never appears on two pages or on none.
	for _, name := range t.PrimaryKey {
		if !used[name] {
			parts = append(parts, ident(name)+" ASC")
		}
	}
	if len(parts) == 0 {
		return "", nil
	}
	return " ORDER BY " + strings.Join(parts, ", "), nil
}

func where(t catalog.Table, p Params) (string, []any, error) {
	var (
		clauses []string
		args    []any
	)
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}

	if p.Q != "" {
		if len([]rune(p.Q)) > MaxSearchLength {
			return "", nil, invalid(CodeInvalidFilter)
		}
		var searches []string
		var placeholder string
		for _, c := range t.Columns {
			if !c.Searchable {
				continue
			}
			if placeholder == "" {
				placeholder = arg("%" + escapeLike(p.Q) + "%")
			}
			searches = append(searches, ident(c.Name)+"::text ILIKE "+placeholder)
		}
		if len(searches) == 0 {
			clauses = append(clauses, "false")
		} else {
			clauses = append(clauses, "("+strings.Join(searches, " OR ")+")")
		}
	}

	if len(p.Filters) > MaxFilters {
		return "", nil, invalid(CodeInvalidFilter)
	}
	for _, f := range p.Filters {
		c, ok := t.Column(f.Column)
		if !ok || !c.Filterable {
			return "", nil, invalid(CodeInvalidFilter)
		}
		clause, err := filterClause(c, f, arg)
		if err != nil {
			return "", nil, err
		}
		clauses = append(clauses, clause)
	}

	if len(clauses) == 0 {
		return "", nil, nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args, nil
}

func filterClause(c catalog.Column, f Filter, arg func(any) string) (string, error) {
	col := ident(c.Name)
	if f.Op == "is_null" {
		return col + " IS NULL", nil
	}
	if f.Op == "contains" {
		if c.Type != catalog.KindText && c.Type != catalog.KindUUID {
			return "", invalid(CodeInvalidFilter)
		}
		return col + "::text ILIKE " + arg("%"+escapeLike(f.Value)+"%"), nil
	}
	var sqlOp string
	switch f.Op {
	case "eq":
		sqlOp = "="
	case "gte":
		sqlOp = ">="
	case "lte":
		sqlOp = "<="
	default:
		return "", invalid(CodeInvalidFilter)
	}
	if (f.Op == "gte" || f.Op == "lte") && c.Type != catalog.KindNumber && c.Type != catalog.KindTimestamp {
		return "", invalid(CodeInvalidFilter)
	}
	value, err := parseValue(c, f.Value)
	if err != nil {
		return "", err
	}
	return col + " " + sqlOp + " " + arg(value), nil
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

var timeLayouts = []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02"}

// parseValue converts the text of a filter into the Go type that matches the column.
func parseValue(c catalog.Column, raw string) (any, error) {
	switch c.Type {
	case catalog.KindText:
		return raw, nil
	case catalog.KindUUID:
		if !uuidPattern.MatchString(raw) {
			return nil, invalid(CodeInvalidFilter)
		}
		return strings.ToLower(raw), nil
	case catalog.KindBool:
		switch strings.ToLower(raw) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, invalid(CodeInvalidFilter)
	case catalog.KindNumber:
		switch c.DataType {
		case "smallint", "integer", "bigint":
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return nil, invalid(CodeInvalidFilter)
			}
			return n, nil
		}
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, invalid(CodeInvalidFilter)
		}
		if c.DataType == "numeric" {
			return raw, nil // compared as text-typed numeric, keeps full precision
		}
		return n, nil
	case catalog.KindTimestamp:
		for _, layout := range timeLayouts {
			if ts, err := time.Parse(layout, raw); err == nil {
				return ts, nil
			}
		}
		return nil, invalid(CodeInvalidFilter)
	}
	return nil, invalid(CodeInvalidFilter)
}

// escapeLike makes %, _ and \ literal for ILIKE (whose default escape character is the backslash).
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
