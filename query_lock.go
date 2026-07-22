package orm

import (
	"fmt"
	"strings"
)

// Locking is what turns a read followed by a write into something two
// transactions can run at once without one of them losing.
//
//	err := db.Transaction(ctx, func(tx *orm.DB) error {
//	    jobs, err := Jobs.With(tx).
//	        Where(Jobs.State.Equals("queued")).
//	        ForUpdate().SkipLocked().
//	        Limit(10).
//	        All(ctx)
//	    ...
//	})
//
// The lock is held until the transaction ends, which is the whole point and
// also the reason a lock outside one buys nothing: a statement of its own is
// already a transaction, and it is over by the time the rows come back.
// Nothing here rejects that, since a caller may be running inside a
// transaction this package did not open, but it is worth knowing.

// LockMode is how strongly a read locks the rows it returns.
type LockMode int

const (
	// LockUpdate is the exclusive lock, taken by a reader about to write.
	LockUpdate LockMode = iota

	// LockShare lets other readers take the same lock and blocks writers,
	// which is what a reader depending on a row staying as it is wants.
	LockShare

	// LockNoKeyUpdate is LockUpdate without blocking the writes that only
	// reference the row: it conflicts with a change to the key columns, and
	// not with another transaction inserting a row whose foreign key points
	// here. It is the weaker exclusive lock, for a reader about to write
	// columns nobody references.
	LockNoKeyUpdate

	// LockKeyShare is LockShare narrowed the same way: it holds the row's
	// key still, so a foreign key pointing at it stays valid, while leaving
	// its other columns free to change. It is what a write to a child row
	// takes on its parent, and taking it deliberately is how a reader avoids
	// blocking one.
	LockKeyShare
)

// LockWait is what a read does about a row another transaction has already
// locked.
type LockWait int

const (
	// LockBlock waits for the other transaction to end, which is what a lock
	// does when nothing says otherwise.
	LockBlock LockWait = iota

	// LockSkip passes over the locked rows and returns the rest.
	LockSkip

	// LockNoWait fails rather than waiting.
	LockNoWait
)

// lockClause is a read's locking intent, as data, for the reason predicates
// are: the dialect writes the SQL and this says only what it is asked to.
type lockClause struct {
	mode LockMode
	wait LockWait

	// of narrows the lock to these tables' rows, and is empty for a lock
	// that takes every table the statement reads. See Filtered.LockOf.
	of []string
}

// ForUpdate locks the rows this query returns against every other
// transaction, which is what a reader about to write them needs.
//
//	Jobs.With(tx).Where(Jobs.State.Equals("queued")).ForUpdate().Limit(10).All(ctx)
//	// SELECT ... FROM "jobs" WHERE "state" = $1 LIMIT 10 FOR UPDATE
//
// Without it, two workers reading the same queue both see the same rows and
// both claim them; the second write silently overwrites the first. With it,
// the second reader waits until the first transaction ends and then sees what
// it did.
//
// It is only meaningful inside a transaction, since that is what the lock is
// held for the length of.
func (f *Filtered[E]) ForUpdate() *Filtered[E] { return f.locked(LockUpdate) }

// ForShare locks the rows this query returns against writers, while letting
// other readers take the same lock.
//
// It is the weaker half of the pair, for a reader that needs a row to stay as
// it is without intending to change it, such as one checking a balance before
// writing somewhere else.
func (f *Filtered[E]) ForShare() *Filtered[E] { return f.locked(LockShare) }

// ForNoKeyUpdate is ForUpdate without blocking the writes that only
// reference these rows.
//
// It conflicts with a change to the row's key and with another exclusive
// lock, and not with a transaction inserting a row whose foreign key points
// here. Where ForUpdate would make every child insert wait, this lets them
// through — which is what a reader about to write a column nobody references
// actually needs.
func (f *Filtered[E]) ForNoKeyUpdate() *Filtered[E] { return f.locked(LockNoKeyUpdate) }

// ForKeyShare holds these rows' keys still, leaving their other columns free
// to change.
//
// It is the lock a write to a child row already takes on its parent, so
// taking it deliberately is how a reader says "this row must keep existing
// with this key" without blocking anyone updating the rest of it.
func (f *Filtered[E]) ForKeyShare() *Filtered[E] { return f.locked(LockKeyShare) }

