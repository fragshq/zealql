package zealql

import "testing"

func TestDatabaseClone(t *testing.T) {
	// 1. Setup source database with data
	src, _ := NewDatabase()
	defer src.Close()

	data := []map[string]any{
		{"id": 1, "name": "Alice", "meta": map[string]any{"color": "blue"}},
	}
	src.CreateTableFromMaps("users", data)

	// 2. Clone the database
	dst, err := src.Clone()
	if err != nil {
		t.Fatalf("Clone failed: %v", err)
	}
	defer dst.Close()

	// 3. Verify data in clone
	results, err := dst.Query("SELECT * FROM users")
	if err != nil {
		t.Fatalf("Query on clone failed: %v", err)
	}
	if len(results) != 1 || results[0]["name"] != "Alice" {
		t.Errorf("Cloned data mismatch: %+v", results)
	}

	// 4. Verify metadata in clone
	table, ok := dst.GetTable("users")
	if !ok || table.Name != "users" {
		t.Error("Metadata (tables map) not correctly cloned")
	}

	// 5. Verify isolation (modifying clone shouldn't affect source)
	dst.db.Exec("INSERT INTO users (id, name) VALUES (2, 'Bob')")

	srcResults, _ := src.Query("SELECT COUNT(*) as count FROM users")
	if srcResults[0]["count"].(int64) != 1 {
		t.Errorf("Source database affected by clone modification: count = %v", srcResults[0]["count"])
	}

	dstResults, _ := dst.Query("SELECT COUNT(*) as count FROM users")
	if dstResults[0]["count"].(int64) != 2 {
		t.Errorf("Clone database doesn't have new record: count = %v", dstResults[0]["count"])
	}
}
