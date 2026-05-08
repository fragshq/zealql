package zealql

import (
	"fmt"
	"testing"
)

// generateTestData creates a slice of maps for benchmarking
func generateTestData(rows int) []map[string]any {
	data := make([]map[string]any, rows)
	for i := 0; i < rows; i++ {
		data[i] = map[string]any{
			"id":     i,
			"name":   fmt.Sprintf("User-%d", i),
			"score":  float64(i) * 1.5,
			"active": i%2 == 0,
			"meta": map[string]any{
				"age":    30,
				"tags":   []string{"tag1", "tag2"},
				"nested": map[string]any{"a": 1, "b": 2},
			},
		}
	}
	return data
}

func BenchmarkInferTable(b *testing.B) {
	sizes := []int{10, 100, 1000}
	for _, size := range sizes {
		data := generateTestData(size)
		b.Run(fmt.Sprintf("Rows-%d", size), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := InferTable("bench", data)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkInsert(b *testing.B) {
	sizes := []int{10, 100, 1000}
	for _, size := range sizes {
		data := generateTestData(size)
		db, _ := NewDatabase()
		table, _ := InferTable("bench", data)
		_, _ = db.db.Exec(table.ToSQL())

		b.Run(fmt.Sprintf("Rows-%d", size), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				err := table.Insert(db.db, data)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkQuery(b *testing.B) {
	sizes := []int{10, 100, 1000}
	for _, size := range sizes {
		data := generateTestData(size)
		db, _ := NewDatabase()
		_, _ = db.CreateTableFromMaps("bench", data)

		b.Run(fmt.Sprintf("Rows-%d", size), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := db.Query("SELECT * FROM bench")
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkFullCycle(b *testing.B) {
	// Measures NewDatabase -> CreateTable (Infer + Create + Insert) -> Query -> Close
	size := 100
	data := generateTestData(size)

	b.Run("Cycle-100", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			db, _ := NewDatabase()
			_, _ = db.CreateTableFromMaps("bench", data)
			_, _ = db.Query("SELECT * FROM bench WHERE id = 50")
			_ = db.Close()
		}
	})
}