// locked records the mode, keeping whatever SkipLocked, NoWait or LockOf
// already said so the parts can be written in any order.
func (f *Filtered[E]) locked(mode LockMode) *Filtered[E] {
	out := f.clone()
	next := lockClause{mode: mode}
	if out.lock != nil {
		next.wait = out.lock.wait
		next.of = out.lock.of
	}
	out.lock = &next
	return out
}

// LockOf narrows the lock to the rows of these tables, rather than every
// table the statement reads.
//
//	Books.With(tx).Join(Books.Author).ForUpdate().LockOf(Books).All(ctx)
//	// SELECT ... FROM "books" JOIN "authors" ... FOR UPDATE OF "books"
//
// A locking read over a join locks a row of every table it touched, which is
// rarely what was meant: a reader about to write a book has no reason to
// hold its author still, and holding it blocks everyone else editing that
// author's other books. Naming the tables is how to take only the lock the
// write needs.
//
// A table named here has to be one the statement reads, under the name the
// statement reads it as — so an aliased table is named by its alias, which
// is what the model orm.Alias returned already reports.
func (f *Filtered[E]) LockOf(tables ...Model) *Filtered[E] {
	out := f.clone()
	if out.lock == nil {
		out.fail(fmt.Errorf("orm: table %q: LockOf narrows a lock to some of the tables "+
			"read, so it needs a lock of its own; call ForUpdate or ForShare first",
			f.tableName()))
		return out
	}
	if len(tables) == 0 {
		out.fail(fmt.Errorf("orm: table %q: LockOf was given no tables; leave it out to "+
			"lock every table the statement reads", f.tableName()))
		return out
	}
	next := *out.lock
	next.of = append(append([]string(nil), next.of...), make([]string, 0, len(tables))...)
	for i, m := range tables {
		st := stateOf(m)
		if st == nil {
			out.fail(fmt.Errorf("orm: table %q: LockOf table %d carries no table identity; "+
				"declare it with DefineTable", f.tableName(), i))
			return out
		}
		next.of = append(next.of, st.name)
	}
	out.lock = &next
	return out
}

// ForUpdate is Filtered.ForUpdate, off an unfiltered query — locking every
// row of the table, which a queue reader taking whatever is next does.
func (q *Query[E]) ForUpdate() *Filtered[E] { return q.filtered().ForUpdate() }

// ForShare is Filtered.ForShare, off an unfiltered query.
func (q *Query[E]) ForShare() *Filtered[E] { return q.filtered().ForShare() }

// ForNoKeyUpdate is Filtered.ForNoKeyUpdate, off an unfiltered query.
func (q *Query[E]) ForNoKeyUpdate() *Filtered[E] { return q.filtered().ForNoKeyUpdate() }

// ForKeyShare is Filtered.ForKeyShare, off an unfiltered query.
func (q *Query[E]) ForKeyShare() *Filtered[E] { return q.filtered().ForKeyShare() }

// SkipLocked passes over rows another transaction has locked rather than
// waiting for them.
//
//	Jobs.With(tx).Where(Jobs.State.Equals("queued")).ForUpdate().SkipLocked().Limit(10).All(ctx)
//	// SELECT ... FOR UPDATE SKIP LOCKED
//
// This is what makes a table usable as a work queue. Ten workers each asking
// for the next ten queued jobs get ten different batches instead of nine of
// them waiting behind the first, and none of them has to coordinate with the
// others to manage it.
//
// The rows it skips are not reported: a query asking for ten may return fewer,
// and that is the answer rather than a shortfall.
func (f *Filtered[E]) SkipLocked() *Filtered[E] { return f.waiting(LockSkip, "SkipLocked") }

// NoWait fails rather than waiting for a row another transaction has locked.
//
// It is for the caller who would rather report contention than sit behind it,
// where SkipLocked is for the one who would rather take different rows.
func (f *Filtered[E]) NoWait() *Filtered[E] { return f.waiting(LockNoWait, "NoWait") }

// waiting records what to do about a locked row.
//
// A mode has to have been chosen: SQL has no way to say "skip locked rows"
// about a read that is not locking any, and a statement carrying only half of
// the clause would be a syntax error naming nothing the caller wrote.
func (f *Filtered[E]) waiting(wait LockWait, op string) *Filtered[E] {
	out := f.clone()
	if out.lock == nil {
		out.fail(fmt.Errorf("orm: table %q: %s says what to do about a row another "+
			"transaction has locked, so it needs a lock of its own; call ForUpdate or "+
			"ForShare first", f.tableName(), op))
		return out
	}
	next := *out.lock
	next.wait = wait
	out.lock = &next
	return out
}

