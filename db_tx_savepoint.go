package orm

import (
	"context"
	"fmt"
	"sync/atomic"
)

// A transaction inside a transaction is a savepoint: a marker the database
// can roll back to without abandoning everything before it.
//
//	db.Transaction(ctx, func(tx *orm.DB) error {
//	    if err := Orders.With(tx).Insert(ctx, order); err != nil {
//	        return err
//	    }
//	    // The audit row is worth having and not worth failing over.
//	    _ = tx.Transaction(ctx, func(sp *orm.DB) error {
//	        return Audits.With(sp).Insert(ctx, audit)
//	    })
//	    return nil
//	})
//
// The inner failure undoes the audit row and nothing else; the order is
// still there when the outer transaction commits.
//
// Savepoints are spelled the same way by every database Tork targets —
// SAVEPOINT, ROLLBACK TO SAVEPOINT, RELEASE SAVEPOINT — so they are written
// here rather than asked of the dialect. A database that spells them
// differently is the one to add a dialect method for, and none of the
// targets does.

// savepoints numbers them, so two opened at once never share a name.
//
// The counter is per process rather than per transaction, which is more than
// enough: a name only has to be unique within its own transaction, and a
// number that never repeats is unique everywhere. It is atomic because two
// goroutines may hold transactions on two connections at the same time.
var savepoints atomic.Uint64

// nested runs fn between a savepoint and its release, which is what a
// Transaction called on a handle already inside one does.
//
// Options are refused rather than ignored: isolation and read-only belong to
// a transaction, and a savepoint is a marker inside one that has already
// begun. A caller who asked for serializable here would otherwise get
// whatever the outer transaction was opened with and no way to tell.
func (db *DB) nested(ctx context.Context, opts TxOptions, fn func(tx *DB) error) error {
	if opts != (TxOptions{}) {
		return fmt.Errorf("orm: this handle is already inside a transaction, so a nested " +
			"one is a savepoint and cannot set its own isolation, read-only or retries; " +
			"pass those to the outermost TransactionWith")
	}

	name := fmt.Sprintf("orm_sp_%d", savepoints.Add(1))
	quoted := db.d.QuoteIdent(name)
	if _, err := db.ex.Exec(ctx, "SAVEPOINT "+quoted); err != nil {
		return fmt.Errorf("orm: opening a savepoint: %w", err)
	}

	released := false
	defer func() {
		if !released {
			// The rollback's own error is dropped for the reason
			// Transaction's is: something has already gone wrong, and this
			// would replace the reason.
			_, _ = db.ex.Exec(ctx, "ROLLBACK TO SAVEPOINT "+quoted)
		}
	}()

	if err := fn(db); err != nil {
		return err
	}
	// Releasing is not committing: the work stays part of the transaction
	// this savepoint sits in, and it is that transaction's commit that
	// decides whether any of it lasts. What release does is drop the marker,
	// so the database need not keep the state to roll back to.
	if _, err := db.ex.Exec(ctx, "RELEASE SAVEPOINT "+quoted); err != nil {
		return fmt.Errorf("orm: releasing a savepoint: %w", err)
	}
	released = true
	return nil
}

// InTransaction reports whether this handle is inside a transaction.
//
// It answers the one question a caller cannot otherwise ask of a *DB, and
// the answer decides real things: whether a lock will be held past the
// statement that takes it, and whether a Transaction call will open one or
// mark a savepoint inside the one already open.
func (db *DB) InTransaction() bool { return db != nil && db.ex != nil && db.conn == nil }
