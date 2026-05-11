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

	return &Database{
		db:     db,
		tables: make(map[string]*Table),
	}, nil
}

// CreateTableFromMaps infers a schema from the provided sample data, executes the
// CREATE TABLE statement, and inserts the data. The inferred schema is stored
// for future reference.
func (d *Database) CreateTableFromMaps(name string, data []map[string]any) (*Table, error) {
	table, err := InferTable(name, data)
	if err != nil {
		return nil, err
	}

	if _, err := d.db.Exec(table.ToSQL()); err != nil {
		return nil, fmt.Errorf("failed to create table %s: %w", name, err)
	}

	if err := table.Insert(d.db, data); err != nil {
		return nil, fmt.Errorf("failed to insert initial data into %s: %w", name, err)
	}

	d.mu.Lock()
	d.tables[name] = table
	d.mu.Unlock()
	return table, nil
}

func (d *Database) CreateTable(name string, data []any) (*Table, error) {
	mid := make([]map[string]any, len(data))
	for i, item := range data {
		if cast, ok := item.(map[string]any); ok {
			mid[i] = cast
		} else {
			return nil, errors.New("data must be a slice of maps")
		}
	}
	return d.CreateTableFromMaps(name, mid)
}

// GetTable returns a table definition by its name if it was created via CreateTableFromMaps.
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
			if columnTypes[i].DatabaseTypeName() == string(JSON) && val != nil {
				var js any
				if err := json.Unmarshal([]byte(val.(string)), &js); err == nil {
					val = js
				}
			}
			m[colName] = val
		}
		results = append(results, m)
	}

	return results, nil
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
	// 1. Create a new destination database
	dst, err := NewDatabase()
	if err != nil {
		return nil, fmt.Errorf("failed to create destination database for clone: %w", err)
	}

	// 2. Perform raw SQLite serialization/deserialization
	ctx := context.Background()
	srcConn, err := d.db.Conn(ctx)
	if err != nil {
		dst.Close()
		return nil, fmt.Errorf("failed to get source connection: %w", err)
	}
	defer srcConn.Close()

	dstConn, err := dst.db.Conn(ctx)
	if err != nil {
		dst.Close()
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
		dst.Close()
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
			dst.Close()
			return nil, fmt.Errorf("deserialization failed: %w", err)
		}
	}

	// 3. Deep copy the tables metadata
	d.mu.RLock()
	defer d.mu.RUnlock()
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
