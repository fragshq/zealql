package zealql

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConcurrentAccess mixes queries, inserts into an existing table and inserts into new tables from many
// goroutines. Before the pool was limited to one connection, most of these failed with "no such table", because
// extra connections opened their own empty in-memory database.
func TestConcurrentAccess(t *testing.T) {
	db, err := NewDatabase()
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Insert("users", []map[string]any{{"id": 0, "name": "seed"}})
	require.NoError(t, err)

	const goroutines = 60
	errs := make(chan error, goroutines)
	wg := sync.WaitGroup{}
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var err error
			switch i % 3 {
			case 0:
				_, err = db.Query("SELECT * FROM users")
			case 1:
				_, err = db.Insert("users", []map[string]any{{"id": i, "name": "user"}})
			case 2:
				// several goroutines race to create the same few new tables
				_, err = db.Insert(fmt.Sprintf("table_%d", i%4), []map[string]any{{"id": i}})
			}
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		assert.NoError(t, err)
	}

	res, err := db.Query("SELECT COUNT(*) AS count FROM users")
	require.NoError(t, err)
	assert.Equal(t, int64(1+goroutines/3), res[0]["count"])

	total := int64(0)
	for i := 0; i < 4; i++ {
		res, err := db.Query(fmt.Sprintf("SELECT COUNT(*) AS count FROM table_%d", i))
		require.NoError(t, err)
		total += res[0]["count"].(int64)
	}
	assert.Equal(t, int64(goroutines/3), total)
}

// TestInstancesAreIsolated checks that two databases used concurrently don't share data or block each other.
func TestInstancesAreIsolated(t *testing.T) {
	db1, err := NewDatabase()
	require.NoError(t, err)
	defer db1.Close()
	db2, err := NewDatabase()
	require.NoError(t, err)
	defer db2.Close()

	wg := sync.WaitGroup{}
	for _, db := range []*Database{db1, db2} {
		wg.Add(1)
		go func(db *Database) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				_, err := db.Insert("shared_name", []map[string]any{{"id": i}})
				assert.NoError(t, err)
			}
		}(db)
	}
	wg.Wait()

	for _, db := range []*Database{db1, db2} {
		res, err := db.Query("SELECT COUNT(*) AS count FROM shared_name")
		require.NoError(t, err)
		assert.Equal(t, int64(20), res[0]["count"])
	}
}

// TestInsertIsAtomic checks that a failing row leaves neither a half-filled table nor stale metadata behind.
func TestInsertIsAtomic(t *testing.T) {
	db, err := NewDatabase()
	require.NoError(t, err)
	defer db.Close()

	data := []map[string]any{
		{"id": 1, "meta": map[string]any{"ok": true}},
		{"id": 2, "meta": map[string]any{"bad": make(chan int)}}, // can't be marshaled to JSON
	}
	_, err = db.Insert("items", data)
	require.Error(t, err)

	_, ok := db.GetTable("items")
	assert.False(t, ok)
	_, err = db.Query("SELECT * FROM items")
	assert.Error(t, err, "the table creation must have been rolled back")

	// a valid insert afterwards creates the table from scratch
	_, err = db.Insert("items", data[:1])
	require.NoError(t, err)

	// a failing insert into an existing table leaves the existing rows untouched
	_, err = db.Insert("items", []map[string]any{
		{"id": 3, "meta": map[string]any{"ok": false}},
		{"id": 4, "meta": map[string]any{"bad": make(chan int)}},
	})
	require.Error(t, err)
	res, err := db.Query("SELECT COUNT(*) AS count FROM items")
	require.NoError(t, err)
	assert.Equal(t, int64(1), res[0]["count"])
}

func TestInsertPointers(t *testing.T) {
	db, err := NewDatabase()
	require.NoError(t, err)
	defer db.Close()

	meta := map[string]any{"color": "red"}
	name := "Alice"
	var nilName *string
	_, err = db.Insert("ptrs", []map[string]any{
		{"id": 1, "meta": &meta, "name": &name, "tags": [2]string{"a", "b"}},
		{"id": 2, "meta": &meta, "name": nilName, "tags": [2]string{"c", "d"}},
	})
	require.NoError(t, err)

	res, err := db.Query("SELECT * FROM ptrs ORDER BY id")
	require.NoError(t, err)
	require.Len(t, res, 2)
	assert.Equal(t, map[string]any{"color": "red"}, res[0]["meta"])
	assert.Equal(t, "Alice", res[0]["name"])
	assert.Nil(t, res[1]["name"])
	assert.Equal(t, []any{"c", "d"}, res[1]["tags"])
}

func TestInsertEmptyBatch(t *testing.T) {
	db, err := NewDatabase()
	require.NoError(t, err)
	defer db.Close()

	// an empty batch doesn't define a schema, so a later batch still can
	_, err = db.Insert("events", []map[string]any{})
	require.NoError(t, err)
	_, ok := db.GetTable("events")
	assert.False(t, ok)

	_, err = db.Insert("events", []map[string]any{{"id": 1}})
	require.NoError(t, err)

	// an empty batch into an existing table is a no-op
	table, err := db.Insert("events", []any{})
	require.NoError(t, err)
	assert.Equal(t, "events", table.Name)
}

func TestCloneWhileWriting(t *testing.T) {
	db, err := NewDatabase()
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Insert("users", []map[string]any{{"id": 0}})
	require.NoError(t, err)

	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 1; i <= 50; i++ {
			_, err := db.Insert("users", []map[string]any{{"id": i}})
			assert.NoError(t, err)
		}
	}()
	for i := 0; i < 10; i++ {
		clone, err := db.Clone()
		require.NoError(t, err)
		res, err := clone.Query("SELECT COUNT(*) AS count FROM users")
		require.NoError(t, err)
		assert.GreaterOrEqual(t, res[0]["count"].(int64), int64(1))
		require.NoError(t, clone.Close())
	}
	wg.Wait()
}
