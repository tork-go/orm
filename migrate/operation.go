package migrate

import "github.com/tork-go/orm/schema"

// Operation is one schema change. Each kind is its own type rather than a
// single struct with a kind enum and a pile of optional fields, so every
// operation is self-documenting and there's no way to populate the wrong
// field for a given kind.
type Operation interface {
	isOperation()
}

type CreateTable struct{ Table schema.Table }
type DropTable struct{ Table string }
type AddColumn struct {
	Table  string
	Column schema.Column
}
type DropColumn struct{ Table, Column string }
type AlterColumnType struct {
	Table  string
	Column schema.Column
}
type AlterColumnNullability struct {
	Table, Column string
	NotNull       bool
}
type AddPrimaryKey struct {
	Table      string
	PrimaryKey schema.PrimaryKey
}
type DropPrimaryKey struct{ Table, Name string }
type AddUnique struct {
	Table  string
	Unique schema.UniqueConstraint
}
type DropUnique struct{ Table, Name string }
type AddForeignKey struct {
	Table      string
	ForeignKey schema.ForeignKey
}
type DropForeignKey struct{ Table, Name string }

func (CreateTable) isOperation()            {}
func (DropTable) isOperation()              {}
func (AddColumn) isOperation()              {}
func (DropColumn) isOperation()             {}
func (AlterColumnType) isOperation()        {}
func (AlterColumnNullability) isOperation() {}
func (AddPrimaryKey) isOperation()          {}
func (DropPrimaryKey) isOperation()         {}
func (AddUnique) isOperation()              {}
func (DropUnique) isOperation()             {}
func (AddForeignKey) isOperation()          {}
func (DropForeignKey) isOperation()         {}
