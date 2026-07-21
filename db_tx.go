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
// The consequence is that an inner Transaction returning an error rolls back
// the whole outer one, rather than just its own part. Real nesting means
// savepoints, which no driver method exposes yet; joining is the honest
// behaviour to document until one does.
func (db *DB) Transaction(ctx context.Context, fn func(tx *DB) error) error {
	if db == nil || db.ex == nil {
		return fmt.Errorf("orm: no database handle; pass one to NewDB")
	}
	if db.conn == nil {
		return fn(db)
	}

	tx, err := db.conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("orm: beginning a transaction: %w", err)
	}

	// conn is deliberately left nil, which is what marks this handle as
	// already being a transaction and makes the join above possible.
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
