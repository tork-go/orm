package orm

import (
	"context"
	"fmt"
)

// An upsert is spelled in two steps, what conflicts and what to do about it,
// because those are two decisions and running them together produces a method
// per combination:
//
//	Users.With(db).OnConflict(Users.Email).DoNothing().InsertMany(ctx, users...)
//	Users.With(db).OnConflict(Users.Email).DoUpdateAll().Insert(ctx, user)
//
// Each step hands back a type carrying only what can follow it, so what to do
// next is a completion list rather than something to look up. The end of the
// chain is an Upsert, which offers Insert and InsertMany and nothing else:
// Update, Delete and Save have a row already and so nothing to conflict with.

// OnConflict begins an upsert: an insert that expects the row may already be
// there, and says what to do when it is.
//
// The columns are the ones whose duplication is what "already there" means,
// which is to say the ones carrying the unique constraint the insert would
// violate. That is usually a natural key rather than the primary key, since a
// row identified by a generated key is new by construction.
//
// Passing none means any conflict at all. That is only expressible for
// DoNothing, since overwriting has to know which row it is overwriting; the
// dialect says so if it is asked for the other.
func (q *Query[E]) OnConflict(cols ...ColumnMeta) *Conflict[E] {
	c := &Conflict[E]{q: q, clause: conflictClause{target: cols}}
	for i, col := range cols {
		if col == nil {
			c.clause.err = fmt.Errorf("orm: table %q: OnConflict column %d is nil",
				q.tableNameOrEmpty(), i)
			break
		}
	}
	return c
}

// Conflict is an upsert with its target chosen and its action still to come.
type Conflict[E any] struct {
	q      *Query[E]
	clause conflictClause
}

// DoNothing skips a row already there, leaving what is stored untouched.
//
//	n, err := Users.With(db).OnConflict(Users.Email).DoNothing().InsertMany(ctx, users...)
//
// The count is how many rows were actually written, which is the only report
// of a skip there is: nothing in a statement that wrote nothing says which row
// it declined to write.
func (c *Conflict[E]) DoNothing() *Upsert[E] {
	out := c.clause
	out.action = conflictNothing
	return &Upsert[E]{q: c.q, clause: out}
}

// DoUpdate overwrites the given columns of a row already there, with the
// values this insert was carrying for it.
//
//	Users.With(db).OnConflict(Users.Email).
//	    DoUpdate(Users.Username, Users.Age).
//	    InsertMany(ctx, users...)
//
// It names columns rather than taking assignments because the values are not
// the caller's to choose here: they are the ones already in the rows being
// inserted, and an upsert whose update wrote something else would be two
// statements wearing one name.
func (c *Conflict[E]) DoUpdate(cols ...ColumnMeta) *Upsert[E] {
	out := c.clause
	out.action = conflictUpdate
	out.update = cols
	if len(cols) == 0 {
		out.err = firstErr(out.err, fmt.Errorf("orm: table %q: DoUpdate was given no "+
			"columns to overwrite; name them, or use DoUpdateAll",
			c.q.tableNameOrEmpty()))
	}
	for i, col := range cols {
		if col == nil {
			out.err = firstErr(out.err, fmt.Errorf("orm: table %q: DoUpdate column %d is nil",
				c.q.tableNameOrEmpty(), i))
			break
		}
	}
	return &Upsert[E]{q: c.q, clause: out}
}

// DoUpdateAll overwrites every column the insert was writing, other than the
// ones it conflicted on and the primary key.
//
//	Users.With(db).OnConflict(Users.Email).DoUpdateAll().Insert(ctx, user)
//
// Which columns those are is decided per statement, not once: a column with a
// server default is left out of the insert while its field is still zero, and
// a column this insert did not write has nothing to copy from. So a batch
// whose rows write different columns overwrites different columns too, which
// is the only reading under which the rows all mean what they say.
//
// The primary key is excluded because it says which row is being written
// rather than what to write, the same rule Update follows.
func (c *Conflict[E]) DoUpdateAll() *Upsert[E] {
	out := c.clause
	out.action = conflictUpdateAll
	return &Upsert[E]{q: c.q, clause: out}
}

// Upsert is an insert that knows what to do about a row already there.
type Upsert[E any] struct {
	q      *Query[E]
	clause conflictClause
}

