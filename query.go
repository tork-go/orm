package orm

import (
	"context"
	"errors"
	"fmt"
	"reflect"
)

// With begins a query against this table on the given handle.
//
//	users, err := Users.With(db).Where(Users.ID.Gt(100)).All(ctx)
func (t Table[E]) With(db *DB) *Query[E] {
	return &Query[E]{st: t.st, db: db}
}

// Query is a query over a whole table.
//
// It is the entry point for both reading and for the operations that act
// on a single row: Insert, Update, Save and Delete take a *E and find
// their row by primary key. Naming a condition turns it into a Filtered,
// which drops those operations, so writing a filter before one of them is
// a compile error rather than something reported at run time. A filter
// cannot change which row a *E identifies, so combining the two never
// meant anything.
type Query[E any] struct {
	st *tableState
	db *DB
}

// Filtered is a query narrowed by conditions, ordering or a limit.
//
// It reads rows and performs set operations. The entity operations are on
// Query instead; see there for why.
//
// Every builder method returns a new Filtered rather than narrowing the
// one it was called on, so a query is safe to hold and to branch from:
//
//	adults := Users.With(db).Where(Users.Age.Gte(18))
//	alice := adults.Where(Users.Name.Eq("alice"))
//	bob := adults.Where(Users.Name.Eq("bob"))
//
// Were they to share state, each branch would silently carry the other's
// conditions and both would match nothing.
type Filtered[E any] struct {
	st  *tableState
	db  *DB
	err error

	preds  []Predicate
	ords   []Ordering
	limit  *int
	offset *int

	// whereCalled records that Where was called at all, however many
	// conditions it went on to contribute. Reading rows does not care, but
	// UpdateAll and DeleteAll do: a Where that narrowed nothing is a
	// filter the caller meant to have and did not get, and running the
	// statement anyway would write every row in the table. Not calling
	// Where is a different thing entirely, and stays allowed.
	whereCalled bool
}

// filtered starts a Filtered from an unfiltered query. Only Filtered can
// carry an error, since only its builder methods can fail.
func (q *Query[E]) filtered() *Filtered[E] {
	return &Filtered[E]{st: q.st, db: q.db}
}

// Where narrows the query. Conditions are joined with AND; use orm.Or to
// nest alternatives.
func (q *Query[E]) Where(preds ...Predicate) *Filtered[E] {
	return q.filtered().Where(preds...)
}

// OrderBy sorts the results.
func (q *Query[E]) OrderBy(ords ...Ordering) *Filtered[E] {
	return q.filtered().OrderBy(ords...)
}

// Limit caps the number of rows returned.
func (q *Query[E]) Limit(n int) *Filtered[E] { return q.filtered().Limit(n) }

// Offset skips rows before returning any.
func (q *Query[E]) Offset(n int) *Filtered[E] { return q.filtered().Offset(n) }

// clone copies the query so a builder method can narrow the copy and leave
// the original alone.
//
// The slices are copied rather than shared, since appending to a shared
// backing array would let one branch overwrite another's conditions
// whenever the append happened to have spare capacity: a bug that appears
// only at certain lengths.
func (f *Filtered[E]) clone() *Filtered[E] {
	out := *f
	out.preds = append([]Predicate(nil), f.preds...)
	out.ords = append([]Ordering(nil), f.ords...)
	return &out
}

// Where narrows the query further. Conditions accumulate across calls and
// are joined with AND.
func (f *Filtered[E]) Where(preds ...Predicate) *Filtered[E] {
	out := f.clone()
	out.whereCalled = true
	out.preds = append(out.preds, preds...)
	return out
}

// OrderBy sorts the results. Terms accumulate across calls, in the order
// given.
func (f *Filtered[E]) OrderBy(ords ...Ordering) *Filtered[E] {
	out := f.clone()
	out.ords = append(out.ords, ords...)
	return out
}

