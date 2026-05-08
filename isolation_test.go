package zealql

import "testing"

func TestDatabaseIsolation(t *testing.T) {
	// Create first database
	db1, err := NewDatabase()
	if err != nil {
		t.Fatalf("Failed to create db1: %v", err)
	}
	defer db1.Close()

	// Create second database
	db2, err := NewDatabase()
	if err != nil {
		t.Fatalf("Failed to create db2: %v", err)
	}
	defer db2.Close()

	// Define data for db1
	data1 := []map[string]any{
		{"id": 1, "name": "Alice"},
	}

	// Create table in db1
	_, err = db1.CreateTableFromMaps("users", data1)
	if err != nil {
		t.Fatalf("Failed to create table in db1: %v", err)
	}

	// Verify table exists in db1
	_, err = db1.Query("SELECT * FROM users")
	if err != nil {
		t.Errorf("Query failed in db1: %v", err)
	}

	// Verify table DOES NOT exist in db2
	_, err = db2.Query("SELECT * FROM users")
	if err == nil {
		t.Error("Table 'users' unexpectedly found in db2 (isolation failure)")
	} else {
		t.Logf("Correctly received error from db2: %v", err)
	}
}
