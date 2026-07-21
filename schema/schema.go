package schema

// Kind is a database-agnostic column type.
type Kind int

const (
	KindBoolean Kind = iota
	KindInteger
	KindBigInteger
	KindFloat
	KindDouble
	KindVarchar
	KindText
	KindTimestamp
	KindUUID
	KindNumeric
	KindArray
	KindJSON
	KindJSONB
	KindEnum
)

// ColumnType is a column's type, dialect-agnostic. Length only applies
// when Kind is KindVarchar. Precision and Scale only apply when Kind is
// KindNumeric. TypeName only applies when Kind is KindEnum, it is the
// enum type's own name. Elem only applies when Kind is KindArray, it is
// the array's element type.
type ColumnType struct {
	Kind      Kind
	Length    int
	Precision int
	Scale     int
	TypeName  string
	Elem      *ColumnType
}

// Equal reports whether two column types are the same for diffing
// purposes. Length, Precision/Scale, TypeName, and Elem are only compared
// for the Kind they're meaningful on.
func (t ColumnType) Equal(other ColumnType) bool {
	if t.Kind != other.Kind {
		return false
	}
	switch t.Kind {
	case KindVarchar:
		return t.Length == other.Length
	case KindNumeric:
		return t.Precision == other.Precision && t.Scale == other.Scale
	case KindEnum:
		return t.TypeName == other.TypeName
	case KindArray:
		if t.Elem == nil || other.Elem == nil {
			return t.Elem == other.Elem
		}
		return t.Elem.Equal(*other.Elem)
	default:
		return true
	}
}

// Column is a single column in a table.
type Column struct {
	Name          string
	Type          ColumnType
	NotNull       bool
	ServerDefault string // raw SQL expression for a DEFAULT clause; empty means none
}

// PrimaryKey is a table's primary key constraint. Name is empty for a
// model-derived desired schema and filled in by a driver's introspection
// once the constraint actually exists in a live database.
type PrimaryKey struct {
	Name    string
	Columns []string
}

// UniqueConstraint is a unique constraint on one or more columns.
type UniqueConstraint struct {
	Name    string
	Columns []string
}

// ForeignKeyAction is a referential action Postgres runs when a
// referenced row is deleted or updated. This is a distinct type from
// orm.ForeignKeyAction, not a reuse: driver/postgres builds ForeignKey
// values from introspection alone, with no import of orm, and schema
// itself must stay the only package importing orm in that direction.
type ForeignKeyAction int

const (
	ActionNoAction ForeignKeyAction = iota
	ActionCascade
	ActionSetNull
	ActionSetDefault
	ActionRestrict
)

// ForeignKey is a foreign key constraint on one or more columns.
type ForeignKey struct {
	Name              string
	Columns           []string
	ReferencedTable   string
	ReferencedColumns []string
	OnDelete          ForeignKeyAction
	OnUpdate          ForeignKeyAction
}

// Index is a plain (non-unique) index on one or more columns. A unique
// constraint already provides an index in every dialect Tork targets, so
// a column that is both unique and indexed produces one UniqueConstraint,
// never an Index alongside it.
type Index struct {
	Name    string
	Columns []string

	// Expressions are expression keys, such as lower(email). An index has
	// either column keys or expression keys, never a mix: Postgres allows
	// interleaving them, but nothing here records where each key sat, so
	// such an index is left alone by introspection rather than
	// misrepresented.
	Expressions []string

	// Where is a partial index's predicate, empty for a full index.
	Where string
}

// Check is a table-level CHECK constraint. Expression is a raw SQL
// boolean expression, not parsed or validated by Tork.
type Check struct {
	Name       string
	Expression string
}

// Table is a single table: its columns and constraints.
type Table struct {
	Name        string
	Columns     []Column
	PrimaryKey  *PrimaryKey
	Uniques     []UniqueConstraint
	Indexes     []Index
	Checks      []Check
	ForeignKeys []ForeignKey
}

// EnumType is a Postgres native enum type: CREATE TYPE <Name> AS ENUM
// (...). It is schema-global rather than table-scoped, since a single
// enum type can be shared by columns across multiple tables.
type EnumType struct {
	Name   string
	Values []string // declared order; Postgres enum ordering is significant
}

// Schema is a set of tables and the enum types they use.
type Schema struct {
	Tables    []Table
	EnumTypes []EnumType
}
