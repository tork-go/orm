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

// AlterColumnDefault sets or drops a column's DEFAULT clause. An empty
// Default drops it.
type AlterColumnDefault struct {
	Table   string
	Column  string
	Default string
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
type AddIndex struct {
	Table string
	Index schema.Index
}
type DropIndex struct{ Table, Name string }
type AddCheck struct {
	Table string
	Check schema.Check
}
type DropCheck struct{ Table, Name string }
type AddForeignKey struct {
	Table      string
	ForeignKey schema.ForeignKey
}
type DropForeignKey struct{ Table, Name string }
type CreateEnumType struct{ Enum schema.EnumType }
type DropEnumType struct{ Name string }

// AddEnumValue adds Value to the enum type Name. Before/After are
// mutually exclusive; both empty appends the value at the end of the
// type's current value list.
type AddEnumValue struct{ Name, Value, Before, After string }

func (CreateTable) isOperation()            {}
func (DropTable) isOperation()              {}
func (AddColumn) isOperation()              {}
func (DropColumn) isOperation()             {}
func (AlterColumnType) isOperation()        {}
func (AlterColumnDefault) isOperation()     {}
func (AlterColumnNullability) isOperation() {}
func (AddPrimaryKey) isOperation()          {}
func (DropPrimaryKey) isOperation()         {}
func (AddUnique) isOperation()              {}
func (DropUnique) isOperation()             {}
func (AddIndex) isOperation()               {}
func (DropIndex) isOperation()              {}
func (AddCheck) isOperation()               {}
func (DropCheck) isOperation()              {}
func (AddForeignKey) isOperation()          {}
func (DropForeignKey) isOperation()         {}
func (CreateEnumType) isOperation()         {}
func (DropEnumType) isOperation()           {}
func (AddEnumValue) isOperation()           {}