// lockSuffix renders the locking clause, or "" when the read takes no lock.
func (q queryState) lockSuffix() (string, error) {
	if q.lock == nil {
		return "", nil
	}
	if q.distinct || len(q.distinctOn) > 0 {
		// A lock names rows of a table, and a read that collapses rows
		// returns values rather than rows: two rows that became one have no
		// single row to lock. Postgres rejects the pair outright, and saying
		// so here names what the caller wrote rather than what the statement
		// became.
		clause := "Distinct"
		if len(q.distinctOn) > 0 {
			clause = "DistinctOn"
		}
		return "", fmt.Errorf("orm: table %q: a locking read cannot also be %s, "+
			"since collapsing duplicate rows leaves nothing in particular to lock",
			q.tableName(), clause)
	}
	if err := q.lockableJoins(); err != nil {
		return "", err
	}
	of, err := q.lockOfNames()
	if err != nil {
		return "", err
	}
	clause, err := q.db.d.RenderLock(q.lock.mode, q.lock.wait, of)
	if err != nil {
		return "", fmt.Errorf("orm: table %q: %w", q.tableName(), err)
	}
	return " " + clause, nil
}

// lockOfNames is the quoted tables the lock is narrowed to, having checked
// that every one of them is a table the statement reads.
//
// A name the statement never read is refused here rather than sent: the
// database's own complaint names a relation the caller may not recognise —
// an alias they wrote is not the table it aliases — while this one can say
// which tables were available.
func (q queryState) lockOfNames() ([]string, error) {
	if len(q.lock.of) == 0 {
		return nil, nil
	}
	read := map[string]bool{q.st.name: true}
	names := []string{q.st.name}
	for _, spec := range q.joins {
		name := spec.joinedName()
		read[name] = true
		names = append(names, name)
	}

	quoted := make([]string, len(q.lock.of))
	for i, name := range q.lock.of {
		if !read[name] {
			return nil, fmt.Errorf("orm: table %q: LockOf names table %q, which this "+
				"statement does not read; it reads %s",
				q.tableName(), name, strings.Join(names, ", "))
		}
		quoted[i] = q.db.d.QuoteIdent(name)
	}
	return quoted, nil
}

// lockableJoins rejects a locking read whose rows may have no row of the
// joined table behind them.
//
// A lock names rows. A left, right or full join hands back rows where one
// side matched nothing, and there is no row there to lock — Postgres rejects
// the pair outright, naming the nullable side. Saying so here names what the
// caller wrote, and points at the narrowing that makes it legal.
func (q queryState) lockableJoins() error {
	for _, spec := range q.joins {
		if spec.kind == joinInner {
			continue
		}
		if len(q.lock.of) > 0 && !q.lockNames(spec.joinedName()) {
			// The lock was already narrowed away from this join's table, so
			// the nullable side is not being locked at all.
			continue
		}
		return fmt.Errorf("orm: table %q: a locking read cannot cover %q, which this "+
			"statement joins in a way that returns rows with nothing there to lock; "+
			"narrow the lock with LockOf, or join with Join rather than LeftJoin",
			q.tableName(), spec.joinedName())
	}
	return nil
}

// lockNames reports whether the lock's OF list covers this table.
func (q queryState) lockNames(table string) bool {
	for _, name := range q.lock.of {
		if name == table {
			return true
		}
	}
	return false
}

// noLock rejects a lock on a statement that cannot carry one.
//
// Counting and aggregating collapse the rows into a number, so there is
// nothing left to lock and no clause to attach: Postgres rejects FOR UPDATE
// beside an aggregate outright, and a dialect that accepted it would be
// locking rows the caller never sees. Dropping the clause silently would run
// a statement holding no lock where the caller wrote one that appeared to,
// which is the failure locking exists to prevent.
func (q queryState) noLock(op string) error {
	if q.lock == nil {
		return nil
	}
	return fmt.Errorf("orm: table %q: %s collapses the rows into a value, so there is "+
		"nothing left for ForUpdate or ForShare to lock; lock the read that returns the "+
		"rows instead", q.tableName(), op)
}
