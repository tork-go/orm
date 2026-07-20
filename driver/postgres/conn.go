package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/tork-go/orm/driver"
)

// conn adapts a *pgx.Conn to driver.Conn. pgx.Rows and pgx.Row already
// satisfy driver.Rows and driver.Row directly (same method signatures), so
// only Exec needs a wrapper, to discard pgx's command tag return value.
type conn struct {
	pg *pgx.Conn
}

func (c *conn) Query(ctx context.Context, sql string, args ...any) (driver.Rows, error) {
	return c.pg.Query(ctx, sql, args...)
}

func (c *conn) QueryRow(ctx context.Context, sql string, args ...any) driver.Row {
	return c.pg.QueryRow(ctx, sql, args...)
}

func (c *conn) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := c.pg.Exec(ctx, sql, args...)
	return err
}

func (c *conn) Begin(ctx context.Context) (driver.Tx, error) {
	pgTx, err := c.pg.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &tx{pg: pgTx}, nil
}

func (c *conn) Close(ctx context.Context) error {
	return c.pg.Close(ctx)
}

// tx adapts a pgx.Tx to driver.Tx.
type tx struct {
	pg pgx.Tx
}

func (t *tx) Query(ctx context.Context, sql string, args ...any) (driver.Rows, error) {
	return t.pg.Query(ctx, sql, args...)
}

func (t *tx) QueryRow(ctx context.Context, sql string, args ...any) driver.Row {
	return t.pg.QueryRow(ctx, sql, args...)
}

func (t *tx) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := t.pg.Exec(ctx, sql, args...)
	return err
}

func (t *tx) Commit(ctx context.Context) error {
	return t.pg.Commit(ctx)
}

func (t *tx) Rollback(ctx context.Context) error {
	return t.pg.Rollback(ctx)
}
