package zealql

import (
	"fmt"
	"testing"
)

func BenchmarkDatabaseClone(b *testing.B) {
	sizes := []int{100, 1000, 10000} // Number of rows to simulate different DB sizes

	for _, size := range sizes {
		b.Run(fmt.Sprintf("Rows-%d", size), func(b *testing.B) {
			db, _ := NewDatabase()
			data := make([]map[string]any, size)
			for i := 0; i < size; i++ {
				data[i] = map[string]any{
					"id":   i,
					"name": "User Name",
					"meta": map[string]any{
						"key1": "value1",
						"key2": []int{1, 2, 3, 4, 5},
					},
				}
			}
			db.CreateTableFromMaps("bench", data)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				clone, err := db.Clone()
				if err != nil {
					b.Fatal(err)
				}
				clone.Close()
			}
		})
	}
}
