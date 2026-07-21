package orm_test

import (
	"testing"

	"github.com/tork-go/orm"
)

func TestTable_TableName(t *testing.T) {
	tests := []struct {
		name      string
		tableName string
	}{
		{name: "simple name", tableName: "users"},
		{name: "snake_case name", tableName: "user_posts"},
		{name: "empty name", tableName: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tbl := orm.NewTable[orm.NoEntity](tt.tableName)
			if got := tbl.TableName(); got != tt.tableName {
				t.Errorf("TableName() = %q, want %q", got, tt.tableName)
			}
		})
	}
}

// TestTable_ValueEmbedding proves Table can be embedded by value in a model
// struct and still promote TableName() through a pointer to that model, as
// used throughout the target model-declaration API.
func TestTable_ValueEmbedding(t *testing.T) {
	type Model struct {
		orm.Table[orm.NoEntity]
	}

	m := &Model{Table: orm.NewTable[orm.NoEntity]("widgets")}
	if got, want := m.TableName(), "widgets"; got != want {
		t.Errorf("TableName() = %q, want %q", got, want)
	}
}
