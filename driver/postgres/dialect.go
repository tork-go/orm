package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/tork-go/orm/driver"
)

// Dialect is the PostgreSQL implementation of driver.Dialect.
type Dialect struct{}

// Name returns "postgres".
func (Dialect) Name() string { return "postgres" }

// Connect opens a connection to dsn using pgx.
func (Dialect) Connect(ctx context.Context, dsn string) (driver.Conn, error) {
	pg, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, err
	}
	return &conn{pg: pg}, nil
}
