package orm

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// The set operations write every row a query matches, in one statement,
// without loading any of them.
//
// They are named for All rather than for Many because All already means
// "every row this query matches" on the read side: All returns those rows,
// UpdateAll and DeleteAll write them. The Many operations are the other
// thing entirely, a batch of rows the caller is already holding; see query_bulk.go.
//
// Coming from Prisma, these are updateMany and deleteMany. Prisma spells the
// set operation with Many and has no name for the entity batch, so the two
// vocabularies collide on exactly one word. UpdateAll here is
// updateMany({where, data}) there.
//
// No write hook fires. A set operation is given a condition rather than a
// row, so there is no value to call BeforeUpdate on: the rows it writes may
// not even be represented in Go. That is the clearest thing the split between
// Query and Filtered buys, and it is why the entity operations live on the
// other side of it.
//
// The Returning forms are the exception, and only for AfterLoad: they really
// do read rows back, and a *E handed to a caller has been through AfterLoad
// however it was read. Nothing else changes; they still cannot fire a write
// hook, since the row they would call it on did not exist until the statement
// that returned it had already run.

// UpdateAll writes sets to every row of the table.
//
// It is the unfiltered form, which is the whole table; narrow it with Where
// first for anything else. See Filtered.UpdateAll.
func (q *Query[E]) UpdateAll(ctx context.Context, sets ...Assignment) (int64, error) {
	return q.filtered().UpdateAll(ctx, sets...)
}

// DeleteAll removes every row of the table.
//
// It is the unfiltered form, which is the whole table; narrow it with Where
// first for anything else. See Filtered.DeleteAll.
func (q *Query[E]) DeleteAll(ctx context.Context) (int64, error) {
	return q.filtered().DeleteAll(ctx)
}

// ForceDeleteAll removes every row of the table, ignoring any soft-delete
// column. See Filtered.ForceDeleteAll.
func (q *Query[E]) ForceDeleteAll(ctx context.Context) (int64, error) {
	return q.filtered().ForceDeleteAll(ctx)
}

// UpdateAllReturning writes sets to every row of the table and returns them.
//
// It is the unfiltered form; see Filtered.UpdateAllReturning.
func (q *Query[E]) UpdateAllReturning(ctx context.Context, sets ...Assignment) ([]*E, error) {
	return q.filtered().UpdateAllReturning(ctx, sets...)
}

// DeleteAllReturning removes every row of the table and returns them.
//
// It is the unfiltered form; see Filtered.DeleteAllReturning.
func (q *Query[E]) DeleteAllReturning(ctx context.Context) ([]*E, error) {
	return q.filtered().DeleteAllReturning(ctx)
}

// ForceDeleteAllReturning removes every row of the table, ignoring any
// soft-delete column, and returns them. See Filtered.ForceDeleteAllReturning.
func (q *Query[E]) ForceDeleteAllReturning(ctx context.Context) ([]*E, error) {
	return q.filtered().ForceDeleteAllReturning(ctx)
}

// UpdateAll writes sets to every row this query matches, in one statement,
// and returns how many rows it changed.
//
//	n, err := Users.With(db).
//	    Where(Users.Age.LessThan(18)).
//	    UpdateAll(ctx, Users.Active.Set(false), Users.Note.SetNull())
//
//	UPDATE "users" SET "active" = $1, "note" = $2 WHERE "age" < $3
//
// Every column type carries Set, and the nullable ones also carry SetPtr and
// SetNull, so what may be assigned is decided the same way what may be
// compared is: by the kind of the column, at compile time.
//
// To write a batch of rows that each need different values, use UpdateMany
// instead; this writes one set of values to many rows.
func (f *Filtered[E]) UpdateAll(ctx context.Context, sets ...Assignment) (int64, error) {
	sql, args, err := f.compileUpdateAll("UpdateAll", sets)
	if err != nil {
		return 0, err
	}
	res, err := f.db.ex.Exec(ctx, sql, args...)
	if err != nil {
		return 0, fmt.Errorf("orm: table %q: updating: %w", f.st.name, err)
	}
	return res.RowsAffected, nil
}

