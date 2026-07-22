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

	// conflict is what an insert does about a row already there, set only by
	// the OnConflict chain and read only by the insert path. See
	// query_upsert.go.
	conflict *conflictClause
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
type Filtered[E any] struct{ queryState }

// queryState is everything a query is, minus the row type.
//
// It is split out because not everything built from a query is parameterised
// by that type. Selecting one column yields a Scalars[T] whose T is the
// column's, not the row's, and it needs the same conditions, ordering and
// paging to run. Holding them here means the two share one representation
// rather than one copying the other's fields.
type queryState struct {
	st  *tableState
	db  *DB
	err error

	preds  []Predicate
	ords   []Ordering
	limit  *int
	offset *int

	// sel narrows which columns are read. Nil means the table's own, in
	// declaration order, which is what every read did before projections
	// existed and what scanning positionally still assumes.
	sel []ColumnMeta

	// distinct drops duplicate rows.
	distinct bool

	// unscoped disables the table's default scope for this query: its
	// Scoper predicate and/or its soft-delete "not yet deleted" filter.
	// See Unscoped.
	unscoped bool

	// loads are the relationships to fetch alongside the rows, each in a
	// statement of its own once the rows are in hand. See query_load.go.
	loads []loadSpec

	// lock is the row lock the read takes, nil when it takes none. See
	// query_lock.go.
	lock *lockClause

	// whereCalled records that Where was called at all, however many
	// conditions it went on to contribute. Reading rows does not care, but
	// UpdateAll and DeleteAll do: a Where that narrowed nothing is a
	// filter the caller meant to have and did not get, and running the
	// statement anyway would write every row in the table. Not calling
	// Where is a different thing entirely, and stays allowed.
	whereCalled bool
}

// columns returns the columns a read of this query covers.
func (q queryState) columns() []ColumnMeta {
	if q.sel != nil {
		return q.sel
	}
	return q.st.cols
}

// QuerySource is a query something else can be built from, satisfied by
// Query and Filtered and by nothing outside this package.
//
// It exists so orm.Select accepts either, since narrowing to one column is
// as reasonable before a Where as after one. The method is unexported, which
// is what stops anything else claiming to be a query.
type QuerySource interface {
	querySource() queryState
}

func (q *Query[E]) querySource() queryState    { return q.filtered().queryState }
func (f *Filtered[E]) querySource() queryState { return f.queryState }