// Limit caps the number of rows returned. A negative limit is an error,
// reported from whichever terminal runs.
func (f *Filtered[E]) Limit(n int) *Filtered[E] {
	out := f.clone()
	if n < 0 {
		out.fail(fmt.Errorf("orm: table %q: Limit(%d) is negative", f.st.name, n))
		return out
	}
	out.limit = &n
	return out
}

// Offset skips rows before returning any. A negative offset is an error.
func (f *Filtered[E]) Offset(n int) *Filtered[E] {
	out := f.clone()
	if n < 0 {
		out.fail(fmt.Errorf("orm: table %q: Offset(%d) is negative", f.st.name, n))
		return out
	}
	out.offset = &n
	return out
}

// fail records the first error a builder call hit.
//
// A builder method cannot return one without breaking the chain, so the
// error is kept and surfaces from the terminal. The first is kept rather
// than the last, since a later failure is usually a consequence of it.
func (f *Filtered[E]) fail(err error) {
	if f.err == nil {
		f.err = err
	}
}

// All runs the query and returns every matching row.
func (q *Query[E]) All(ctx context.Context) ([]*E, error) { return q.filtered().All(ctx) }

// First returns the first matching row, or ErrNoRows.
func (q *Query[E]) First(ctx context.Context) (*E, error) { return q.filtered().First(ctx) }

// Count returns how many rows the query matches.
func (q *Query[E]) Count(ctx context.Context) (int64, error) { return q.filtered().Count(ctx) }

// Exists reports whether the query matches any row.
func (q *Query[E]) Exists(ctx context.Context) (bool, error) { return q.filtered().Exists(ctx) }

// SQL returns the statement this query would run, and its bound
// arguments, without running it. Useful in tests and when reading what an
// unfamiliar chain produces.
func (q *Query[E]) SQL() (string, []any, error) { return q.filtered().SQL() }

// SQL returns the statement this query would run, and its bound arguments,
// without running it.
func (f *Filtered[E]) SQL() (string, []any, error) {
	sql, args, err := f.compileSelect()
	if err != nil {
		return "", nil, err
	}
	return sql, args, nil
}

// compileSelect builds the SELECT for this query.
func (f *Filtered[E]) compileSelect() (string, []any, error) {
	if err := f.ready(); err != nil {
		return "", nil, err
	}
	c := &compiler{d: f.db.d, args: &argBuilder{d: f.db.d}, table: f.st.name}

	where, err := c.where(f.preds)
	if err != nil {
		return "", nil, err
	}
	order, err := c.orderBy(f.ords)
	if err != nil {
		return "", nil, err
	}
	sql := "SELECT " + c.selectList(f.st.cols) + " FROM " + c.d.QuoteIdent(f.st.name) +
		where + order + limitOffset(f.limit, f.offset)
	return sql, c.args.args, nil
}

// ready reports whether the query can run at all.
func (f *Filtered[E]) ready() error {
	if f.err != nil {
		return f.err
	}
	if f.st == nil {
		return errNoEntityMapping("")
	}
	if f.db == nil {
		return fmt.Errorf("orm: table %q: no database handle; pass one to With", f.st.name)
	}
	if f.st.fieldIdx == nil {
		return errNoEntityMapping(f.st.name)
	}
	return nil
}

// All runs the query and returns every matching row.
func (f *Filtered[E]) All(ctx context.Context) ([]*E, error) {
	sql, args, err := f.compileSelect()
	if err != nil {
		return nil, err
	}
	return f.collect(ctx, sql, args)
}

// collect runs sql and scans every row.
//
// Each row is allocated on its own rather than sliced out of one backing
// array. Growing a []E reallocates, which would invalidate every pointer
// already handed out, and those pointers are what AfterLoad, eager loading
// and change tracking all hold on to.
func (f *Filtered[E]) collect(ctx context.Context, sql string, args []any) ([]*E, error) {
	rows, err := f.db.ex.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("orm: table %q: %w", f.st.name, err)
	}
	defer rows.Close()

	var out []*E
	for rows.Next() {
		e := new(E)
		if err := scanRowInto(f.st, rows, reflect.ValueOf(e).Elem()); err != nil {
			return nil, err
		}
		if err := runHook(ctx, f.st.name, "AfterLoad", any(e), AfterLoader.AfterLoad); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("orm: table %q: reading rows: %w", f.st.name, err)
	}
	return out, nil
}

