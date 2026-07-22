package orm

import (
	"context"
	"fmt"
)

// Transaction runs fn inside a database transaction, committing when it
// returns nil and rolling back when it returns an error.
//
//	err := db.Transaction(ctx, func(tx *orm.DB) error {
//	    if err := Users.With(tx).Insert(ctx, user); err != nil {
//	        return err
//	    }
//	    post.AuthorID = user.ID
//	    return Posts.With(tx).Insert(ctx, post)
//	})
//
// The handle fn is given is an ordinary *DB, so everything else in the API
// works inside unchanged: it is only the statement surface that differs,
// and Execer is satisfied by an open transaction as readily as by a
// connection. That is what lets a caller move existing code inside a
// transaction without rewriting it.
//
// A panic rolls back too, and then continues unwinding. Returning the panic
// to the caller with the transaction still open would leave it open until
// the connection closed, holding whatever locks its statements had taken.
//
// # Joining rather than nesting
//
// A handle that is already a transaction has no connection to start a
// second one from, so fn runs against the existing one instead. Composition
// is the point: a bulk write opens a transaction of its own, and calling one
// inside a Transaction must not deadlock or silently commit halfway.
//
// A Transaction inside a Transaction opens a savepoint rather than a second
// transaction, so an inner failure undoes only the inner part and the outer
// one carries on. That is what nesting means, and it composes: a bulk write
// opening a transaction of its own inside a caller's neither deadlocks nor
// commits halfway.
func (db *DB) Transaction(ctx context.Context, fn func(tx *DB) error) error {
	return db.TransactionWith(ctx, TxOptions{}, fn)
}

// TransactionWith is Transaction, opened with options.
//
//	err := db.TransactionWith(ctx, orm.TxOptions{
//	    Isolation: orm.IsolationSerializable,
//	    Retries:   3,
//	}, func(tx *orm.DB) error { ... })
//
// The zero TxOptions is what Transaction runs with, so the two differ only
// in whether the caller had something to say.
//
// Options belong to a transaction rather than to a savepoint, so an inner
// call carrying them inside an outer transaction is refused: the level is
// already set, and quietly ignoring what was asked for would be worse than
// saying so.
func (db *DB) TransactionWith(ctx context.Context, opts TxOptions, fn func(tx *DB) error) error {
	if db == nil || db.ex == nil {
		return fmt.Errorf("orm: no database handle; pass one to NewDB")
	}
	if db.conn == nil {
		return db.nested(ctx, opts, fn)
	}
	if opts.Retries < 0 {
		return fmt.Errorf("orm: Retries(%d) is negative", opts.Retries)
	}

	var err error
	for attempt := 0; ; attempt++ {
		err = db.once(ctx, opts, fn)
		if err == nil || attempt >= opts.Retries || !db.d.IsRetryable(err) {
			return err
		}
		// A retry starts a fresh transaction: the failed one is already
		// rolled back, and a serialization failure leaves nothing to reuse.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return err
		}
	}
}

// once runs fn in one transaction, from BEGIN to COMMIT or ROLLBACK.
func (db *DB) once(ctx context.Context, opts TxOptions, fn func(tx *DB) error) error {
	tx, err := db.conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("orm: beginning a transaction: %w", err)
	}

	// conn is deliberately left nil, which is what marks this handle as
	// already being a transaction and makes the nesting above possible.
	inner := &DB{ex: tx, d: db.d}

	committed := false
	defer func() {
		if !committed {
			// The rollback's own error is dropped: it is reported while
			// something has already gone wrong, and replacing that first
			// failure with this one would hide the reason.
			_ = tx.Rollback(ctx)
		}
	}()

	// The options are applied as the transaction's first statement, before
	// anything the caller does can have read or written at the wrong level.
	setup, err := db.d.RenderTransactionOptions(opts)
	if err != nil {
		return fmt.Errorf("orm: %w", err)
	}
	if setup != "" {
		if _, err := tx.Exec(ctx, setup); err != nil {
			return fmt.Errorf("orm: setting the transaction's options: %w", err)
		}
	}

	if err := fn(inner); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("orm: committing: %w", err)
	}
	committed = true
	return nil
}

// atomically runs fn in a transaction when the operation takes more than
// one statement, and directly when it takes one.
//
// A single statement is already atomic, so wrapping it would cost a round
// trip either side for nothing. Several are not, which is the whole reason
// a bulk write that chunks has to open one: a failure partway through would
// otherwise leave the chunks before it committed.
func (db *DB) atomically(ctx context.Context, multiple bool, fn func(*DB) error) error {
	if !multiple {
		return fn(db)
	}
	return db.Transaction(ctx, fn)
}
