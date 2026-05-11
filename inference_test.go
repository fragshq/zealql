package zealql

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInferTable(t *testing.T) {
	tests := []struct {
		name      string
		tableName string
		data      []map[string]any
		wantCols  map[string]SQLType
	}{
		{
			name:      "Simple types",
			tableName: "users",
			data: []map[string]any{
				{"id": 1, "name": "Alice", "active": true, "score": 10.5},
			},
			wantCols: map[string]SQLType{
				"id":     Integer,
				"name":   Text,
				"active": Integer,
				"score":  Real,
			},
		},
		{
			name:      "Type promotion (Int to Real)",
			tableName: "data",
			data: []map[string]any{
				{"val": 10},
				{"val": 12.5},
			},
			wantCols: map[string]SQLType{
				"val": Real,
			},
		},
		{
			name:      "Incompatible types (fallback to Text)",
			tableName: "mixed",
			data: []map[string]any{
				{"col": 10},
				{"col": "not a number"},
			},
			wantCols: map[string]SQLType{
				"col": Text,
			},
		},
		{
			name:      "Missing keys",
			tableName: "sparse",
			data: []map[string]any{
				{"a": 1},
				{"b": "hello"},
			},
			wantCols: map[string]SQLType{
				"a": Integer,
				"b": Text,
			},
		},
		{
			name:      "Blobs",
			tableName: "files",
			data: []map[string]any{
				{"content": []byte{0x00, 0x01}},
			},
			wantCols: map[string]SQLType{
				"content": Blob,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := InferTable(tt.tableName, tt.data)
			require.NoError(t, err)
			assert.Equal(t, tt.tableName, got.Name)
			assert.Len(t, got.Columns, len(tt.wantCols))

			for _, col := range got.Columns {
				wantType, ok := tt.wantCols[col.Name]
				assert.True(t, ok, "Unexpected column %s", col.Name)
				assert.Equal(t, wantType, col.Type, "Column %s type mismatch", col.Name)
			}
		})
	}
}

func TestJSONSchemaInference(t *testing.T) {
	data := []map[string]any{
		{
			"id": 1,
			"profile": map[string]any{
				"name": "Alice",
				"age":  30,
				"tags": []string{"developer", "golang"},
			},
		},
	}

	table, err := InferTable("users", data)
	require.NoError(t, err)

	sql := table.ToSQL()
	assert.Contains(t, sql, "/* JSON Schema:")
	assert.Contains(t, sql, `"type":"object"`)
	assert.True(t, assert.Contains(t, sql, `"properties":{"age":{"type":"integer"}`) || assert.Contains(t, sql, `"age":{"type":"integer"}`))
}

func TestInferJSONSchema_Comprehensive(t *testing.T) {
	tests := []struct {
		name     string
		samples  []any
		validate func(*testing.T, string)
	}{
		{
			name: "Deeply nested object",
			samples: []any{
				map[string]any{
					"a": map[string]any{
						"b": map[string]any{
							"c": 42,
						},
					},
				},
			},
			validate: func(t *testing.T, js string) {
				assert.Contains(t, js, `"type":"object"`)
				assert.Contains(t, js, `"a"`)
				assert.Contains(t, js, `"b"`)
				assert.Contains(t, js, `"c"`)
			},
		},
		{
			name: "Array of objects",
			samples: []any{
				[]any{
					map[string]any{"id": 1},
					map[string]any{"id": 2},
				},
			},
			validate: func(t *testing.T, js string) {
				assert.Contains(t, js, `"type":"array"`)
				assert.Contains(t, js, `"items":{"type":"object"`)
			},
		},
		{
			name: "Mixed types in array (fallback to any)",
			samples: []any{
				[]any{1, "string"},
			},
			validate: func(t *testing.T, js string) {
				assert.Contains(t, js, `"type":"array"`)
			},
		},
		{
			name: "Null handling",
			samples: []any{
				nil,
				map[string]any{"key": "val"},
			},
			validate: func(t *testing.T, js string) {
				assert.Contains(t, js, `"type":"object"`)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := inferJSONSchema(tt.samples)
			require.NotNil(t, schema)
			b, err := json.Marshal(schema)
			require.NoError(t, err)
			tt.validate(t, string(b))
		})
	}
}

func TestDateTimeInference(t *testing.T) {
	now := time.Now()
	data := []map[string]any{
		{"ts": now},
		{"ts": &now},
	}

	table, err := InferTable("timestamps", data)
	require.NoError(t, err)

	var found bool
	for _, col := range table.Columns {
		if col.Name == "ts" {
			assert.Equal(t, DateTime, col.Type)
			found = true
		}
	}
	assert.True(t, found, "Column ts not found")
}

func TestEmptyInference(t *testing.T) {
	table, err := InferTable("empty", []map[string]any{})
	require.NoError(t, err)
	assert.Empty(t, table.Columns)
	assert.Empty(t, table.ToSQL())
}

func TestPromoteMixedToText(t *testing.T) {
	data := []map[string]any{
		{"mixed": 1},
		{"mixed": "string"},
		{"mixed": 1.5},
	}

	table, err := InferTable("mixed_table", data)
	require.NoError(t, err)

	for _, col := range table.Columns {
		if col.Name == "mixed" {
			assert.Equal(t, Text, col.Type)
		}
	}
}

func TestInferenceStability(t *testing.T) {
	data := []map[string]any{
		{"z": 1, "a": 2, "m": 3},
	}

	table, err := InferTable("stable", data)
	require.NoError(t, err)
	require.Len(t, table.Columns, 3)
	assert.Equal(t, "a", table.Columns[0].Name)
	assert.Equal(t, "m", table.Columns[1].Name)
	assert.Equal(t, "z", table.Columns[2].Name)
}

func TestInferSpecialTypes(t *testing.T) {
	data := []map[string]any{
		{"id": uuid.New(), "t": time.Now()},
	}

	table, err := InferTable("time", data)
	require.NoError(t, err)
	assert.NotEmpty(t, table.ToSQL())
}