// Insert writes e, resolving a conflict as the chain said to, and reports
// whether the row was written: 1 when it was, 0 when DoNothing skipped it.
//
// Values the database produced are read back into e exactly as a plain Insert
// reads them, including the ones an overwrite settled on. A row that was
// skipped has nothing to read back and keeps the values it arrived with.
func (u *Upsert[E]) Insert(ctx context.Context, e *E) (int64, error) {
	return u.InsertMany(ctx, e)
}

// InsertMany writes es, resolving each conflict as the chain said to, and
// returns how many rows were written.
//
// A shortfall is not an error, unlike the one UpdateMany reports: rows that
// were already there is what an upsert is for, and the count is the answer to
// how many were new rather than the sign of a failure.
//
// BeforeCreate and AfterCreate run for every row given, including one the
// database went on to skip. Which rows those were is not something an upsert
// reports, so the alternative would be hooks that fire or not depending on
// what a statement covering several rows happened to do.
func (u *Upsert[E]) InsertMany(ctx context.Context, es ...*E) (int64, error) {
	if err := u.clause.err; err != nil {
		return 0, err
	}
	if len(es) == 0 {
		return 0, nil
	}
	return u.q.withConflict(&u.clause).insertMany(ctx, es)
}

// withConflict returns a copy of the query carrying the clause, so the insert
// path reads it from one place whether it came from Insert or from an Upsert.
func (q *Query[E]) withConflict(cl *conflictClause) *Query[E] {
	out := *q
	out.conflict = cl
	return &out
}

// tableNameOrEmpty names the table for an error built before anything has
// checked the query can run at all.
func (q *Query[E]) tableNameOrEmpty() string {
	if q.st == nil {
		return ""
	}
	return q.st.name
}

// conflictAction is what an upsert does about a row already there.
type conflictAction int

const (
	conflictNothing conflictAction = iota
	conflictUpdate
	conflictUpdateAll
)

// conflictClause is the whole of an upsert's intent, as data, for the reason
// predicates are data: the dialect writes the SQL, and this says only what it
// is being asked to write.
//
// err is what the chain that built it hit, kept rather than returned because
// a builder method cannot return one without breaking the chain. InsertMany
// reports it before running a hook, which is why nothing downstream of that
// has to look at it again.
type conflictClause struct {
	target []ColumnMeta
	action conflictAction
	update []ColumnMeta
	err    error
}

// conflict renders the clause an INSERT carries, or "" when it carries none.
//
// written is the columns this particular statement inserts, which is what
// DoUpdateAll overwrites and so what makes the clause depend on the statement
// rather than only on the chain that built it.
func (c *compiler) conflict(cl *conflictClause, written []ColumnMeta) (string, error) {
	if cl == nil {
		return "", nil
	}
	target, err := c.plainNames(cl.target)
	if err != nil {
		return "", err
	}
	if cl.action == conflictNothing {
		return c.d.RenderUpsertDoNothing(target)
	}

	update := cl.update
	if cl.action == conflictUpdateAll {
		update = overwritable(written, cl.target)
		if len(update) == 0 {
			return "", fmt.Errorf("orm: table %q: DoUpdateAll has nothing to overwrite; "+
				"every column this insert writes is part of the conflict target or of the "+
				"primary key", c.table)
		}
	}
	names, err := c.plainNames(update)
	if err != nil {
		return "", err
	}
	return c.d.RenderUpsertDoUpdate(target, names)
}

// plainNames quotes a list of columns without qualifying them, checking each
// belongs to the statement's table.
//
// Unqualified for the reason a SET clause is: these name columns of the table
// being written, and the value beside them is qualified by the dialect with
// whatever it calls the row that was proposed.
func (c *compiler) plainNames(cols []ColumnMeta) ([]string, error) {
	names := make([]string, len(cols))
	for i, col := range cols {
		if _, err := c.column(col); err != nil {
			return nil, err
		}
		names[i] = c.d.QuoteIdent(col.Name())
	}
	return names, nil
}

// overwritable is what DoUpdateAll resolves to: the columns this statement
// writes, less the ones it conflicted on and the primary key.
func overwritable(written, target []ColumnMeta) []ColumnMeta {
	skip := make(map[string]bool, len(target))
	for _, col := range target {
		skip[col.Name()] = true
	}
	out := make([]ColumnMeta, 0, len(written))
	for _, col := range written {
		if col.IsPrimaryKey() || skip[col.Name()] {
			continue
		}
		out = append(out, col)
	}
	return out
}
