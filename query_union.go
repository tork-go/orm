package orm

import (
	"context"
	"fmt"
	"reflect"
)

// combineOp is which of the four set operations joins a Combined's two
// operands.
type combineOp int

const (
	combineUnion combineOp = iota
	combineUnionAll
	combineIntersect
	combineExcept
)

// String renders the SQL keyword joining the two operands.
func (o combineOp) String() string {
	switch o {
	case combineUnion:
		return "UNION"
	case combineUnionAll:
		return "UNION ALL"
	case combineIntersect:
		return "INTERSECT"
	case combineExcept:
		return "EXCEPT"
	}
	// Unreachable from this package's own callers, all four of which name a
	// constant declared above; kept rather than assumed, the same as
	// arithOp.String's own default, since combineOp is not sealed against a
	// stray value of the underlying int the way Predicate's cases are.
	return "?"
}

// name is how this op reads in an error message: the exported function
// that built it.
func (o combineOp) name() string {
	switch o {
	case combineUnion:
		return "Union"
	case combineUnionAll:
		return "UnionAll"
	case combineIntersect:
		return "Intersect"
	case combineExcept:
		return "Except"
	}
	// Unreachable for the same reason String's own default is: every caller
	// in this package names one of the constants declared above.
	return "Union"
}

// Combined is two queries over the same row type joined by a set operation —
// UNION, UNION ALL, INTERSECT or EXCEPT — built by Union, UnionAll,
// Intersect or Except.
//
// It carries its own OrderBy and Limit, applied once to the combined result
// rather than to either operand: SQL orders and limits a set operation's
// output as a whole. A combined result's column identity comes from the
// left operand, which is also where OrderBy's columns, and the rows All and
// First scan back into *E, both come from.
type Combined[E any] struct {
	op          combineOp
	left, right *Filtered[E]

	ords  []Ordering
	limit *int
	err   error
}

// Union reads every row either query matches, dropping duplicates.
//
//	admins     := Users.With(db).Where(Users.Role.Equals("admin"))
//	moderators := Users.With(db).Where(Users.Role.Equals("moderator"))
//	staff, err := orm.Union(admins, moderators).All(ctx)
//	// (SELECT ... FROM "users" WHERE "role" = $1) UNION (SELECT ... FROM "users" WHERE "role" = $2)
//
// Both queries must read the same number of columns; Tork checks the count,
// not whether the two line up type for type, which is the database's own
// job the same way it already is for every other value this package binds.
// Neither may carry a Join, a lock, or a Preload. See UnionAll, Intersect
// and Except for the other three operators.
func Union[E any](a, b *Filtered[E]) *Combined[E] { return newCombined(combineUnion, a, b) }

// UnionAll is Union, keeping duplicate rows rather than dropping them. It is
// the cheaper of the two: a plain UNION deduplicates by comparing every row
// against every other, which UnionAll never does.
func UnionAll[E any](a, b *Filtered[E]) *Combined[E] { return newCombined(combineUnionAll, a, b) }

// Intersect reads only the rows both queries match.
//
//	common, err := orm.Intersect(setA, setB).All(ctx)
func Intersect[E any](a, b *Filtered[E]) *Combined[E] { return newCombined(combineIntersect, a, b) }

// Except reads the rows the left query matches that the right does not.
//
//	onlyInA, err := orm.Except(setA, setB).All(ctx)
func Except[E any](a, b *Filtered[E]) *Combined[E] { return newCombined(combineExcept, a, b) }

func newCombined[E any](op combineOp, a, b *Filtered[E]) *Combined[E] {
	if a == nil || b == nil {
		return &Combined[E]{op: op, err: fmt.Errorf("orm: %s was given a nil query", op.name())}
	}
	return &Combined[E]{op: op, left: a, right: b}
}

// OrderBy sorts the combined result, applied once after both operands are
// combined rather than to either side. Its columns must belong to the left
// query's table, which is where a combined result's column identity comes
// from.
func (c *Combined[E]) OrderBy(ords ...Ordering) *Combined[E] {
	out := c.clone()
	out.ords = append(out.ords, ords...)
	return out
}

