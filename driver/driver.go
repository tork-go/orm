package driver

import (
	"context"
	"time"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/schema"
)

// AppliedRevision is one row from a Dialect's migrations history table.
type AppliedRevision struct {
	Revision  string
	AppliedAt time.Time
}

// The execution interfaces are declared in orm and re-exported here as
// aliases, not redeclared. An alias is the same type rather than a copy,
// so a driver.Conn is an orm.Conn and every reference to these names in
// this package, in migrate, and in each driver keeps working unchanged.
//
// They live in orm because query building hangs off Table[E], which is
// there, and a query has to reach a connection to run. orm cannot import
// driver without closing a cycle through schema, and a matching set
// redeclared here would not satisfy orm's, since Go interface satisfaction
// needs identical signatures and named types from different packages are
// different types however alike they look.
type (
	// Result is what a statement did.
	Result = orm.Result
	// Row scans a single query result row.
	Row = orm.Row
	// Rows is a cursor over query results.
	Rows = orm.Rows
	// Execer is the statement surface shared by a connection and an open
	// transaction, letting a Dialect's history methods run against either.
	Execer = orm.Execer
	// Tx is a transaction scoped Execer, without Begin or Close.
	Tx = orm.Tx
	// Conn is the minimal query and exec surface each driver adapts its
	// native client to.
	Conn = orm.Conn
)

// Dialect knows how to connect to, introspect, and generate SQL for one
// specific database. Each driver package (driver/postgres, and future
// siblings) provides one implementation.
//
// It embeds orm.QueryDialect, the handful of things a query compiler
// cannot write for itself, so a driver implements one interface and
// callers have one to pass around.
type Dialect interface {
	orm.QueryDialect

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
	// RenderAlterColumnDefault sets a column's DEFAULT clause, or drops it
	// when def is empty.
	RenderAlterColumnDefault(table, column, def string) []string
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
	RenderAddCheck(table string, c schema.Check) []string
	RenderDropCheck(table, name string) []string
	RenderAddForeignKey(table string, fk schema.ForeignKey) []string
	RenderDropForeignKey(table, name string) []string

	// RenderCreateEnumType, RenderDropEnumType, and RenderAddEnumValue
	// manage a native enum type's own lifecycle, separate from any single
	// column or table: an enum type can be shared by columns across
	// multiple tables.
	RenderCreateEnumType(e schema.EnumType) []string
	RenderDropEnumType(name string) []string
	// RenderAddEnumValue renders ALTER TYPE ... ADD VALUE, optionally
	// positioned via before/after (mutually exclusive; both empty appends
	// the value at the end).
	RenderAddEnumValue(name, value, before, after string) []string
}