// UpdateAllReturning is UpdateAll, handing back the rows it wrote rather than
// counting them.
//
//	users, err := Users.With(db).
//	    Where(Users.Age.LessThan(18)).
//	    UpdateAllReturning(ctx, Users.Active.Set(false))
//
//	UPDATE "users" SET "active" = $1 WHERE "age" < $2
//	    RETURNING "id", "username", "email", "age"
//
// The rows come back as they are after the write, which is what makes this
// worth having over updating and then selecting: a column the database
// computed, a trigger changed, or another transaction had a hand in is
// readable without a second statement and without the gap between the two.
//
// Every column comes back, since a Select is not something a write takes; see
// readyForSetOp.
func (f *Filtered[E]) UpdateAllReturning(ctx context.Context, sets ...Assignment) ([]*E, error) {
	sql, args, err := f.compileUpdateAll("UpdateAllReturning", sets)
	if err != nil {
		return nil, err
	}
	return f.returning(ctx, "UpdateAllReturning", "UpdateAll", sql, args)
}

// compileUpdateAll builds the UPDATE both spellings run, checking what only
// a set operation can check on the way.
func (f *Filtered[E]) compileUpdateAll(op string, sets []Assignment) (string, []any, error) {
	if err := f.readyForSetOp(op); err != nil {
		return "", nil, err
	}
	if len(sets) == 0 {
		return "", nil, fmt.Errorf("orm: table %q: %s has nothing to write; "+
			"pass at least one assignment, as Users.Active.Set(false)", f.st.name, op)
	}

	c := f.compiler()

	// The SET clause is rendered before the WHERE so its values are bound
	// first, which is the order they appear in the statement.
	assignments, err := c.set(sets)
	if err != nil {
		return "", nil, err
	}
	where, err := c.where(f.effectivePreds())
	if err != nil {
		return "", nil, err
	}
	if err := f.requireFilter(op, "update"); err != nil {
		return "", nil, err
	}
	return "UPDATE " + c.d.QuoteIdent(f.st.name) + " SET " + assignments + where,
		c.args.args, nil
}

// DeleteAll removes every row this query matches, in one statement, and
// returns how many rows it removed.
//
//	n, err := Users.With(db).
//	    Where(Users.Email.IsNull(), Users.CreatedAt.LessThan(cutoff)).
//	    DeleteAll(ctx)
//
//	DELETE FROM "users" WHERE ("email" IS NULL AND "created_at" < $1)
//
// Matching no rows is not an error. Unlike Delete, which is given a row and
// so can say that row was not there, this is given a condition, and a
// condition matching nothing is an ordinary answer rather than a failure.
//
// A table with a soft-delete column is updated rather than deleted; see
// SoftDelete. ForceDeleteAll always removes the rows.
//
// To remove a batch of rows already in hand, use DeleteMany.
func (f *Filtered[E]) DeleteAll(ctx context.Context) (int64, error) {
	return f.deleteAll(ctx, "DeleteAll", false)
}

// ForceDeleteAll is DeleteAll, but always issues a physical DELETE even when
// the table has a soft-delete column.
func (f *Filtered[E]) ForceDeleteAll(ctx context.Context) (int64, error) {
	return f.deleteAll(ctx, "ForceDeleteAll", true)
}

func (f *Filtered[E]) deleteAll(ctx context.Context, op string, force bool) (int64, error) {
	sql, args, err := f.compileDeleteAll(op, force)
	if err != nil {
		return 0, err
	}
	res, err := f.db.ex.Exec(ctx, sql, args...)
	if err != nil {
		return 0, fmt.Errorf("orm: table %q: deleting: %w", f.st.name, err)
	}
	return res.RowsAffected, nil
}

