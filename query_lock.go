package orm

import "fmt"

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

// locked records the mode, keeping whatever SkipLocked or NoWait already said
// so the two halves can be written in either order.
func (f *Filtered[E]) locked(mode LockMode) *Filtered[E] {
	out := f.clone()
	wait := LockBlock
	if out.lock != nil {
		wait = out.lock.wait
	}
	out.lock = &lockClause{mode: mode, wait: wait}
	return out
}

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
	out.lock = &lockClause{mode: out.lock.mode, wait: wait}
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
	clause, err := q.db.d.RenderLock(q.lock.mode, q.lock.wait)
	if err != nil {
		return "", fmt.Errorf("orm: table %q: %w", q.tableName(), err)
	}
	return " " + clause, nil
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
