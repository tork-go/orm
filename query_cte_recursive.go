package orm

import (
	"fmt"
	"strings"
)

// A recursive CTE is a query that reads its own output. It starts from an
// anchor — the rows it can find without recursing — and then runs a step
// that joins against everything found so far, over and over, until a pass
// finds nothing new:
//
//	WITH RECURSIVE "reports"("id", "name", "manager_id") AS (
//	    SELECT "id", "name", "manager_id" FROM "employees" WHERE "manager_id" IS NULL
//	    UNION ALL
//	    SELECT "employees"."id", ... FROM "employees"
//	    JOIN "reports" ON "reports"."id" = "employees"."manager_id"
//	)
//	SELECT "id", "name", "manager_id" FROM "reports"
//
// That is the whole of it, and it is the one query shape no amount of
// ordinary SQL replaces: an org chart to any depth, a category tree, the
// parts of a part, a graph walked from a starting node.

// recursiveSpec is a recursive CTE's two halves, and whether the rows they
// find are pooled with duplicates or without.
type recursiveSpec struct {
	anchor DerivedSource
	step   DerivedSource

	// all is UNION ALL rather than UNION: every row the step finds is kept,
	// including one already in the pool. See DerivedTable.Recursive.
	all bool
}

// Recursive gives a derived table its rows by recursion: an anchor, and a
// step that reads what has been found so far.
//
//	type Report struct {
//	    ID        int
//	    Name      string
//	    ManagerID *int
//	}
//
//	var Reports = orm.DefineDerived[Report]("reports", ...)
//
//	tree, err := Reports.Recursive(
//	    // the anchor: everyone with no manager
//	    orm.SelectAs[Report](
//	        Employees.With(db).Where(Employees.ManagerID.IsNull()),
//	        Employees.ID, Employees.Name, Employees.ManagerID,
//	    ),
//	    // the step: everyone who reports to somebody already found
//	    orm.SelectAs[Report](
//	        Employees.With(db).JoinTo(Reports, Reports.ID.Value().Equals(Employees.ManagerID)),
//	        Employees.ID, Employees.Name, Employees.ManagerID,
//	    ),
//	).All(ctx)
//
// The step names the derived table itself, which is what makes it recursive:
// inside the recursion the model is an ordinary table reference, holding
// every row found so far. Nothing else about it is special — join it,
// filter on its columns, order by them.
//
// Both halves must yield exactly what the model declares, one for one and in
// order, which is checked here rather than left to the database, the same
// way From checks a derived table's single source.
//
// The result is a Filtered over the finished pool, so everything a read does
// it does: Where, OrderBy, Limit, a projection over it, a join onto it.
//
// # Recursion that ends
//
// Rows are pooled with UNION ALL, so a step that keeps finding rows keeps
// running. Over a tree that is what you want and it always ends, because a
// tree runs out of children. Over a graph that may contain a cycle it does
// not: A points at B, B points back at A, and the two feed each other
// forever. RecursiveDistinct is the answer there — it pools with UNION,
// which drops a row already found, so a cycle closes instead of looping.
//
// A depth column is the other guard, and it is worth carrying anyway when
// the answer is "how far", not merely "who": select a literal 0 in the
// anchor, that column plus one in the step, and filter on it afterwards.
func (d DerivedTable[E]) Recursive(anchor, step DerivedSource) *Filtered[E] {
	return d.recursive(anchor, step, true)
}

// RecursiveDistinct is Recursive pooling with UNION rather than UNION ALL,
// so a row already found is not added again — and a cycle in the data ends
// the recursion instead of feeding it.
//
// It costs what dropping duplicates always costs: every row found has to be
// compared against every row already in the pool. Recursive is the one to
// reach for over a tree, where there is nothing to compare.
func (d DerivedTable[E]) RecursiveDistinct(anchor, step DerivedSource) *Filtered[E] {
	return d.recursive(anchor, step, false)
}

func (d DerivedTable[E]) recursive(anchor, step DerivedSource, all bool) *Filtered[E] {
	out := &Filtered[E]{queryState{st: d.st}}
	if d.st == nil {
		out.fail(fmt.Errorf("orm: Recursive was called on a zero-valued DerivedTable; " +
			"declare the model with DefineDerived"))
		return out
	}
	for _, half := range []struct {
		name string
		src  DerivedSource
	}{{"anchor", anchor}, {"step", step}} {
		if half.src == nil {
			out.fail(fmt.Errorf("orm: derived table %q: Recursive was given no %s",
				d.st.name, half.name))
			return out
		}
		if err := half.src.derivedErr(); err != nil {
			out.fail(err)
			return out
		}
	}

	// The handle comes from the anchor, for the reason From takes it from
	// its own source: the query was built on one already, and a second would
	// be one more thing to keep in agreement.
	out.db = anchor.derivedDB()
	out.recursive = &recursiveSpec{anchor: anchor, step: step, all: all}
	if err := checkRecursiveShape(d.st, "anchor", anchor); err != nil {
		out.fail(err)
		return out
	}
	if err := checkRecursiveShape(d.st, "step", step); err != nil {
		out.fail(err)
	}
	return out
}

// checkRecursiveShape reports whether one half of a recursion yields exactly
// what the model declares, naming which half went wrong.
func checkRecursiveShape(st *tableState, half string, src DerivedSource) error {
	if err := checkDerivedShape(st, src.derivedShape()); err != nil {
		return fmt.Errorf("%s: %w", half, err)
	}
	return nil
}

// recursiveClause renders the WITH RECURSIVE definition, or "" when the
// query has no recursion.
//
// The column names are written out after the table's own — `"reports"("id",
// "name")` — rather than left to the anchor to alias. A recursive CTE names
// its columns once for both halves, which is both what SQL asks for and one
// less thing for the two halves to agree about.
//
// Both halves compile against the enclosing statement's compiler, so their
// placeholders number continuously with the rest of it, in the order they
// appear: the anchor's first, then the step's.
func (q queryState) recursiveClause(c *compiler) (string, error) {
	if q.recursive == nil {
		return "", nil
	}
	anchor, err := q.recursive.anchor.derivedSource(c, nil)
	if err != nil {
		return "", err
	}
	step, err := q.recursive.step.derivedSource(c, nil)
	if err != nil {
		return "", err
	}
	union := " UNION "
	if q.recursive.all {
		union = " UNION ALL "
	}

	cols := make([]string, len(q.st.cols))
	for i, col := range q.st.cols {
		cols[i] = c.d.QuoteIdent(col.Name())
	}
	return c.d.QuoteIdent(q.st.name) + "(" + strings.Join(cols, ", ") + ") AS (" +
		anchor + union + step + ")", nil
}
