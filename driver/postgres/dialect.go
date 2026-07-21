package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver"
)

// Dialect is the PostgreSQL implementation of driver.Dialect.
type Dialect struct{}

// init registers the driver under the schemes a Postgres connection string
// uses, so linking this package in is what makes orm.Connect understand them.
//
// Both spellings are registered because both are in the wild: libpq documents
// postgresql:// and accepts postgres://, and every tool in the ecosystem
// follows suit. Rejecting one would be a papercut with no upside.
func init() {
	orm.Register(Dialect{}, "postgres", "postgresql")
}

// Name returns "postgres".
func (Dialect) Name() string { return "postgres" }

// defaultPort is Postgres's own, used when a Config gives no port.
const defaultPort = 5432

// Open connects using cfg, returning a handle backed by a connection pool.
//
// A pool rather than a single connection: a connection serves one statement at
// a time, so a server handling requests concurrently would serialise on it.
// pgx offers both, and there is no reason to hand out the one that does not
// scale. A caller wanting exactly one connection can say MaxConns: 1.
func (Dialect) Open(ctx context.Context, cfg orm.Config) (driver.Conn, error) {
	pgCfg, err := pgxpool.ParseConfig(orm.BuildURL(cfg, "postgres", defaultPort))
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}

	// Only settings the caller actually chose are applied, so anything left
	// zero keeps pgx's own default rather than being overwritten with one.
	if cfg.MaxConns > 0 {
		pgCfg.MaxConns = int32(cfg.MaxConns)
	}
	if cfg.MinConns > 0 {
		pgCfg.MinConns = int32(cfg.MinConns)
	}
	if cfg.MaxConnLifetime > 0 {
		pgCfg.MaxConnLifetime = cfg.MaxConnLifetime
	}
	if cfg.MaxConnIdleTime > 0 {
		pgCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	}

	pool, err := pgxpool.NewWithConfig(ctx, pgCfg)
	if err != nil {
		return nil, err
	}
	// NewWithConfig is lazy, so a bad host or password would otherwise surface
	// at the first query rather than here, where the caller is still holding
	// the configuration that caused it.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &conn{pg: pool, closePool: pool.Close}, nil
}

// Connect opens a connection to dsn.
//
// It is Open in the shape driver.Dialect asks for, which is what migrations
// use: they are given a connection string and nothing else.
func (d Dialect) Connect(ctx context.Context, dsn string) (driver.Conn, error) {
	return d.Open(ctx, orm.Config{URL: dsn})
}