// filtered starts a Filtered from an unfiltered query. Only Filtered can
// carry an error, since only its builder methods can fail.
func (q *Query[E]) filtered() *Filtered[E] {
	return &Filtered[E]{queryState{st: q.st, db: q.db}}
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

// Select narrows the read to the given columns.
func (q *Query[E]) Select(cols ...ColumnMeta) *Filtered[E] { return q.filtered().Select(cols...) }

// Distinct drops duplicate rows from the result.
func (q *Query[E]) Distinct() *Filtered[E] { return q.filtered().Distinct() }

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
	// sel is copied only when there is one, so the nil that means "every
	// column" stays nil rather than becoming an empty slice, which means
	// something else entirely.
	if f.sel != nil {
		out.sel = append([]ColumnMeta(nil), f.sel...)
	}
	out.loads = append([]loadSpec(nil), f.loads...)
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

// Select narrows the read to the given columns, in the order given.
//
//	users, err := Users.With(db).Select(Users.ID, Users.Username).All(ctx)
//
// The rows still come back as *E. A field whose column was not selected keeps
// its zero value, which is worth knowing: nothing distinguishes a name that
// was not read from one that is genuinely empty. Where a single column is
// what you actually want, orm.Select returns it typed and has no such
// ambiguity.
//
// Selecting accumulates rather than replacing, so a projection assembled in
// branches means what it reads as.
func (f *Filtered[E]) Select(cols ...ColumnMeta) *Filtered[E] {
	out := f.clone()
	if len(cols) == 0 {
		out.fail(fmt.Errorf("orm: table %q: Select was given no columns; "+
			"leave it out to read every column", f.tableName()))
		return out
	}
	for i, c := range cols {
		if c == nil {
			out.fail(fmt.Errorf("orm: table %q: Select column %d is nil", f.tableName(), i))
			return out
		}
	}
	// Appending to nil is what turns "every column" into "these", and
	// appending to an existing selection is what makes calls accumulate.
	out.sel = append(out.sel, cols...)
	return out
}

// Distinct drops duplicate rows from the result.
//
//	countries, err := orm.Select(Users.With(db), Users.Country).Distinct().All(ctx)
//
// Over a whole row it rarely says much, since a table with a primary key has
// no duplicate rows to drop. It earns its place on a projection, where the
// columns left out are exactly what made the rows differ.
func (f *Filtered[E]) Distinct() *Filtered[E] {
	out := f.clone()
	out.distinct = true
	return out
}

// tableName is the table's name, or "" for a query with no table, so an error
// built before ready has run does not dereference a nil.
func (q queryState) tableName() string {
	if q.st == nil {
		return ""
	}
	return q.st.name
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
	c := f.compiler()
	list, err := c.selectList(f.columns())
	if err != nil {
		return "", nil, err
	}
	sql, err := f.compileRead(c, list)
	if err != nil {
		return "", nil, err
	}
	return sql, c.args.args, nil
}

// compiler starts a compiler for this query's table.
func (q queryState) compiler() *compiler {
	return &compiler{d: q.db.d, args: &argBuilder{d: q.db.d}, table: q.st.name, unscoped: q.unscoped}
}

// compileRead wraps list in the clauses every read shares, so a projection, a
// single column and a whole row differ only in what they select.
func (q queryState) compileRead(c *compiler, list string) (string, error) {
	where, err := c.where(q.effectivePreds())
	if err != nil {
		return "", err
	}
	order, err := c.orderBy(q.ords)
	if err != nil {
		return "", err
	}
	// The lock goes last, after LIMIT: it is a property of the read as a
	// whole, and every dialect that has one puts it there.
	lock, err := q.lockSuffix()
	if err != nil {
		return "", err
	}
	keyword := "SELECT "
	if q.distinct {
		keyword = "SELECT DISTINCT "
	}
	return keyword + list + " FROM " + c.d.QuoteIdent(q.st.name) +
		where + order + limitOffset(q.limit, q.offset) + lock, nil
}

// ready reports whether the query can run and scan rows into E.
func (q queryState) ready() error {
	if err := q.readyToRead(); err != nil {
		return err
	}
	if q.st.fieldIdx == nil {
		return errNoEntityMapping(q.st.name)
	}
	return nil
}

// readyToRead reports whether the query can run a statement at all, without
// asking for an entity mapping.
//
// Counting, reading one column and aggregating all scan into something other
// than a row, so none of them needs the mapping a row would. A model declared
// with NewTable can therefore still be counted, which it could not be read
// from.
func (q queryState) readyToRead() error {
	if q.err != nil {
		return q.err
	}
	if q.st == nil {
		return errNoEntityMapping("")
	}
	if q.db == nil {
		return fmt.Errorf("orm: table %q: no database handle; pass one to With", q.st.name)
	}
	return nil
}

// All runs the query and returns every matching row.
func (f *Filtered[E]) All(ctx context.Context) ([]*E, error) {
	sql, args, err := f.compileSelect()
	if err != nil {
		return nil, err
	}
	rows, err := f.collect(ctx, sql, args)
	if err != nil {
		return nil, err
	}
	if err := f.load(ctx, rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// load fetches whatever Load asked for, once the rows it fills are in hand.
func (f *Filtered[E]) load(ctx context.Context, rows []*E) error {
	if len(f.loads) == 0 || len(rows) == 0 {
		return nil
	}
	if err := f.keysWereRead(); err != nil {
		return err
	}
	parents := make([]reflect.Value, len(rows))
	for i, r := range rows {
		parents[i] = reflect.ValueOf(r).Elem()
	}
	return runLoads(ctx, f.db, f.st, parents, f.loads)
}

// keysWereRead rejects a load whose matching column the query did not read.
//
// Related rows are matched to their parent by a column of the parent, so a
// projection that leaves it out gives every parent the same zero value to
// match on. The rows would arrive somewhere plausible and wrong, which is
// worth a statement that never runs.
func (f *Filtered[E]) keysWereRead() error {
	if f.sel == nil {
		return nil
	}
	read := make(map[string]bool, len(f.sel))
	for _, c := range f.sel {
		read[c.Name()] = true
	}
	for _, spec := range f.loads {
		info, err := spec.rel.info()
		if err != nil {
			return err
		}
		if !read[info.LocalColumn.Name()] {
			return fmt.Errorf("orm: table %q: loading %s needs column %q, which this "+
				"query does not select; add it to the Select, or drop the Select",
				f.st.name, spec.rel.field, info.LocalColumn.Name())
		}
	}
	return nil
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

	cols := f.columns()
	var out []*E
	for rows.Next() {
		e := new(E)
		if err := scanRowInto(f.st, rows, reflect.ValueOf(e).Elem(), cols); err != nil {
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
	if err := f.noLock("Count"); err != nil {
		return 0, err
	}
	c := f.compiler()
	where, err := c.where(f.effectivePreds())
	if err != nil {
		return 0, err
	}
	sql := "SELECT COUNT(*) FROM " + c.d.QuoteIdent(f.st.name) + where

	if f.distinct {
		// Counting a distinct query means counting the rows that query
		// returns, which is a count over the read rather than over the table.
		// The derived table is named because Postgres and MySQL both require
		// it; leaving it out would work only where it is optional.
		list, err := c.selectList(f.columns())
		if err != nil {
			return 0, err
		}
		sql = "SELECT COUNT(*) FROM (SELECT DISTINCT " + list + " FROM " +
			c.d.QuoteIdent(f.st.name) + where + ") AS " + c.d.QuoteIdent("t")
	}

	return scanCount(ctx, f.db, f.st.name, sql, c.args.args)
}

// scanCount runs a statement whose one row is one number and reads it, shared
// by counting rows and counting a single column's values.
func scanCount(ctx context.Context, db *DB, table, sql string, args []any) (int64, error) {
	rows, err := db.ex.Query(ctx, sql, args...)
	if err != nil {
		return 0, fmt.Errorf("orm: table %q: %w", table, err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, fmt.Errorf("orm: table %q: counting: %w", table, err)
		}
		return 0, fmt.Errorf("orm: table %q: COUNT returned no row", table)
	}
	var n int64
	if err := rows.Scan(&n); err != nil {
		return 0, fmt.Errorf("orm: table %q: scanning count: %w", table, err)
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
