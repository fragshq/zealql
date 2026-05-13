package zealql

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestDatabase(t *testing.T) {
	db, err := NewDatabase()
	require.NoError(t, err)
	defer db.Close()

	data := []map[string]any{
		{"id": 1, "name": "Alice"},
		{"id": 2, "name": "Bob"},
	}

	_, err = db.Insert("users", data)
	require.NoError(t, err)

	results, err := db.Query("SELECT name FROM users WHERE id = 1")
	require.NoError(t, err)

	require.Len(t, results, 1)
	assert.Equal(t, "Alice", results[0]["name"])
}

func TestNestedData(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	data := []map[string]any{
		{
			"id":   1,
			"meta": map[string]any{"color": "red", "tags": []string{"a", "b"}},
			"list": []int{1, 2, 3},
		},
	}

	table, err := InferTable("test_nested", data)
	require.NoError(t, err)

	_, err = db.Exec(table.ToSQL())
	require.NoError(t, err)

	err = table.Insert(db, data)
	require.NoError(t, err)

	var metaStr, listStr string
	err = db.QueryRow("SELECT meta, list FROM test_nested WHERE id = 1").Scan(&metaStr, &listStr)
	require.NoError(t, err)

	assert.Contains(t, metaStr, `"color":"red"`)
	assert.Equal(t, "[1,2,3]", listStr)
}

func TestJsonSelection(t *testing.T) {
	db, err := NewDatabase()
	require.NoError(t, err)
	defer db.Close()

	data := []map[string]any{
		{
			"id": 1,
			"profile": map[string]any{
				"name": "Alice",
				"age":  30,
			},
		},
		{
			"id": 2,
			"profile": map[string]any{
				"name": "Bob",
				"age":  30,
			},
		},
		{
			"id":      3,
			"profile": nil,
		},
	}
	_, err = db.Insert("users", data)
	require.NoError(t, err)

	res, err := db.Query("SELECT * FROM users")
	require.NoError(t, err)

	p, ok := res[0]["profile"].(map[string]any)
	assert.True(t, ok, "JSON type mismatch")
	assert.Equal(t, "Alice", p["name"])
}

func TestRelationalSystem(t *testing.T) {
	db, err := NewDatabase()
	require.NoError(t, err)
	defer db.Close()

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

	booksData := []map[string]any{
		{"id": 101, "author_id": 1, "title": "Harry Potter"},
		{"id": 102, "author_id": 1, "title": "Fantastic Beasts"},
		{"id": 103, "author_id": 2, "title": "A Game of Thrones"},
	}

	authorTable, err := db.Insert("authors", authorsData)
	require.NoError(t, err)

	_, err = db.Insert("books", booksData)
	require.NoError(t, err)

	authorSQL := authorTable.ToSQL()
	assert.Contains(t, authorSQL, "/* JSON Schema:")
	assert.Contains(t, authorSQL, "nationality")

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
	require.NoError(t, err)

	assert.Len(t, results, 4)

	assert.Equal(t, "J.K. Rowling", results[0]["author_name"])
	assert.Equal(t, "British", results[0]["nationality"])
	assert.Equal(t, "Harry Potter", results[0]["book_title"])

	assert.Equal(t, "George R.R. Martin", results[2]["author_name"])
	assert.Equal(t, "American", results[2]["nationality"])
	assert.Equal(t, "A Game of Thrones", results[2]["book_title"])

	assert.Equal(t, "Anonymous Author", results[3]["author_name"])
	assert.Nil(t, results[3]["book_title"])
}

func TestDatabaseClone(t *testing.T) {
	src, err := NewDatabase()
	require.NoError(t, err)
	defer src.Close()

	data := []map[string]any{
		{"id": 1, "name": "Alice", "meta": map[string]any{"color": "blue"}},
	}
	_, err = src.Insert("users", data)
	require.NoError(t, err)

	dst, err := src.Clone()
	require.NoError(t, err)
	defer dst.Close()

	results, err := dst.Query("SELECT * FROM users")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Alice", results[0]["name"])

	table, ok := dst.GetTable("users")
	assert.True(t, ok)
	assert.Equal(t, "users", table.Name)

	_, err = dst.db.Exec("INSERT INTO users (id, name) VALUES (2, 'Bob')")
	require.NoError(t, err)

	srcResults, err := src.Query("SELECT COUNT(*) as count FROM users")
	require.NoError(t, err)
	assert.Equal(t, int64(1), srcResults[0]["count"])

	dstResults, err := dst.Query("SELECT COUNT(*) as count FROM users")
	require.NoError(t, err)
	assert.Equal(t, int64(2), dstResults[0]["count"])
}

func TestDatabaseIsolation(t *testing.T) {
	db1, err := NewDatabase()
	require.NoError(t, err)
	defer db1.Close()

	db2, err := NewDatabase()
	require.NoError(t, err)
	defer db2.Close()

	data1 := []map[string]any{
		{"id": 1, "name": "Alice"},
	}

	_, err = db1.Insert("users", data1)
	require.NoError(t, err)

	_, err = db1.Query("SELECT * FROM users")
	require.NoError(t, err)

	_, err = db2.Query("SELECT * FROM users")
	assert.Error(t, err, "Table 'users' unexpectedly found in db2 (isolation failure)")
}

func TestInsertSpecialTypes_Integration(t *testing.T) {
	db, err := NewDatabase()
	require.NoError(t, err)
	defer db.Close()

	tx := time.Now()
	id := uuid.New()
	authorsData := []map[string]any{
		{
			"id":      id,
			"release": tx,
			"name":    "john snow",
		},
	}
	_, err = db.Insert("authors", authorsData)
	require.NoError(t, err)

	res, err := db.Query("SELECT * FROM authors")
	require.NoError(t, err)
	require.Len(t, res, 1)

	// Note: depending on how sqlite driver handles time, it might lose some precision or return as string/int
	// but based on the original test it expects time.Time
	assert.True(t, tx.Equal(res[0]["release"].(time.Time)))
	// uuid should also match if driver handles it
	assert.Equal(t, id.String(), res[0]["id"].(string))
}

func TestInsertCompatibility(t *testing.T) {
	db, err := NewDatabase()
	require.NoError(t, err)
	defer db.Close()

	data1 := []map[string]any{
		{"id": 1, "name": "Alice"},
	}
	_, err = db.Insert("users", data1)
	require.NoError(t, err)

	// 1. Identical structure - should work and append
	data2 := []map[string]any{
		{"id": 2, "name": "Bob"},
	}
	_, err = db.Insert("users", data2)
	require.NoError(t, err)

	results, err := db.Query("SELECT COUNT(*) as count FROM users")
	require.NoError(t, err)
	assert.Equal(t, int64(2), results[0]["count"])

	// 2. Different structure (new column) - should fail
	data3 := []map[string]any{
		{"id": 3, "name": "Charlie", "age": 30},
	}
	_, err = db.Insert("users", data3)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not match existing schema")

	// 3. Subset structure (missing column) - should fail
	data4 := []map[string]any{
		{"id": 4},
	}
	_, err = db.Insert("users", data4)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not match existing schema")

	// 4. Different types for same key - should fail
	data5 := []map[string]any{
		{"id": "five", "name": "Eve"},
	}
	_, err = db.Insert("users", data5)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not match existing schema")

	// Verify final count is still 2
	results, err = db.Query("SELECT COUNT(*) as count FROM users")
	require.NoError(t, err)
	assert.Equal(t, int64(2), results[0]["count"])

	// Verify metadata remains intact
	table, ok := db.GetTable("users")
	require.True(t, ok)
	require.Len(t, table.Columns, 2)
}