// DeleteAllReturning is DeleteAll, handing back the rows it removed rather
// than counting them.
//
//	gone, err := Users.With(db).Where(Users.Draft.Equals(true)).DeleteAllReturning(ctx)
//
//	DELETE FROM "users" WHERE "draft" = $1
//	    RETURNING "id", "username", "email", "age"
//
// This is the only way to see a row a set operation removed. Selecting the
// rows first and then deleting them leaves a gap in which another transaction
// can change what the second statement matches, so the two lists need not be
// the same list; here they are the same statement.
func (f *Filtered[E]) DeleteAllReturning(ctx context.Context) ([]*E, error) {
	return f.deleteAllReturning(ctx, "DeleteAllReturning", "DeleteAll", false)
}

// ForceDeleteAllReturning is DeleteAllReturning, but always issues a
// physical DELETE even when the table has a soft-delete column.
func (f *Filtered[E]) ForceDeleteAllReturning(ctx context.Context) ([]*E, error) {
	return f.deleteAllReturning(ctx, "ForceDeleteAllReturning", "ForceDeleteAll", true)
}

func (f *Filtered[E]) deleteAllReturning(ctx context.Context, op, plain string, force bool) ([]*E, error) {
	sql, args, err := f.compileDeleteAll(op, force)
	if err != nil {
		return nil, err
	}
	return f.returning(ctx, op, plain, sql, args)
}

// compileDeleteAll builds the DELETE (or, for a soft-delete table not
// forced, the UPDATE) every spelling runs.
func (f *Filtered[E]) compileDeleteAll(op string, force bool) (string, []any, error) {
	if err := f.readyForSetOp(op); err != nil {
		return "", nil, err
	}

	c := f.compiler()

	if !force && f.st.softDelete != nil {
		// c.set's error is unreachable here for the reason writer.delete's
		// identical call documents: softDelete is always a real column of
		// this table.
		assignments, err := c.set([]Assignment{{Col: f.st.softDelete, Value: time.Now()}})
		if err != nil {
			return "", nil, err
		}
		where, err := c.where(f.effectivePreds())
		if err != nil {
			return "", nil, err
		}
		if err := f.requireFilter(op, "delete"); err != nil {
			return "", nil, err
		}
		return "UPDATE " + c.d.QuoteIdent(f.st.name) + " SET " + assignments + where,
			c.args.args, nil
	}

	where, err := c.where(f.effectivePreds())
	if err != nil {
		return "", nil, err
	}
	if err := f.requireFilter(op, "delete"); err != nil {
		return "", nil, err
	}
	return "DELETE FROM " + c.d.QuoteIdent(f.st.name) + where, c.args.args, nil
}

// returning appends the RETURNING clause and reads the rows back.
//
// plain is the counting spelling, named in the error so a caller on a driver
// without RETURNING is pointed at the operation that does work there rather
// than only told that this one does not.
func (f *Filtered[E]) returning(ctx context.Context, op, plain, sql string, args []any) ([]*E, error) {
	if !f.db.d.SupportsReturning() {
		return nil, fmt.Errorf("orm: table %q: %s needs the database to hand back the rows "+
			"a write touched, and this driver reports no support for that; use %s and read "+
			"the rows in a statement of your own", f.st.name, op, plain)
	}
	names := make([]string, len(f.st.cols))
	for i, col := range f.st.cols {
		names[i] = f.db.d.QuoteIdent(col.Name())
	}
	// collect is the ordinary row scanner, so a document column is decoded and
	// AfterLoad runs exactly as they do for a read.
	return f.collect(ctx, sql+" RETURNING "+strings.Join(names, ", "), args)
}

