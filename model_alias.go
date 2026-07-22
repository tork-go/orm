package orm

import "fmt"

// noAlias rejects an operation that cannot run over an alias, which is every
// write: a statement that changes rows names a stored table, and an alias
// exists only inside the statement that introduces it. Reading through one
// is what an alias is for, and stays allowed.
//
// It is on the table rather than on a query, unlike its neighbour noDerived,
// because the entity writes reach it through *Query, which holds its table
// directly rather than through a queryState.
func (st *tableState) noAlias(op string) error {
	if st == nil || st.aliasOf == "" {
		return nil
	}
	return fmt.Errorf("orm: table %q: %s cannot run over an alias of %q, which names no "+
		"stored table to write to; write through the model the alias was made from",
		st.name, op, st.aliasOf)
}

// Alias gives a table a second name, so one statement can name it twice.
//
//	mgr := orm.Alias(Employees, "mgr")
//
//	Employees.With(db).
//	    JoinAs(Employees.Manager, mgr).
//	    Where(mgr.Name.Equals("Ada")).
//	    All(ctx)
//
//	// SELECT "employees".* FROM "employees"
//	// JOIN "employees" AS "mgr" ON "mgr"."id" = "employees"."manager_id"
//	// WHERE "mgr"."name" = $1
//
// It returns the same model type it was given, with the same typed columns,
// each bound to the new name instead of the stored one. So mgr.Name is an
// ordinary StringColumn and mgr.Name.Equals("Ada") an ordinary predicate;
// what differs is only that it qualifies as "mgr"."name". Everything a query
// can do with a table's own columns it can do with an alias's, because they
// are columns in exactly the same sense.
//
// Two tables need a name of their own between them whenever one statement
// reaches the same table twice: a self join, which JoinAs is for, and a join
// onto a table already joined for another reason. Two tables with no
// relationship declared between them need no alias, only conditions; see
// Filtered.JoinTo.
//
// # How it is built
//
// The model is declared a second time, by re-running the very build function
// DefineTable was given, against a table state named for the alias. Nothing
// is copied and nothing is reflected over: the build function already knows
// how to produce a correctly wired model, and running it again is the only
// way to obtain one whose columns were bound to a different name from the
// start.
//
// Aliasing an alias aliases the stored table, so a chain of them never
// grows a chain of names.
//
// # What it does not do
//
// It does not register the row type. The registry maps a row type to the
// table declared for it, which is how a relationship finds the far side, and
// an alias registering itself would shadow the real table for every
// relationship naming that row type.
//
// It cannot be written through. An INSERT, UPDATE or DELETE names a stored
// table, and an alias exists only inside the statement that introduces it;
// every write reports that rather than writing to a table that is not there.
//
// It takes no part in migrations, for the reason DefineDerived gives: the
// models are handed to schema.ExtractSchema explicitly, so an alias is
// simply never passed to it.
//
// # Failure is a panic
//
// For the reason DefineTable's is: an alias is declared from a model and a
// name, neither of which can vary at run time, so a bad one is a mistake in
// the source that should surface where it is written rather than at the
// first query.
func Alias[M Model](m M, name string) M {
	st := stateOf(m)
	switch {
	case st == nil:
		panic(fmt.Sprintf("orm: Alias(%T, %q): this model carries no table identity; "+
			"declare it with DefineTable", m, name))
	case st.derived:
		panic(fmt.Sprintf("orm: derived table %q: Alias cannot alias a derived table, "+
			"whose name is already the alias its subquery is wrapped under", st.name))
	case st.rebuild == nil:
		panic(fmt.Sprintf("orm: table %q: Alias needs the model's build function, which "+
			"only DefineTable keeps; a model built with NewTable cannot be aliased",
			st.name))
	case name == "":
		panic(fmt.Sprintf("orm: table %q: Alias was given an empty name", st.name))
	case name == st.storageName():
		panic(fmt.Sprintf("orm: table %q: Alias was given the table's own name; an alias "+
			"is a second name, and one statement cannot tell two of the same apart",
			st.name))
	}

	alias := &tableState{
		name:    name,
		aliasOf: st.storageName(),
		entity:  st.entity,
		rebuild: st.rebuild,
	}
	// The assertion cannot fail: rebuild closes over the very build function
	// that produced m, so what comes back is the same type it returned then.
	return st.rebuild(alias).(M)
}