// First returns the first matching row, or ErrNoRows when there is none.
//
// It runs with a LIMIT of one and asks whether a row came back, rather
// than using QueryRow. Row has no Err method, so a QueryRow reports "no
// rows" only through its driver's own sentinel, and this package imports
// no driver and so cannot recognise one.
func (f *Filtered[E]) First(ctx context.Context) (*E, error) {
	// A limit the caller set is narrowed rather than respected: one row is
	// all this reads either way. The copy is what keeps that from being
	// visible in the query it was called on.
	rows, err := f.Limit(1).All(ctx)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNoRows
	}
	return rows[0], nil
}

// Count returns how many rows the query matches.
//
// Ordering and paging are dropped: they change which rows come back, not
// how many match, and ORDER BY in a count is wasted work.
func (f *Filtered[E]) Count(ctx context.Context) (int64, error) {
	if err := f.ready(); err != nil {
		return 0, err
	}
	c := &compiler{d: f.db.d, args: &argBuilder{d: f.db.d}, table: f.st.name}
	where, err := c.where(f.preds)
	if err != nil {
		return 0, err
	}
	sql := "SELECT COUNT(*) FROM " + c.d.QuoteIdent(f.st.name) + where

	rows, err := f.db.ex.Query(ctx, sql, c.args.args...)
	if err != nil {
		return 0, fmt.Errorf("orm: table %q: %w", f.st.name, err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, fmt.Errorf("orm: table %q: counting: %w", f.st.name, err)
		}
		return 0, fmt.Errorf("orm: table %q: COUNT returned no row", f.st.name)
	}
	var n int64
	if err := rows.Scan(&n); err != nil {
		return 0, fmt.Errorf("orm: table %q: scanning count: %w", f.st.name, err)
	}
	return n, rows.Err()
}

// Exists reports whether the query matches any row.
func (f *Filtered[E]) Exists(ctx context.Context) (bool, error) {
	_, err := f.First(ctx)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, ErrNoRows):
		return false, nil
	default:
		return false, err
	}
}

// Find returns the row with the given primary key, or ErrNoRows.
//
// The key is checked against the primary key column's type before the
// statement is built, so a mismatch is reported as such rather than as
// whatever the database says about a wrongly typed parameter. A table with
// a composite primary key has no single value to look up by, and says so.
func (q *Query[E]) Find(ctx context.Context, key any) (*E, error) {
	if q.st == nil {
		return nil, errNoEntityMapping("")
	}
	if q.st.fieldIdx == nil {
		return nil, errNoEntityMapping(q.st.name)
	}
	pk := q.st.pk
	switch {
	case len(pk) == 0:
		return nil, fmt.Errorf("orm: table %q: Find needs a primary key, and this "+
			"table declares none; use Where instead", q.st.name)
	case len(pk) > 1:
		names := make([]string, len(pk))
		for i, c := range pk {
			names[i] = c.Name()
		}
		return nil, fmt.Errorf("orm: table %q: Find takes one key but the primary key "+
			"is %v; use Where instead", q.st.name, names)
	}
	if key == nil {
		return nil, fmt.Errorf("orm: table %q: Find was given a nil key", q.st.name)
	}
	if kt := reflect.TypeOf(key); !kt.AssignableTo(pk[0].GoType()) {
		return nil, fmt.Errorf("orm: table %q: Find was given a %s but %q is %s",
			q.st.name, kt, pk[0].Name(), pk[0].GoType())
	}
	return q.Where(Comparison{Col: pk[0], Op: OpEq, Value: key}).First(ctx)
}
