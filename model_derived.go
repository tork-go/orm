package orm

import (
	"fmt"
	"reflect"
)

// DerivedTable gives a model the identity of a table whose rows come from a
// query rather than from storage. Embed it by value in the model, in place
// of Table:
//
//	type RankedModel struct {
//	    DerivedTable[Ranked]
//	    Username *StringColumn
//	    Rank     *BigIntColumn
//	}
//
// Its columns are ordinary typed columns, so everything a query can do with
// a stored table's columns it can do with these. What differs is where the
// rows come from, which From says; see DefineDerived.
type DerivedTable[E any] struct {
	st *tableState
}

// TableName returns the name the derived table is given in the statement,
// which is the alias its subquery is wrapped under.
//
// It reports "" for a zero-valued DerivedTable rather than panicking, for
// the reason Table.TableName gives.
func (d DerivedTable[E]) TableName() string {
	if d.st == nil {
		return ""
	}
	return d.st.name
}

// state returns the table this identity was built from, so a derived model
// reaching Alias is recognised and refused rather than silently aliased.
func (d DerivedTable[E]) state() *tableState { return d.st }

// DefineDerived declares a model for a table whose rows come from a query.
//
//	type Ranked struct {
//	    Username string
//	    Rank     int64
//	}
//
//	type RankedModel struct {
//	    orm.DerivedTable[Ranked]
//	    Username *orm.StringColumn
//	    Rank     *orm.BigIntColumn
//	}
//
//	var RankedT = orm.DefineDerived[Ranked]("ranked",
//	    func(t *orm.TableBuilder[Ranked]) *RankedModel {
//	        return &RankedModel{
//	            DerivedTable: t.Derived(),
//	            Username:     t.String("username"),
//	            Rank:         t.BigInt("rank"),
//	        }
//	    })
//
// It is DefineTable's own declaration, for a table that is not stored. The
// columns, their order, and the field of E each scans into are all worked
// out identically, so a derived table's columns are typed handles like any
// other and every query operation reads the same.
//
// Declaring the shape as a model rather than inferring it is what buys that.
// The alternative — naming the derived columns by string or by position —
// would give up the compile-time checking the rest of this package rests on
// for the one table where the columns are least obvious.
//
// # What it does not do
//
// It does not register the row type. The registry exists so a relationship
// can find the table declared for a row type, and a derived table has no
// relationships; registering it would shadow whatever real table was
// declared for the same type.
//
// It does not bind relationships, and rejects a model that declares one: a
// relationship is resolved through foreign keys between stored tables, and
// a derived table has none.
//
// It takes no part in migrations. Nothing extra is needed for that:
// schema.ExtractSchema is given its models explicitly, so a derived one is
// simply never passed to it.
//
// Failure is a panic, for the reason DefineTable's is.
func DefineDerived[E any, M Model](name string, build func(*TableBuilder[E]) M) M {
	st := &tableState{name: name, entity: reflect.TypeFor[E](), derived: true}
	m := build(&TableBuilder[E]{st: st})
	fillTableState(name, m, st, "DerivedTable: t.Derived()")

	if field, ok := firstRelationField(m); ok {
		panic(fmt.Sprintf("orm: derived table %q: field %s is a relationship, which a "+
			"derived table cannot have: a relationship is resolved through foreign keys "+
			"between stored tables", name, field))
	}

	return m
}

// firstRelationField reports the name of the model's first relationship
// marker, if it has one. It walks the model the same way bindRelations
// does, since what counts as a marker has to be the same question.
func firstRelationField(m Model) (string, bool) {
	v := reflect.ValueOf(m)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return "", false
	}
	t := v.Type()
	for i := range t.NumField() {
		if !t.Field(i).IsExported() {
			continue
		}
		// The check is on a fresh pointer rather than the field's own
		// address, so a model passed by value is inspected the same way one
		// passed by pointer is; nothing is bound here, only recognised.
		if _, ok := reflect.New(t.Field(i).Type).Interface().(relationBinder); ok {
			return t.Field(i).Name, true
		}
	}
	return "", false
}
