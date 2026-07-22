package orm

import (
	"fmt"
	"reflect"
)

// DerivedSource is a query usable as a derived table's rows: what goes
// inside the parentheses of `FROM (...) AS name`.
//
// It is satisfied by a projection, a combined query and an ordinary
// filtered read, and by nothing outside this package.
type DerivedSource interface {
	// derivedSource renders the query as a subquery of the statement c is
	// compiling, aliasing its output columns to the names given so the
	// enclosing statement can refer to them.
	derivedSource(c *compiler, aliases []string) (string, error)

	// derivedShape is the Go type each output column decodes as, in order,
	// so the derived table's declared columns can be checked against what
	// the source actually yields.
	derivedShape() []reflect.Type

	// derivedDB is the handle the source was built on, which the derived
	// query runs on too.
	derivedDB() *DB

	// derivedErr is whatever the source is already carrying, so From
	// reports the reason the source is unusable rather than a consequence
	// of it — a combined query built from a nil operand has no handle and
	// no shape, and neither of those is the thing that went wrong.
	derivedErr() error
}

// From gives the derived table its rows.
//
//	inner := orm.SelectAs[Ranked](
//	    Users.With(db), Users.Username,
//	    orm.RowNumber().PartitionBy(Users.Country).OrderBy(Users.Age.Desc()),
//	)
//	top3, err := RankedT.From(inner).Where(RankedT.Rank.LessOrEqual(3)).All(ctx)
//
//	// SELECT "username", "rank" FROM (
//	//     SELECT "username", ROW_NUMBER() OVER (...) AS "rank" FROM "users"
//	// ) AS "ranked" WHERE "rank" <= $1
//
// It takes the database handle from the source rather than asking for one
// separately: the source already carries the handle it was built on, and a
// second would be one more thing to keep in agreement.
//
// The result is a Filtered rather than a Query, so the entity operations
// are not offered: a derived table has no stored row for Insert or Delete
// to identify.
//
// The source's shape is checked here — one output per declared column, each
// assignable to its column's type — so a mismatch is reported against the
// declaration rather than surfacing as a scan failure once rows come back.
func (d DerivedTable[E]) From(src DerivedSource) *Filtered[E] {
	out := &Filtered[E]{queryState{st: d.st}}
	if d.st == nil {
		out.fail(fmt.Errorf("orm: From was called on a zero-valued DerivedTable; " +
			"declare the model with DefineDerived"))
		return out
	}
	if src == nil {
		out.fail(fmt.Errorf("orm: derived table %q: From was given no source", d.st.name))
		return out
	}
	if err := src.derivedErr(); err != nil {
		out.fail(err)
		return out
	}
	out.db = src.derivedDB()
	out.derived = src
	if err := checkDerivedShape(d.st, src.derivedShape()); err != nil {
		out.fail(err)
	}
	return out
}

// checkDerivedShape reports whether the source yields exactly what the
// derived table declares, in count and in type. The wording follows
// SelectAs's own shape check, since it is the same kind of mistake.
func checkDerivedShape(st *tableState, shape []reflect.Type) error {
	if len(shape) != len(st.cols) {
		return fmt.Errorf("orm: derived table %q: declares %d column(s) but the source "+
			"yields %d; they are matched one for one, in order",
			st.name, len(st.cols), len(shape))
	}
	for i, col := range st.cols {
		if got := shape[i]; !got.AssignableTo(col.GoType()) {
			return fmt.Errorf("orm: derived table %q: column %d, %q, is %s but the "+
				"source's expression %d is %s",
				st.name, i, col.Name(), col.GoType(), i, got)
		}
	}
	return nil
}

// derivedAliases is the names a source must give its output columns: the
// derived table's own, in declaration order.
func (q queryState) derivedAliases() []string {
	out := make([]string, len(q.st.cols))
	for i, c := range q.st.cols {
		out[i] = c.Name()
	}
	return out
}

// fromClause renders what follows FROM: an ordinary table's quoted name, or
// a derived table's source wrapped and aliased under it.
//
// Every read calls this rather than quoting the name itself, so a derived
// table reached by a path that was never wired up says so instead of
// compiling to a reference to a table that does not exist.
//
// It must be called before the statement's WHERE is rendered. The FROM is
// textually earlier, so a derived source's own placeholders have to number
// first; this is the ordering cteClause already follows for the same
// reason.
func (q queryState) fromClause(c *compiler) (string, error) {
	name := c.d.QuoteIdent(q.st.name)
	if !q.st.derived {
		return name, nil
	}
	if q.derived == nil {
		return "", fmt.Errorf("orm: derived table %q: this query has no source; "+
			"a derived table is queried with From, not With", q.st.name)
	}
	sub, err := q.derived.derivedSource(c, q.derivedAliases())
	if err != nil {
		return "", err
	}
	return "(" + sub + ") AS " + name, nil
}

// noDerived rejects an operation that cannot run over a derived table,
// mirroring noJoins and noCTEs.
func (q queryState) noDerived(op string) error {
	if q.st == nil || !q.st.derived {
		return nil
	}
	return fmt.Errorf("orm: derived table %q: %s cannot run over a derived table, "+
		"whose rows come from a query rather than from storage", q.st.name, op)
}
