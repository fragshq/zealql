package zealql

import (
	"reflect"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
)

// SQLType represents a standard SQLite data type.
type SQLType string

const (
	Integer  SQLType = "INTEGER"
	Real     SQLType = "REAL"
	Text     SQLType = "TEXT"
	Blob     SQLType = "BLOB"
	DateTime SQLType = "DATETIME"
	Null     SQLType = "NULL"
	JSON     SQLType = "JSON"
)

// inferType determines the best SQLType for a given Go value.
// It handles pointers, common primitives, and identifies complex types
// that should be stored as JSON (TEXT).
func inferType(v any) SQLType {
	if v == nil {
		return Null
	}

	rv := reflect.ValueOf(v)
	// Handle pointers by dereferencing until the underlying type is found.
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return Null
		}
		rv = rv.Elem()
		v = rv.Interface()
	}

	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return Integer
	case reflect.Float32, reflect.Float64:
		return Real
	case reflect.Bool:
		return Integer // SQLite uses 0 and 1 for booleans
	case reflect.String:
		return Text
	case reflect.Slice, reflect.Array:
		switch v.(type) {
		case uuid.UUID, *uuid.UUID:
			return Text
		}
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			return Blob
		}

		return JSON
	case reflect.Map, reflect.Struct:
		// Check for time.Time
		switch v.(type) {
		case time.Time, *time.Time:
			return DateTime
		default:
			return JSON
		}
	default:
		return Text
	}
}

// promoteType determines the resulting SQLType when two different types
// are found for the same column (e.g., Integer and Real becomes Real).
func promoteType(current, next SQLType) SQLType {
	if current == next {
		return current
	}
	if current == Null {
		return next
	}
	if next == Null {
		return current
	}

	// Promotion rules
	if (current == Integer && next == Real) || (current == Real && next == Integer) {
		return Real
	}

	// Fallback to TEXT for mixed or incompatible types.
	return Text
}

// inferJSONSchema generates a JSON Schema from a set of sample values.
// It recursively analyzes maps and slices to build a structural definition.
func inferJSONSchema(samples []any) *jsonschema.Schema {
	if len(samples) == 0 {
		return nil
	}

	s := &jsonschema.Schema{}

	// Simple logic: find first non-nil sample to determine type
	var firstNonNil any
	for _, sample := range samples {
		if sample != nil {
			firstNonNil = sample
			break
		}
	}

	if firstNonNil == nil {
		s.Type = "null"
		return s
	}

	rv := reflect.ValueOf(firstNonNil)
	switch rv.Kind() {
	case reflect.String:
		s.Type = "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		s.Type = "integer"
	case reflect.Float32, reflect.Float64:
		s.Type = "number"
	case reflect.Bool:
		s.Type = "boolean"
	case reflect.Slice, reflect.Array:
		s.Type = "array"
		var items []any
		for _, sample := range samples {
			if sample == nil {
				continue
			}
			srv := reflect.ValueOf(sample)
			if srv.Kind() != reflect.Slice && srv.Kind() != reflect.Array {
				continue
			}
			for i := 0; i < srv.Len(); i++ {
				items = append(items, srv.Index(i).Interface())
			}
		}
		if len(items) > 0 {
			s.Items = inferJSONSchema(items)
		}
	case reflect.Map:
		s.Type = "object"
		s.Properties = make(map[string]*jsonschema.Schema)
		allKeys := make(map[string]bool)
		for _, sample := range samples {
			if sample == nil {
				continue
			}
			m, ok := sample.(map[string]any)
			if !ok {
				continue
			}
			for k := range m {
				allKeys[k] = true
			}
		}

		for k := range allKeys {
			var keySamples []any
			for _, sample := range samples {
				if sample == nil {
					continue
				}
				m, ok := sample.(map[string]any)
				if !ok {
					continue
				}
				if val, exists := m[k]; exists {
					keySamples = append(keySamples, val)
				}
			}
			s.Properties[k] = inferJSONSchema(keySamples)
		}
	default:
		s.Type = "any"
	}

	return s
}
