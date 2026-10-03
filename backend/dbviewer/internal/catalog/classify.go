// Package catalog reads the schema of a database and decides, column by column, what the viewer
// may do with it. Everything the query builder later puts into SQL as an identifier comes from here.
package catalog

import "strings"

// Kind is the coarse type of a column as the viewer sees it.
type Kind string

const (
	KindText      Kind = "text"
	KindNumber    Kind = "number"
	KindTimestamp Kind = "timestamp"
	KindUUID      Kind = "uuid"
	KindBool      Kind = "bool"
	KindOther     Kind = "other"
)

// Column describes one column and what the viewer allows for it.
type Column struct {
	Name       string `json:"name"`
	Type       Kind   `json:"type"`
	Sensitive  bool   `json:"sensitive"`
	Searchable bool   `json:"searchable"`
	Filterable bool   `json:"filterable"`
	Sortable   bool   `json:"sortable"`

	// DataType is the PostgreSQL type name (information_schema.columns.data_type). It stays inside
	// the process: the query builder needs it to choose casts and to parse filter values.
	DataType string `json:"-"`
}

// sensitiveMarkers are the name fragments that make a column secret. The match is on purpose broad
// and by name: a column added by a later migration is hidden until someone decides otherwise.
var sensitiveMarkers = []string{"hash", "secret", "token", "password"}

// Classify decides the kind and the allowed operations for a column from its name and PostgreSQL
// type. A sensitive column is neither searchable, filterable nor sortable: its value is never read.
func Classify(name, dataType string) Column {
	kind := kindOf(dataType)
	col := Column{Name: name, Type: kind, DataType: dataType, Sensitive: isSensitive(name, dataType)}
	if col.Sensitive {
		return col
	}
	col.Searchable = kind == KindText || kind == KindUUID
	col.Filterable = kind != KindOther
	col.Sortable = kind != KindOther
	return col
}

func isSensitive(name, dataType string) bool {
	if dataType == "bytea" {
		return true
	}
	lower := strings.ToLower(name)
	for _, marker := range sensitiveMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func kindOf(dataType string) Kind {
	switch dataType {
	case "text", "character varying", "character", "citext":
		return KindText
	case "smallint", "integer", "bigint", "numeric", "real", "double precision":
		return KindNumber
	case "timestamp with time zone", "timestamp without time zone", "date":
		return KindTimestamp
	case "uuid":
		return KindUUID
	case "boolean":
		return KindBool
	default:
		return KindOther
	}
}
