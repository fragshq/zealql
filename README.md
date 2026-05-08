# ZealQL

ZealQL is a lightweight Go library that provides an in-memory SQLite wrapper with **automatic schema inference**. It allows you to transform unstructured or semi-structured data (like slices of maps) into fully functional relational tables instantly.

## Features

- **Automatic Schema Inference**: Just pass a slice of `map[string]any` and ZealQL will determine the best SQLite types for your columns.
- **Type Promotion**: Safely handles mixed data types (e.g., promoting a column from `INTEGER` to `REAL` if decimal values are encountered).
- **Embedded JSON Support**: Complex Go structures (maps, slices) are automatically marshaled to JSON strings for storage.
- **JSON Schema Documentation**: Automatically generates and embeds JSON Schemas as SQL comments in the `CREATE TABLE` statement for structured columns.
- **Deep Clonability**: Efficiently clone an entire database instance (data + schema) into a new isolated instance using SQLite's serialization API.
- **Pointer Handling**: Correctly dereferences pointers during inference and ingestion.
- **Fluent Querying**: Simplifies common SQLite operations while maintaining the power of standard SQL.

## Installation

```bash
go get github.com/theirish81/zealql
```

*Note: ZealQL uses `modernc.org/sqlite`, a pure Go SQLite implementation, so no CGO is required.*

## Quick Start

```go
package main

import (
	"fmt"
	"log"
	"github.com/theirish81/zealql"
)

func main() {
	// 1. Initialize an in-memory database
	db, _ := zealql.NewDatabase()
	defer db.Close()

	// 2. Prepare semi-structured data
	data := []map[string]any{
		{
			"id": 1, 
			"name": "Alice", 
			"meta": map[string]any{"age": 30, "tags": []string{"go", "sql"}},
		},
		{
			"id": 2, 
			"name": "Bob", 
			"meta": map[string]any{"age": 25, "active": true},
		},
	}

	// 3. Create table (infers schema and inserts data automatically)
	table, _ := db.CreateTable("users", data)

	// Print the inferred SQL schema
	fmt.Println(table.ToSQL())

	// 4. Query with JSON extraction
	query := "SELECT name, meta ->> '$.age' as age FROM users WHERE id = 1"
	results, _ := db.Query(query)

	for _, row := range results {
		fmt.Printf("User: %s, Age: %v\n", row["name"], row["age"])
	}
}
```

## How It Works

### Schema Inference
ZealQL scans your data to find the most specific common type for every key.
- `int` -> `INTEGER`
- `float64` -> `REAL`
- `string` -> `TEXT`
- `bool` -> `INTEGER` (0 or 1)
- `time.Time` -> `DATETIME`
- `map`/`slice` -> `TEXT` (stored as JSON)

### JSON Schema Comments
When a column is inferred as a JSON field, ZealQL generates a JSON Schema representing the structure of that data and attaches it as a SQL comment:

```sql
CREATE TABLE IF NOT EXISTS users (
  id INTEGER,
  name TEXT,
  meta TEXT /* JSON Schema: {"type":"object","properties":{"age":{"type":"integer"},...}} */
);
```

## License

MIT
