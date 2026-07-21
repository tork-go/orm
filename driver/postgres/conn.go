package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tork-go/orm/driver"
)

// pgxDB is the statement surface both *pgx.Conn and *pgxpool.Pool provide.
//
// Naming it means conn does not care which it holds, which is what lets a
// pooled handle and a single connection share one adapter. pgx offers no such
// interface itself.
type pgxDB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Begin(ctx context.Context) (pgx.Tx, error)
}

// conn adapts pgx to driver.Conn. pgx.Rows and pgx.Row already satisfy
// driver.Rows and driver.Row directly (same method signatures), so only Exec
// needs a wrapper, to turn pgx's command tag into a driver.Result.
type conn struct {
	pg pgxDB

	// closePool is how this handle is released. A pool's Close takes no
	// context and cannot fail, while a single connection's does and can, so
	// the difference is absorbed here rather than exposed.
	closePool func()
}

func (c *conn) Query(ctx context.Context, sql string, args ...any) (driver.Rows, error) {
	return c.pg.Query(ctx, sql, args...)
}

func (c *conn) QueryRow(ctx context.Context, sql string, args ...any) driver.Row {
	return c.pg.QueryRow(ctx, sql, args...)
}

func (c *conn) Exec(ctx context.Context, sql string, args ...any) (driver.Result, error) {
	tag, err := c.pg.Exec(ctx, sql, args...)
	if err != nil {
		return driver.Result{}, err
	}
	return driver.Result{RowsAffected: tag.RowsAffected()}, nil
}

func (c *conn) Begin(ctx context.Context) (driver.Tx, error) {
	pgTx, err := c.pg.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &tx{pg: pgTx}, nil
}

func (c *conn) Close(context.Context) error {
	if c.closePool != nil {
		c.closePool()
	}
	return nil
}

// tx adapts a pgx.Tx to driver.Tx.
//
// A transaction is one connection for its whole life, which the pool checks
// out at Begin and returns at Commit or Rollback. That is what makes a
// transaction on a pooled handle mean the same thing as on a single
// connection.
type tx struct {
	pg pgx.Tx
}

func (t *tx) Query(ctx context.Context, sql string, args ...any) (driver.Rows, error) {
	return t.pg.Query(ctx, sql, args...)
}

func (t *tx) QueryRow(ctx context.Context, sql string, args ...any) driver.Row {
	return t.pg.QueryRow(ctx, sql, args...)
}

func (t *tx) Exec(ctx context.Context, sql string, args ...any) (driver.Result, error) {
	tag, err := t.pg.Exec(ctx, sql, args...)
	if err != nil {
		return driver.Result{}, err
	}
	return driver.Result{RowsAffected: tag.RowsAffected()}, nil
}

func (t *tx) Commit(ctx context.Context) error   { return t.pg.Commit(ctx) }
func (t *tx) Rollback(ctx context.Context) error { return t.pg.Rollback(ctx) }
