package orm

import "reflect"

// NoEntity is the entity type for a model that maps to no row struct.
//
// Most models are declared with DefineTable and name the struct their rows
// scan into, which is what gives the query API its result type. A model
// that exists only to describe a table's shape, whether in a
// migration-only program or in a test that exercises schema extraction,
// has no such struct, and says so by writing Table[NoEntity].
type NoEntity = struct{}

// tableState is the state a model's columns and its Table share.
//
// It is held behind a pointer because Table is embedded by value, so the
// model, the builder that produced it, and every Table copy taken from
// either must observe the same table. Splitting it out is also what lets
// TableBuilder hand out a Table before the columns that will populate it
// exist, which composite-literal evaluation order requires.
type tableState struct {
	name string

	// entity is E's type, and fieldIdx maps each column's name to the
	// index path of the entity field it scans into. Both are set by
	// DefineTable and left zero for a model built by hand, which never
	// scans rows.
	entity   reflect.Type
	fieldIdx map[string][]int

	// relater is the model itself when it names its own relationship keys.
	// It is kept rather than called during declaration because a
	// Relations method routinely mentions another table, which may not
	// have been initialised yet at that point. See relation.resolve.
	relater Relater

	// cols is the model's columns in struct field order, which is the
	// order a generated SELECT lists them in. Scanning is positional and
	// driver.Rows exposes no column names, so this ordering is what ties
	// a result row back to its fields.
	cols []ColumnMeta

	// pk is the primary key columns in declaration order, and identity is
	// the one the database generates, or nil. Both are worked out once by
	// DefineTable rather than on every statement that needs them.
	pk       []ColumnMeta
	identity ColumnMeta
}

// Table gives a model struct its database identity and, through E, the row
// type its queries return. Embed it by value in every model struct:
//
//	type UserModel struct {
//	    Table[User]
//	    ID *IntColumn
//	}
//
// Models are normally built by DefineTable, which fills in the entity
// mapping and binds each column to the table. NewTable builds one directly
// for the cases that need no entity; see NoEntity.
type Table[E any] struct {
	st *tableState
}

// NewTable declares a model's underlying table name.
//
// It performs none of DefineTable's work: columns are not bound to the
// table, and no entity mapping is resolved, so a Table built this way
// describes a schema but cannot back a query. Reach for DefineTable unless
// you specifically want a model with no row type.
func NewTable[E any](name string) Table[E] {
	return Table[E]{st: &tableState{name: name}}
}

// TableName returns the database table name.
//
// It reports "" for a zero-valued Table rather than panicking, so a model
// left partially constructed produces a diagnosable empty name downstream
// instead of a crash at the point of inspection.
func (t Table[E]) TableName() string {
	if t.st == nil {
		return ""
	}
	return t.st.name
}

// primaryKeyColumns returns the table's primary key columns in declaration
// order.
func primaryKeyColumns(st *tableState) []ColumnMeta {
	var pk []ColumnMeta
	for _, c := range st.cols {
		if c.IsPrimaryKey() {
			pk = append(pk, c)
		}
	}
	return pk
}