// Limit caps the number of rows the combined result returns, applied once
// after both operands are combined. A negative Limit is an error, reported
// from whichever terminal runs.
func (c *Combined[E]) Limit(n int) *Combined[E] {
	out := c.clone()
	if n < 0 {
		out.err = firstErr(out.err, fmt.Errorf("orm: %s: Limit(%d) is negative", c.op.name(), n))
		return out
	}
	out.limit = &n
	return out
}

// clone copies the query so a builder method can narrow the copy and leave
// the original alone, the same reason Filtered.clone gives.
func (c *Combined[E]) clone() *Combined[E] {
	out := *c
	out.ords = append([]Ordering(nil), c.ords...)
	return &out
}

// SQL returns the statement this would run, and its bound arguments,
// without running it.
func (c *Combined[E]) SQL() (string, []any, error) { return c.compile() }

func (c *Combined[E]) compile() (string, []any, error) {
	if err := c.checkOperands(); err != nil {
		return "", nil, err
	}

	// One argBuilder shared by both operands, the same technique
	// compiler.sub uses for a correlated subquery, so placeholders number
	// continuously across the whole statement rather than each operand's
	// own compiler restarting at 1 and colliding with the other's.
	args := &argBuilder{d: c.left.db.d}
	sql, err := c.render(args, nil)
	if err != nil {
		return "", nil, err
	}
	return sql, args.args, nil
}

// checkOperands is everything that has to hold before either operand is
// rendered: both runnable, on one handle, reading the same shape.
func (c *Combined[E]) checkOperands() error {
	if c.err != nil {
		return c.err
	}
	if err := c.left.ready(); err != nil {
		return err
	}
	if err := c.right.ready(); err != nil {
		return err
	}
	if c.left.db != c.right.db {
		return fmt.Errorf("orm: %s: the left and right query must share one "+
			"database handle", c.op.name())
	}
	if len(c.left.columns()) != len(c.right.columns()) {
		return fmt.Errorf("orm: %s: the left query reads %d column(s) but the "+
			"right reads %d; both sides of a %s must select the same shape",
			c.op.name(), len(c.left.columns()), len(c.right.columns()), c.op.name())
	}
	return nil
}

// render writes the combined statement against args.
//
// aliases, when given, go to the left operand alone: SQL takes a combined
// result's column names from its first operand, so naming them there names
// them for the whole thing, and repeating the names on the right would say
// nothing.
func (c *Combined[E]) render(args *argBuilder, aliases []string) (string, error) {
	leftSQL, err := c.left.compileWithinCombine(c.op.name(), args, aliases)
	if err != nil {
		return "", err
	}
	rightSQL, err := c.right.compileWithinCombine(c.op.name(), args, nil)
	if err != nil {
		return "", err
	}

	oc := c.left.compiler()
	oc.args = args
	order, err := oc.orderBy(c.ords)
	if err != nil {
		return "", err
	}

	return "(" + leftSQL + ") " + c.op.String() + " (" + rightSQL + ")" +
		order + limitOffset(c.limit, nil), nil
}

// derivedSource renders the combined query as a derived table's rows,
// sharing the enclosing statement's arguments so placeholders number
// continuously across the boundary.
func (c *Combined[E]) derivedSource(outer *compiler, aliases []string) (string, error) {
	if err := c.checkOperands(); err != nil {
		return "", err
	}
	return c.render(outer.args, aliases)
}

// derivedShape is the Go type each of the left operand's columns yields,
// which is what a combined result's columns are.
//
// The nil check cannot fire through From, which asks derivedErr first and
// stops on the error a nil operand already recorded. It is kept because a
// method that dereferences a field it does not own should not depend on
// being called in one particular order.
func (c *Combined[E]) derivedShape() []reflect.Type {
	if c.left == nil {
		return nil
	}
	cols := c.left.columns()
	out := make([]reflect.Type, len(cols))
	for i, col := range cols {
		out[i] = col.GoType()
	}
	return out
}

// derivedDB is the left operand's handle; the nil check is unreachable for
// the reason derivedShape's is.
func (c *Combined[E]) derivedDB() *DB {
	if c.left == nil {
		return nil
	}
	return c.left.db
}

