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
)

// ColumnType is a column's type, dialect-agnostic. Length only applies
// when Kind is KindVarchar.
type ColumnType struct {
	Kind   Kind
	Length int
}

// Equal reports whether two column types are the same for diffing
// purposes. Length is only compared for KindVarchar, where it is
// meaningful.
func (t ColumnType) Equal(other ColumnType) bool {
	if t.Kind != other.Kind {
		return false
	}
	if t.Kind == KindVarchar {
		return t.Length == other.Length
	}
	return true
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

// ForeignKey is a foreign key constraint on one or more columns.
type ForeignKey struct {
	Name              string
	Columns           []string
	ReferencedTable   string
	ReferencedColumns []string
}

// Index is a plain (non-unique) index on one or more columns. A unique
// constraint already provides an index in every dialect Tork targets, so
// a column that is both unique and indexed produces one UniqueConstraint,
// never an Index alongside it.
type Index struct {
	Name    string
	Columns []string
}

// Table is a single table: its columns and constraints.
type Table struct {
	Name        string
	Columns     []Column
	PrimaryKey  *PrimaryKey
	Uniques     []UniqueConstraint
	Indexes     []Index
	ForeignKeys []ForeignKey
}

// Schema is a set of tables.
type Schema struct {
	Tables []Table
}
