package orm

import (
	"context"
	"fmt"
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
// No hook fires. A set operation never loads a row, so there is no value to
// call a method on: the rows it writes may not even be represented in Go.
// That is the clearest thing the split between Query and Filtered buys, and
// it is why the entity operations live on the other side of it.

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

// UpdateAll writes sets to every row this query matches, in one statement,
// and returns how many rows it changed.
//
//	n, err := Users.With(db).
//	    Where(Users.Age.Lt(18)).
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
	if err := f.readyForSetOp("UpdateAll"); err != nil {
		return 0, err
	}
	if len(sets) == 0 {
		return 0, fmt.Errorf("orm: table %q: UpdateAll has nothing to write; "+
			"pass at least one assignment, as Users.Active.Set(false)", f.st.name)
	}

	c := &compiler{d: f.db.d, args: &argBuilder{d: f.db.d}, table: f.st.name}

	// The SET clause is rendered before the WHERE so its values are bound
	// first, which is the order they appear in the statement.
	assignments, err := c.set(sets)
	if err != nil {
		return 0, err
	}
	where, err := c.where(f.preds)
	if err != nil {
		return 0, err
	}
	if err := f.requireFilter("UpdateAll", "update", where); err != nil {
		return 0, err
	}

	sql := "UPDATE " + c.d.QuoteIdent(f.st.name) + " SET " + assignments + where
	res, err := f.db.ex.Exec(ctx, sql, c.args.args...)
	if err != nil {
		return 0, fmt.Errorf("orm: table %q: updating: %w", f.st.name, err)
	}
	return res.RowsAffected, nil
}

// DeleteAll removes every row this query matches, in one statement, and
// returns how many rows it removed.
//
//	n, err := Users.With(db).
//	    Where(Users.Email.IsNull(), Users.CreatedAt.Lt(cutoff)).
//	    DeleteAll(ctx)
//
//	DELETE FROM "users" WHERE ("email" IS NULL AND "created_at" < $1)
//
// Matching no rows is not an error. Unlike Delete, which is given a row and
// so can say that row was not there, this is given a condition, and a
// condition matching nothing is an ordinary answer rather than a failure.
//
// To remove a batch of rows already in hand, use DeleteMany.
func (f *Filtered[E]) DeleteAll(ctx context.Context) (int64, error) {
	if err := f.readyForSetOp("DeleteAll"); err != nil {
		return 0, err
	}

	c := &compiler{d: f.db.d, args: &argBuilder{d: f.db.d}, table: f.st.name}
	where, err := c.where(f.preds)
	if err != nil {
		return 0, err
	}
	if err := f.requireFilter("DeleteAll", "delete", where); err != nil {
		return 0, err
	}

	sql := "DELETE FROM " + c.d.QuoteIdent(f.st.name) + where
	res, err := f.db.ex.Exec(ctx, sql, c.args.args...)
	if err != nil {
		return 0, fmt.Errorf("orm: table %q: deleting: %w", f.st.name, err)
	}
	return res.RowsAffected, nil
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
	case len(f.loads) > 0:
		// Loading fills a field of rows that were read. A write reads none.
		clause = "a Load"
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
// It tests the compiled clause rather than the number of predicates, so a
// condition that is always true, like the empty And, is caught too: that
// compiles away to no WHERE at all and would otherwise slip past a count.
func (f *Filtered[E]) requireFilter(op, verb, where string) error {
	if !f.whereCalled || where != "" {
		return nil
	}
	return fmt.Errorf("orm: table %q: Where added no condition, so %s would %s every "+
		"row; if that is what you meant, call %s without a Where",
		f.st.name, op, verb, op)
}
