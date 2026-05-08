package zealql

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
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
			if err != nil {
				t.Fatalf("InferTable() error = %v", err)
			}

			if got.Name != tt.tableName {
				t.Errorf("Table name = %v, want %v", got.Name, tt.tableName)
			}

			if len(got.Columns) != len(tt.wantCols) {
				t.Errorf("Got %d columns, want %d", len(got.Columns), len(tt.wantCols))
			}

			for _, col := range got.Columns {
				wantType, ok := tt.wantCols[col.Name]
				if !ok {
					t.Errorf("Unexpected column %s", col.Name)
					continue
				}
				if col.Type != wantType {
					t.Errorf("Column %s type = %v, want %v", col.Name, col.Type, wantType)
				}
			}
		})
	}
}

func TestToSQL(t *testing.T) {
	table := &Table{
		Name: "test_table",
		Columns: []Column{
			{Name: "id", Type: Integer},
			{Name: "name", Type: Text},
		},
	}

	got := table.ToSQL()
	want := `CREATE TABLE IF NOT EXISTS "test_table" (`
	if !strings.Contains(got, want) {
		t.Errorf("ToSQL() = %v, want to contain %v", got, want)
	}
	if !strings.Contains(got, `"id" INTEGER`) {
		t.Errorf("ToSQL() missing id column")
	}
	if !strings.Contains(got, `"name" TEXT`) {
		t.Errorf("ToSQL() missing name column")
	}
}

func TestDatabase(t *testing.T) {
	db, err := NewDatabase()
	if err != nil {
		t.Fatalf("NewDatabase() error = %v", err)
	}
	defer db.Close()

	data := []map[string]any{
		{"id": 1, "name": "Alice"},
		{"id": 2, "name": "Bob"},
	}

	_, err = db.CreateTableFromMaps("users", data)
	if err != nil {
		t.Fatalf("CreateTable() error = %v", err)
	}

	results, err := db.Query("SELECT name FROM users WHERE id = 1")
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	if results[0]["name"] != "Alice" {
		t.Errorf("Expected 'Alice', got '%v'", results[0]["name"])
	}
}

func TestNestedData(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	data := []map[string]any{
		{
			"id":   1,
			"meta": map[string]any{"color": "red", "tags": []string{"a", "b"}},
			"list": []int{1, 2, 3},
		},
	}

	table, err := InferTable("test_nested", data)
	if err != nil {
		t.Fatalf("InferTable() error = %v", err)
	}

	_, err = db.Exec(table.ToSQL())
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	err = table.Insert(db, data)
	if err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	var metaStr, listStr string
	err = db.QueryRow("SELECT meta, list FROM test_nested WHERE id = 1").Scan(&metaStr, &listStr)
	if err != nil {
		t.Fatalf("failed to query nested data: %v", err)
	}

	// Verify JSON content
	if !strings.Contains(metaStr, `"color":"red"`) {
		t.Errorf("meta JSON missing color: %s", metaStr)
	}
	if listStr != "[1,2,3]" {
		t.Errorf("list JSON mismatch: %s", listStr)
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
	if err != nil {
		t.Fatalf("InferTable() error = %v", err)
	}

	sql := table.ToSQL()
	if !strings.Contains(sql, "/* JSON Schema:") {
		t.Errorf("SQL missing JSON Schema comment")
	}
	if !strings.Contains(sql, `"type":"object"`) {
		t.Errorf("JSON Schema missing object type")
	}
	if !strings.Contains(sql, `"properties":{"age":{"type":"integer"}`) && !strings.Contains(sql, `"age":{"type":"integer"}`) {
		t.Errorf("JSON Schema missing age property")
	}
}

func TestRelationalSystem(t *testing.T) {
	db, err := NewDatabase()
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer db.Close()

	// 1. Dataset for authors (with a complex column)
	authorsData := []map[string]any{
		{
			"id":   1,
			"name": "J.K. Rowling",
			"info": map[string]any{
				"nationality": "British",
				"born":        1965,
				"awards":      []string{"Hugo", "Locus"},
			},
		},
		{
			"id":   2,
			"name": "George R.R. Martin",
			"info": map[string]any{
				"nationality": "American",
				"born":        1948,
			},
		},
		{
			"id":   3,
			"name": "Anonymous Author",
			"info": nil,
		},
	}

	// 2. Dataset for books
	booksData := []map[string]any{
		{"id": 101, "author_id": 1, "title": "Harry Potter"},
		{"id": 102, "author_id": 1, "title": "Fantastic Beasts"},
		{"id": 103, "author_id": 2, "title": "A Game of Thrones"},
	}

	// 3. Create tables and ingest data
	authorTable, err := db.CreateTableFromMaps("authors", authorsData)
	if err != nil {
		t.Fatalf("failed to create authors table: %v", err)
	}

	_, err = db.CreateTableFromMaps("books", booksData)
	if err != nil {
		t.Fatalf("failed to create books table: %v", err)
	}

	// Verify JSON Schema comment in authors table
	authorSQL := authorTable.ToSQL()
	if !strings.Contains(authorSQL, "/* JSON Schema:") || !strings.Contains(authorSQL, "nationality") {
		t.Errorf("Authors table SQL missing proper JSON Schema comment for 'info' column")
	}

	// 4. Perform a LEFT JOIN query with JSON extraction
	// We want to see all authors, their nationality from the JSON field, and their book titles (if any)
	query := `
		SELECT 
			a.name as author_name,
			a.info ->> '$.nationality' as nationality,
			b.title as book_title
		FROM authors a
		LEFT JOIN books b ON a.id = b.author_id
		ORDER BY a.id ASC, b.id ASC
	`

	results, err := db.Query(query)
	if err != nil {
		t.Fatalf("Relational query failed: %v", err)
	}

	// 5. Assert results
	expectedCount := 4 // 2 for Rowling, 1 for Martin, 1 for Anonymous
	if len(results) != expectedCount {
		t.Errorf("Expected %d results from LEFT JOIN, got %d", expectedCount, len(results))
	}

	// Verify Rowling's data (joined with multiple books)
	if results[0]["author_name"] != "J.K. Rowling" || results[0]["nationality"] != "British" || results[0]["book_title"] != "Harry Potter" {
		t.Errorf("Row 0 mismatch: %+v", results[0])
	}

	// Verify Martin's data
	if results[2]["author_name"] != "George R.R. Martin" || results[2]["nationality"] != "American" || results[2]["book_title"] != "A Game of Thrones" {
		t.Errorf("Row 2 mismatch: %+v", results[2])
	}

	// Verify Anonymous (LEFT JOIN result should have nil for book_title)
	if results[3]["author_name"] != "Anonymous Author" || results[3]["book_title"] != nil {
		t.Errorf("Row 3 (Anonymous) mismatch: %+v", results[3])
	}
}