// compileWithinCombine renders f as one operand of a Combined, sharing args
// with the other operand so the two placeholder sequences merge into one.
//
// A lock and a Join are rejected outright: no dialect Tork targets accepts
// either beside a set operation. A Preload is rejected too, for a different
// reason — it runs a query of its own once an operand's own rows are in
// hand, and a Combined never has an operand's rows on their own, only the
// combined statement's, so silently dropping it would report success while
// never running the load the caller wrote. A With is rejected because this
// renders only the operand's own SELECT, with nowhere to hang the WITH
// clause its CTE's definition needs.
func (f *Filtered[E]) compileWithinCombine(op string, args *argBuilder, aliases []string) (string, error) {
	if f.lock != nil {
		return "", fmt.Errorf("orm: table %q: %s cannot run over a query with a lock; "+
			"no dialect Tork targets accepts FOR UPDATE or FOR SHARE beside a set operation",
			f.st.name, op)
	}
	if err := f.noJoins(op); err != nil {
		return "", err
	}
	if err := f.noCTEs(op); err != nil {
		return "", err
	}
	if len(f.loads) > 0 {
		return "", fmt.Errorf("orm: table %q: %s cannot run over a query carrying a Preload; "+
			"the combined statement is read in one round trip, with no single operand's "+
			"rows to load against", f.st.name, op)
	}
	c := f.compiler()
	c.args = args
	// selectList's error is unreachable here for the same reason it is in
	// compileSelect: f.columns() is either f.sel, which Select already
	// rejects a foreign column from at build time, or f.st.cols, this
	// table's own columns, which always pass. Checked anyway, since
	// selectList is a general renderer with no way to know which caller
	// guarantees what.
	list, err := c.selectListAs(f.columns(), aliases)
	if err != nil {
		return "", err
	}
	return f.compileRead(c, list)
}

// derivedSource renders this read as a derived table's rows.
//
// A lock is rejected: a derived table's rows are the output of a query, and
// no dialect Tork targets locks rows through one. A Preload is rejected for
// the reason a combined query's is — it runs a query of its own against
// rows this statement never has on their own.
func (f *Filtered[E]) derivedSource(outer *compiler, aliases []string) (string, error) {
	if err := f.ready(); err != nil {
		return "", err
	}
	if err := f.noLock("From"); err != nil {
		return "", err
	}
	if len(f.loads) > 0 {
		return "", fmt.Errorf("orm: table %q: From cannot take a query carrying a Preload; "+
			"a derived table is read in one round trip, with no rows of its own to load "+
			"against", f.st.name)
	}
	c, err := f.compilerJoined()
	if err != nil {
		return "", err
	}
	c.args = outer.args
	with, err := f.cteClause(c)
	if err != nil {
		return "", err
	}
	// selectListAs's error is unreachable here for the reason
	// compileWithinCombine's identical call documents: f.columns() is
	// either f.sel, which Select already rejects a foreign column from at
	// build time, or this table's own columns, which always pass.
	list, err := c.selectListAs(f.columns(), aliases)
	if err != nil {
		return "", err
	}
	sql, err := f.compileRead(c, list)
	if err != nil {
		return "", err
	}
	return with + sql, nil
}

// derivedShape is the Go type each column this read covers yields.
func (f *Filtered[E]) derivedShape() []reflect.Type {
	cols := f.columns()
	out := make([]reflect.Type, len(cols))
	for i, col := range cols {
		out[i] = col.GoType()
	}
	return out
}

func (f *Filtered[E]) derivedDB() *DB { return f.db }

// All runs the combined statement and returns every row it matches.
func (c *Combined[E]) All(ctx context.Context) ([]*E, error) {
	sql, args, err := c.compile()
	if err != nil {
		return nil, err
	}
	return c.left.collect(ctx, sql, args)
}

// First returns the first row of the combined result, or ErrNoRows when it
// matched none.
//
// A limit the caller set is narrowed rather than respected, the same way
// Filtered.First's is: one row is all this reads either way.
func (c *Combined[E]) First(ctx context.Context) (*E, error) {
	rows, err := c.Limit(1).All(ctx)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNoRows
	}
	return rows[0], nil
}

func (c *Combined[E]) derivedErr() error { return c.err }

func (f *Filtered[E]) derivedErr() error { return f.err }
