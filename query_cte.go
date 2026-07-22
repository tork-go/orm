package orm

import (
	"fmt"
	"strings"
)

// cteSpec is one named CTE a query carries, added by With, kept in the
// order they were added — which is also the order they render in the
// statement's WITH clause, and so the order their placeholders must
// number in.
type cteSpec struct {
	name string
	src  subquerySource
}

// With attaches a named CTE to the query, defined by source and read back
// elsewhere in the same query with CTE.
//
//	recent := orm.Select(Posts.With(db).Where(Posts.CreatedAt.GreaterThan(cutoff)), Posts.AuthorID)
//	users, err := Users.With(db).
//	    With("recent_authors", recent).
//	    Where(Users.ID.InQuery(orm.CTE[int]("recent_authors"))).
//	    All(ctx)
//	// WITH "recent_authors" AS (SELECT "author_id" FROM "posts" WHERE "created_at" > $1)
//	// SELECT ... FROM "users" WHERE "id" IN (SELECT * FROM "recent_authors")
//
// source is the same shape InQuery and NotInQuery already accept: what
// orm.Select returns. Calls accumulate, so a query can carry more than one
// CTE, each rendered in the order With was called.
//
// Only a plain read supports a With for now — Scalars, Grouped, the scalar
// aggregates, SelectAs, a combined query and a set operation all reject
// one rather than silently dropping its definition while a condition still
// refers to it. The recursive form, where a CTE's own step correlates
// against its own accumulating output, needs machinery this package does
// not have yet.
func (f *Filtered[E]) With(name string, source subquerySource) *Filtered[E] {
	out := f.clone()
	if name == "" {
		out.fail(fmt.Errorf("orm: table %q: With was given an empty name", f.tableName()))
		return out
	}
	if source == nil {
		out.fail(fmt.Errorf("orm: table %q: With(%q) was given no query", f.tableName(), name))
		return out
	}
	out.ctes = append(out.ctes, cteSpec{name: name, src: source})
	return out
}

// With is Filtered.With, starting from an unfiltered query.
func (q *Query[E]) With(name string, source subquerySource) *Filtered[E] {
	return q.filtered().With(name, source)
}

// cteClause renders every With this query carries as a WITH clause prefix,
// or "" when there are none.
//
// Each definition is compiled against c — the same compiler, and so the
// same argBuilder, the rest of the statement uses — before anything else of
// the statement is, so a CTE's own placeholders number first: they are
// textually first in "WITH ... AS (...) SELECT ...", and a placeholder's
// number has to match its position in the finished string, not the order
// its clause was built in Go.
func (q queryState) cteClause(c *compiler) (string, error) {
	if len(q.ctes) == 0 {
		return "", nil
	}
	parts := make([]string, len(q.ctes))
	for i, spec := range q.ctes {
		sql, err := spec.src.compileWithin(c)
		if err != nil {
			return "", err
		}
		parts[i] = c.d.QuoteIdent(spec.name) + " AS (" + sql + ")"
	}
	return "WITH " + strings.Join(parts, ", ") + " ", nil
}

// noCTEs rejects a query carrying a With, mirroring noJoins's shape.
func (q queryState) noCTEs(op string) error {
	if len(q.ctes) == 0 {
		return nil
	}
	return fmt.Errorf("orm: table %q: %s cannot run over a query carrying a With; "+
		"only a plain read supports a CTE for now", q.tableName(), op)
}

// CTE refers elsewhere in the same query to a CTE added by With.
//
// It renders as SELECT * FROM the CTE's own name, which works only because
// a CTE built by With is expected to define exactly one column, the same
// expectation orm.Select's own result already carries — so SELECT * FROM
// it is exactly the one-column SubqueryOf[T] a condition like InQuery
// expects. Naming T is what ties a CTE to the column it is compared
// against, the same way it does for any other SubqueryOf[T].
func CTE[T any](name string) SubqueryOf[T] { return cteRef[T]{name: name} }

// cteRef is CTE's result.
type cteRef[T any] struct{ name string }

func (cteRef[T]) subqueryOf(T) {}

func (c cteRef[T]) compileWithin(outer *compiler) (string, error) {
	if c.name == "" {
		return "", fmt.Errorf("orm: table %q: CTE was given an empty name", outer.table)
	}
	return "SELECT * FROM " + outer.d.QuoteIdent(c.name), nil
}
