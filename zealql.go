// Package zealql provides a lightweight in-memory SQLite wrapper that automatically
// infers schemas from map data and supports JSON columns with embedded JSON Schema comments.
package zealql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	_ "modernc.org/sqlite"
)

// Database represents an in-memory data manipulation database that tracks
// inferred table schemas.
type Database struct {
	db     *sql.DB
	mu     sync.RWMutex
	tables map[string]*Table
}

// NewDatabase initializes a new in-memory SQLite database.
// It returns an error if the database driver is not available or
// fails to initialize.
func NewDatabase() (*Database, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("failed to open in-memory database: %w", err)
	}
	// Every SQLite ":memory:" connection is a separate, empty database, so the pool must never hold more than one
	// connection, and that connection must never be closed while idle. Concurrent callers wait for it in turn.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)

	return &Database{
		db:     db,
		tables: make(map[string]*Table),
	}, nil
}

// Insert infers a table schema from data (a slice of maps), creates the table if it doesn't exist yet and inserts
// the rows. If the table already exists, the inferred schema must be compatible with the existing one.
// The whole operation is atomic: either the table is created and all rows are inserted, or nothing changes.
// It is safe for concurrent use.
func (d *Database) Insert(name string, data any) (*Table, error) {
	rows, err := toRows(data)
	if err != nil {
		return nil, err
	}
	table, err := InferTable(name, rows)
	if err != nil {
		return nil, err
	}

	// The lock is held until the table is created and registered, so concurrent inserts into the same new table
	// can't skip the creation and insert into a table that doesn't exist yet.
	d.mu.Lock()
	defer d.mu.Unlock()

	existingTable, exists := d.tables[name]
	switch {
	case len(rows) == 0:
		// nothing to insert. An empty batch doesn't define a schema, so a new table is not registered and a later
		// non-empty insert can still define it.
		if exists {
			return existingTable, nil
		}
		return table, nil
	case exists:
		if !existingTable.Compatible(table) {
			return nil, fmt.Errorf("inferred schema for %s does not match existing schema", name)
		}
		// Use the existing table to maintain metadata consistency (like JSON schemas)
		table = existingTable
	}

	// Only use tx from here on: the pool has a single connection, which tx holds until commit or rollback.
	tx, err := d.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if !exists {
		if _, err := tx.Exec(table.ToSQL()); err != nil {
			return nil, fmt.Errorf("failed to create table %s: %w", name, err)
		}
	}
	if err := table.Insert(tx, rows); err != nil {
		return nil, fmt.Errorf("failed to insert data into %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit data into %s: %w", name, err)
	}

	if !exists {
		d.tables[name] = table
	}
	return table, nil
}

// toRows converts the data accepted by Insert into a slice of maps.
func toRows(data any) ([]map[string]any, error) {
	switch t := data.(type) {
	case []map[string]any:
		return t, nil
	case []any:
		rows := make([]map[string]any, 0, len(t))
		for _, item := range t {
			cast, ok := item.(map[string]any)
			if !ok {
				return nil, errors.New("data must be a slice of maps")
			}
			rows = append(rows, cast)
		}
		return rows, nil
	default:
		return nil, errors.New("data must be a slice of maps")
	}
}

// GetTable returns a table definition by its name if it was created via Insert.
func (d *Database) GetTable(name string) (*Table, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	t, ok := d.tables[name]
	return t, ok
}

func (d *Database) ListTables() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	var names []string
	for name := range d.tables {
		names = append(names, name)
	}
	return names
}

// Query executes a SQL query against the database and returns the results
// as a slice of maps. It automatically handles basic SQLite type conversion
// and attempts to unmarshal JSON strings back into Go structures.
func (d *Database) Query(query string, args ...any) ([]map[string]any, error) {
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}

	var results []map[string]any
	ptrs := make([]any, len(cols))
	vals := make([]any, len(cols))
	for i := range ptrs {
		ptrs[i] = &vals[i]
	}

	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := make(map[string]any)
		for i, colName := range cols {
			val := vals[i]
			if columnTypes[i].DatabaseTypeName() == string(JSON) {
				val = decodeJSON(val)
			}
			m[colName] = val
		}
		results = append(results, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

// decodeJSON unmarshals a JSON column value into a Go structure. Values that are not valid JSON are returned as-is.
func decodeJSON(val any) any {
	raw := []byte{}
	switch v := val.(type) {
	case string:
		raw = []byte(v)
	case []byte:
		raw = v
	default:
		return val
	}
	var js any
	if err := json.Unmarshal(raw, &js); err != nil {
		return val
	}
	return js
}

// Close closes the underlying SQLite database connection.
func (d *Database) Close() error {
	return d.db.Close()
}

// serializer defines the interface for SQLite serialization/deserialization.
// This matches the methods on the unexported conn struct in modernc.org/sqlite.
type serializer interface {
	Serialize() ([]byte, error)
	Deserialize([]byte) error
}

// Clone creates a complete deep copy of the database, including all tables,
// data, and inferred schema metadata.
func (d *Database) Clone() (*Database, error) {
	// Hold the read lock for the whole clone, so that no Insert can change data or metadata in between, and the
	// clone's metadata matches its data.
	d.mu.RLock()
	defer d.mu.RUnlock()

	// 1. Create a new destination database
	dst, err := NewDatabase()
	if err != nil {
		return nil, fmt.Errorf("failed to create destination database for clone: %w", err)
	}

	// 2. Perform raw SQLite serialization/deserialization
	ctx := context.Background()
	srcConn, err := d.db.Conn(ctx)
	if err != nil {
		_ = dst.Close()
		return nil, fmt.Errorf("failed to get source connection: %w", err)
	}
	defer srcConn.Close()

	dstConn, err := dst.db.Conn(ctx)
	if err != nil {
		_ = dst.Close()
		return nil, fmt.Errorf("failed to get destination connection: %w", err)
	}
	defer dstConn.Close()

	var data []byte
	err = srcConn.Raw(func(srcRaw any) error {
		s, ok := srcRaw.(serializer)
		if !ok {
			return fmt.Errorf("source connection does not support serialization")
		}
		var err error
		data, err = s.Serialize()
		return err
	})

	if err != nil {
		_ = dst.Close()
		return nil, fmt.Errorf("serialization failed: %w", err)
	}

	if data != nil {
		err = dstConn.Raw(func(dstRaw any) error {
			s, ok := dstRaw.(serializer)
			if !ok {
				return fmt.Errorf("destination connection does not support deserialization")
			}
			return s.Deserialize(data)
		})

		if err != nil {
			_ = dst.Close()
			return nil, fmt.Errorf("deserialization failed: %w", err)
		}
	}

	// 3. Deep copy the tables metadata
	for name, table := range d.tables {
		clonedTable := &Table{
			Name:    table.Name,
			Columns: make([]Column, len(table.Columns)),
		}
		copy(clonedTable.Columns, table.Columns)
		dst.tables[name] = clonedTable
	}

	return dst, nil
}
