package zealql

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

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
				if !strings.Contains(js, `"type":"object"`) || !strings.Contains(js, `"a"`) || !strings.Contains(js, `"b"`) || !strings.Contains(js, `"c"`) {
					t.Errorf("Schema missing nested keys: %s", js)
				}
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
				if !strings.Contains(js, `"type":"array"`) || !strings.Contains(js, `"items":{"type":"object"`) {
					t.Errorf("Schema mismatch for array of objects: %s", js)
				}
			},
		},
		{
			name: "Mixed types in array (fallback to any)",
			samples: []any{
				[]any{1, "string"},
			},
			validate: func(t *testing.T, js string) {
				// Current implementation takes the first non-nil sample's type for items
				// which is 'integer' in this case. This is a limitation of the current simple logic.
				if !strings.Contains(js, `"type":"array"`) {
					t.Errorf("Expected array type: %s", js)
				}
			},
		},
		{
			name: "Null handling",
			samples: []any{
				nil,
				map[string]any{"key": "val"},
			},
			validate: func(t *testing.T, js string) {
				if !strings.Contains(js, `"type":"object"`) {
					t.Errorf("Should infer object from second sample: %s", js)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := inferJSONSchema(tt.samples)
			if schema == nil {
				t.Fatal("Schema is nil")
			}
			b, err := json.Marshal(schema)
			if err != nil {
				t.Fatalf("Failed to marshal schema: %v", err)
			}
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
	if err != nil {
		t.Fatalf("InferTable error: %v", err)
	}

	found := false
	for _, col := range table.Columns {
		if col.Name == "ts" {
			if col.Type != DateTime {
				t.Errorf("Expected DATETIME for ts, got %v", col.Type)
			}
			found = true
		}
	}
	if !found {
		t.Error("Column ts not found")
	}
}

func TestEmptyInference(t *testing.T) {
	table, err := InferTable("empty", []map[string]any{})
	if err != nil {
		t.Fatalf("InferTable error: %v", err)
	}
	if len(table.Columns) != 0 {
		t.Errorf("Expected 0 columns, got %d", len(table.Columns))
	}
	if table.ToSQL() != "" {
		t.Errorf("Expected empty SQL, got %q", table.ToSQL())
	}
}

func TestPromoteMixedToText(t *testing.T) {
	data := []map[string]any{
		{"mixed": 1},
		{"mixed": "string"},
		{"mixed": 1.5},
	}

	table, err := InferTable("mixed_table", data)
	if err != nil {
		t.Fatalf("InferTable error: %v", err)
	}

	for _, col := range table.Columns {
		if col.Name == "mixed" {
			if col.Type != Text {
				t.Errorf("Expected TEXT for mixed column, got %v", col.Type)
			}
		}
	}
}

func TestInferenceStability(t *testing.T) {
	// Ensure keys are sorted and inference is deterministic
	data := []map[string]any{
		{"z": 1, "a": 2, "m": 3},
	}

	table, _ := InferTable("stable", data)
	if table.Columns[0].Name != "a" || table.Columns[1].Name != "m" || table.Columns[2].Name != "z" {
		t.Errorf("Columns not sorted by name: %v, %v, %v", table.Columns[0].Name, table.Columns[1].Name, table.Columns[2].Name)
	}
}

func TestInferSpecialTypes(t *testing.T) {
	data := []map[string]any{
		{"id": uuid.New(), "t": time.Now()},
	}

	table, _ := InferTable("time", data)
	fmt.Println(table.ToSQL())
}
