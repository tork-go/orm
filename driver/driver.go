package driver

import (
	"context"
	"time"

	"github.com/tork-go/orm/schema"
)

// AppliedRevision is one row from a Dialect's migrations history table.
type AppliedRevision struct {
	Revision  string
	AppliedAt time.Time
}

// Row scans a single query result row.
type Row interface {
	Scan(dest ...any) error
}

// Rows is a cursor over query results.
type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close()
}

// Tx is a transaction-scoped Conn, without Begin or Close.
type Tx interface {
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) Row
	Exec(ctx context.Context, sql string, args ...any) error
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// Conn is the minimal query and exec surface each driver adapts its
// native client to.
type Conn interface {
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) Row
	Exec(ctx context.Context, sql string, args ...any) error
	Begin(ctx context.Context) (Tx, error)
	Close(ctx context.Context) error
}

// Execer is satisfied by both Conn and Tx (they share this method set),
// letting a Dialect's history methods run against either a plain
// connection or an open transaction.
type Execer interface {
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) Row
	Exec(ctx context.Context, sql string, args ...any) error
}

// Dialect knows how to connect to, introspect, and generate SQL for one
// specific database. Each driver package (driver/postgres, and future
// siblings) provides one implementation.
type Dialect interface {
	// Name identifies the dialect, e.g. "postgres".
	Name() string

	// Connect opens a Conn using dsn.
	Connect(ctx context.Context, dsn string) (Conn, error)

	// Introspect reads the current schema for exactly the given tables.
	Introspect(ctx context.Context, conn Conn, tables []string) (schema.Schema, error)

	// EnsureHistoryTable creates the migrations history table if it does
	// not already exist.
	EnsureHistoryTable(ctx context.Context, conn Conn) error

	// InsertHistoryRow records that revision (whose parent is
	// downRevision) has been applied. exec is a Conn or an open Tx, so the
	// caller can record this in the same transaction as the migration's
	// own SQL.
	InsertHistoryRow(ctx context.Context, exec Execer, revision, downRevision string) error

	// DeleteHistoryRow removes revision's history record, used when
	// rolling it back.
	DeleteHistoryRow(ctx context.Context, exec Execer, revision string) error

	// AppliedRevisions returns every revision recorded as applied, in no
	// particular order.
	AppliedRevisions(ctx context.Context, exec Execer) ([]AppliedRevision, error)

	// RenderCreateTable, RenderDropTable, ... render one schema.Operation
	// kind each into the SQL statements that apply it. They return a
	// slice, not a single string, since some drivers need more than one
	// statement for an operation a simpler dialect can do in one (SQLite's
	// limited ALTER TABLE support is the motivating example).
	RenderCreateTable(t schema.Table) ([]string, error)
	RenderDropTable(table string) []string
	RenderAddColumn(table string, col schema.Column) ([]string, error)
	RenderDropColumn(table, column string) []string
	RenderAlterColumnType(table string, col schema.Column) ([]string, error)
	RenderAlterColumnNullability(table, column string, notNull bool) []string
	RenderAddPrimaryKey(table string, pk schema.PrimaryKey) []string
	RenderDropPrimaryKey(table, name string) []string
	RenderAddUnique(table string, u schema.UniqueConstraint) []string
	RenderDropUnique(table, name string) []string
	RenderAddIndex(table string, idx schema.Index) []string
	// RenderDropIndex keeps table for consistency with every other Drop*
	// method here, and because some dialects need it: MySQL's
	// DROP INDEX name ON table genuinely requires it in the syntax, even
	// though Postgres itself doesn't (its index names are schema-scoped,
	// not table-scoped).
	RenderDropIndex(table, name string) []string
	RenderAddForeignKey(table string, fk schema.ForeignKey) []string
	RenderDropForeignKey(table, name string) []string
}
