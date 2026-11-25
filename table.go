package zealql

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
)

// Column represents a single database column with an optional inferred JSON Schema.
type Column struct {
	Name       string
	Type       SQLType
	JSONSchema *jsonschema.Schema // Optional: Schema for structured JSON data in TEXT columns.
}

// Table represents a SQLite table definition, including its columns
// and logic for SQL generation and data ingestion.
type Table struct {
	Name    string
	Columns []Column
}

// InferTable analyzes a slice of map data to determine the most appropriate
// SQLite table schema. It handles type promotion and JSON schema inference
// for structured fields.
func InferTable(tableName string, data []map[string]any) (*Table, error) {
	if len(data) == 0 {
		return &Table{Name: tableName}, nil
	}

	columnTypes := make(map[string]SQLType)
	samples := make(map[string][]any)
	var keys []string

	for _, row := range data {
		for k, v := range row {
			if _, exists := columnTypes[k]; !exists {
				keys = append(keys, k)
				columnTypes[k] = inferType(v)
			} else {
				columnTypes[k] = promoteType(columnTypes[k], inferType(v))
			}
			samples[k] = append(samples[k], v)
		}
	}

	sort.Strings(keys)

	columns := make([]Column, len(keys))
	for i, k := range keys {
		col := Column{
			Name: k,
			Type: columnTypes[k],
		}
		if col.Type == JSON {
			col.JSONSchema = inferJSONSchema(samples[k])
		}
		columns[i] = col
	}

	return &Table{
		Name:    tableName,
		Columns: columns,
	}, nil
}

// Compatible checks if the provided table has an identical schema (column names and types)
// to the current table. It assumes columns in both tables are sorted by name.
func (t *Table) Compatible(other *Table) bool {
	if len(t.Columns) != len(other.Columns) {
		return false
	}
	for i := range t.Columns {
		if t.Columns[i].Name != other.Columns[i].Name || t.Columns[i].Type != other.Columns[i].Type {
			return false
		}
	}
	return true
}

// ToSQL generates a CREATE TABLE statement for the table.
// If a column has an inferred JSON schema, it is included as a SQL comment.
func (t *Table) ToSQL() string {
	if len(t.Columns) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n", quoteIdentifier(t.Name)))
	for i, col := range t.Columns {
		comment := ""
		if col.JSONSchema != nil {
			b, _ := json.Marshal(col.JSONSchema)
			comment = fmt.Sprintf(" /* JSON Schema: %s */", string(b))
		}
		sb.WriteString(fmt.Sprintf("  %s %s%s", quoteIdentifier(col.Name), col.Type, comment))
		if i < len(t.Columns)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	sb.WriteString(");")

	return sb.String()
}

// GenerateInsertSQL creates an INSERT INTO statement template with
// question mark (?) placeholders for all table columns.
func (t *Table) GenerateInsertSQL() string {
	if len(t.Columns) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("INSERT INTO %s (", quoteIdentifier(t.Name)))
	placeholders := make([]string, len(t.Columns))
	for i, col := range t.Columns {
		sb.WriteString(quoteIdentifier(col.Name))
		if i < len(t.Columns)-1 {
			sb.WriteString(", ")
		}
		placeholders[i] = "?"
	}
	sb.WriteString(") VALUES (")
	sb.WriteString(strings.Join(placeholders, ", "))
	sb.WriteString(")")

	return sb.String()
}

// quoteIdentifier wraps a SQL identifier (table/column name) in double quotes
// and escapes any internal double quotes by doubling them.
func quoteIdentifier(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// Preparer is satisfied by *sql.DB, *sql.Tx and *sql.Conn.
type Preparer interface {
	Prepare(query string) (*sql.Stmt, error)
}

// Insert executes the insertion of multiple rows of data into the provided
// database, transaction or connection. It automatically marshals maps, structs,
// slices and arrays to JSON strings, and dereferences pointers.
func (t *Table) Insert(db Preparer, data []map[string]any) error {
	if len(data) == 0 {
		return nil
	}

	query := t.GenerateInsertSQL()
	stmt, err := db.Prepare(query)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer func() {
		_ = stmt.Close()
	}()

	for _, row := range data {
		args := make([]any, len(t.Columns))
		for i, col := range t.Columns {
			val, err := toSQLValue(row[col.Name])
			if err != nil {
				return fmt.Errorf("failed to convert column %s: %w", col.Name, err)
			}
			args[i] = val
		}
		if _, err := stmt.Exec(args...); err != nil {
			return fmt.Errorf("failed to execute insert: %w", err)
		}
	}

	return nil
}

// toSQLValue converts a Go value into something the SQLite driver can store, mirroring the decisions taken by
// inferType: pointers are dereferenced, byte slices and arrays are stored as BLOBs, and maps, structs, slices and
// arrays are marshaled to JSON. Values implementing driver.Valuer (e.g. uuid.UUID) and time.Time are passed as-is.
func toSQLValue(val any) (any, error) {
	if val == nil {
		return nil, nil
	}
	if _, ok := val.(driver.Valuer); ok {
		return val, nil
	}
	rv := reflect.ValueOf(val)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, nil
		}
		rv = rv.Elem()
	}
	val = rv.Interface()
	switch val.(type) {
	case driver.Valuer, time.Time:
		return val, nil
	}

	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			b := make([]byte, rv.Len())
			reflect.Copy(reflect.ValueOf(b), rv)
			return b, nil
		}
		return marshalJSON(val)
	case reflect.Map, reflect.Struct:
		return marshalJSON(val)
	default:
		return val, nil
	}
}

func marshalJSON(val any) (any, error) {
	b, err := json.Marshal(val)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}
