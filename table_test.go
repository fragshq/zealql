package zealql

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToSQL(t *testing.T) {
	table := &Table{
		Name: "test_table",
		Columns: []Column{
			{Name: "id", Type: Integer},
			{Name: "name", Type: Text},
		},
	}

	got := table.ToSQL()
	assert.Contains(t, got, `CREATE TABLE IF NOT EXISTS "test_table" (`)
	assert.Contains(t, got, `"id" INTEGER`)
	assert.Contains(t, got, `"name" TEXT`)
}