// readyForSetOp is ready, plus the clauses a set operation cannot carry.
//
// Ordering and paging narrow which rows come back, which is meaningful for a
// read and not expressible for a write: no dialect Tork targets accepts
// UPDATE or DELETE with an ORDER BY and a LIMIT. Dropping them silently
// would run a statement over every matching row when the caller had written
// one that appeared to touch ten.
func (f *Filtered[E]) readyForSetOp(op string) error {
	if err := f.ready(); err != nil {
		return err
	}
	// A derived table's rows are a query's output, which is not something
	// a statement can write back to.
	if err := f.noDerived(op); err != nil {
		return err
	}
	// An alias names no table to write to, for the reason noAlias gives.
	if err := f.noAlias(op); err != nil {
		return err
	}
	var clause string
	switch {
	case len(f.ords) > 0:
		clause = "an OrderBy"
	case f.limit != nil:
		clause = "a Limit"
	case f.offset != nil:
		clause = "an Offset"
	case f.sel != nil:
		// Select says which columns a read returns, which is not a question a
		// write answers: UpdateAll is told what to write by its assignments,
		// and DeleteAll writes nothing at all.
		clause = "a Select"
	case f.distinct:
		clause = "a Distinct"
	case len(f.distinctOn) > 0:
		clause = "a DistinctOn"
	case len(f.loads) > 0:
		// Loading fills a field of rows that were read. A write reads none.
		clause = "a Load"
	case f.lock != nil:
		// A write locks the rows it touches by writing them, so there is
		// nothing a ForUpdate could add, and no dialect accepts the clause on
		// an UPDATE or a DELETE.
		clause = "a ForUpdate or ForShare"
	case len(f.joins) > 0:
		// No dialect Tork targets writes a portable UPDATE or DELETE with a
		// JOIN in it; SelectAs is how a joined statement's columns are read.
		clause = "a Join or LeftJoin"
	case len(f.ctes) > 0:
		// A With is not rendered anywhere in an UPDATE or DELETE statement
		// today, so silently accepting one here would drop the CTE's own
		// definition while a WHERE naming it through CTE still referred to
		// it, producing a statement the database rejects as naming an
		// undefined relation instead of one Tork rejects by name.
		clause = "a With"
	default:
		return nil
	}
	return fmt.Errorf("orm: table %q: %s cannot take %s; it writes every row the "+
		"filter matches, and SQL has no way to order or count off the rows a write "+
		"touches; narrow the Where instead", f.st.name, op, clause)
}

// requireFilter rejects a set operation whose Where compiled to nothing.
//
// The case it exists for is a filter built at run time that came back empty:
//
//	conds := buildFilters(req)                    // no criteria this time
//	Users.With(db).Where(conds...).DeleteAll(ctx) // the whole table
//
// which reads as filtered and is not. Never calling Where is the opposite,
// a caller saying every row and meaning it, so that stays allowed and is the
// escape hatch this points at rather than a method of its own.
//
// It compiles the caller's own conditions with a compiler of its own,
// rather than reading the real statement's WHERE, and never the table's
// implicit default scope: that scope is not something the caller wrote,
// and its presence must not silently defeat this guard on exactly the
// tables where an accidental mass write is worst.
//
// It tests the compiled clause rather than the number of predicates, so a
// condition that is always true, like the empty And, is caught too: that
// compiles away to no WHERE at all and would otherwise slip past a count.
//
// check.where's error return is unreachable from either call site today:
// both already compile f.preds, as part of the larger effectivePreds, into
// the real statement before calling this, so a predicate bad enough to
// fail here would already have failed there. It is kept and handled rather
// than ignored because compiler.where can fail in general, and a future
// call site is not guaranteed to compile first.
func (f *Filtered[E]) requireFilter(op, verb string) error {
	if !f.whereCalled {
		return nil
	}
	check := &compiler{d: f.db.d, args: &argBuilder{d: f.db.d}, table: f.st.name}
	where, err := check.where(f.preds)
	if err != nil {
		return err
	}
	if where != "" {
		return nil
	}
	return fmt.Errorf("orm: table %q: Where added no condition, so %s would %s every "+
		"row; if that is what you meant, call %s without a Where",
		f.st.name, op, verb, op)
}
