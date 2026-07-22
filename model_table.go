package orm

import (
	"reflect"
	"sync"
)

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

	// derived marks a table whose rows come from a query rather than from
	// storage, declared by DefineDerived. It is what tells the compiler to
	// render the FROM clause as a subquery under this name instead of as
	// the name alone. See model_derived.go.
	derived bool

	// aliasOf is the stored table an alias stands for, and "" for a table
	// under its own name. It is what fromClause renders as `"employees" AS
	// "mgr"`, and what relationship resolution matches a foreign key
	// against, since a key references the stored table rather than any
	// second name given to it here. See model_alias.go.
	aliasOf string

	// rebuild re-runs the model's own build function against another state,
	// which is how Alias produces a second handle on this table. It is nil
	// for a model built by NewTable or DefineDerived, neither of which has
	// a build function to re-run.
	rebuild func(*tableState) Model

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

	// scoper is the model itself when it declares a default scope. Stashed
	// rather than called during declaration; see Scoper.
	scoper Scoper

	// softDelete is the column Delete and DeleteAll stamp instead of
	// removing the row, and every read excludes rows where it is set
	// unless the query is Unscoped. nil when the table declares none.
	softDelete ColumnMeta

	// scopeOnce and scopeVal cache the table's combined default-scope
	// predicate; see defaultScope.
	scopeOnce sync.Once
	scopeVal  Predicate
}

// storageName is the name the table is stored under: an alias's real
// table, or the table's own name when it is not one.
//
// Everything that names a table in the finished statement — the FROM
// clause, a JOIN, the qualification on a column — uses name, since that is
// what the statement calls it. This is for the two questions that are about
// storage rather than about the statement: which table a foreign key
// references, and which table a write would touch.
func (st *tableState) storageName() string {
	if st.aliasOf != "" {
		return st.aliasOf
	}
	return st.name
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

// state returns the table this identity was built from.
//
// It is how Alias reaches the state behind a Model, which the exported
// Model interface deliberately does not expose: TableName is all anything
// outside this package needs, and a caller able to reach the state could
// rename a table every column has already been bound to. See stateOf.
func (t Table[E]) state() *tableState { return t.st }

// stateHolder is the read side of the table identity a model embeds,
// satisfied through promotion by every model carrying a Table or a
// DerivedTable.
type stateHolder interface {
	state() *tableState
}

// stateOf returns the table state behind a model, or nil for one built
// without either identity — which a caller can write, since Model asks only
// for a TableName.
//
// A nil model pointer reports nil rather than panicking. The identity is
// embedded by value, so reaching its method through a nil pointer would
// dereference it, and a caller who passed a model that was never declared
// deserves the error naming that rather than a nil dereference.
func stateOf(m Model) *tableState {
	h, ok := any(m).(stateHolder)
	if !ok {
		return nil
	}
	if v := reflect.ValueOf(m); v.Kind() == reflect.Pointer && v.IsNil() {
		return nil
	}
	return h.state()
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
