package orm

import "context"

// The interfaces below describe a database connection, and they live here
// rather than in driver so that a model can name one.
//
// Query building hangs off Table[E], which is in this package, and a query
// has to reach a connection to run. But driver imports schema and schema
// imports orm, so orm importing driver would close a cycle. Nor is
// declaring a matching set here enough on its own: Go interface
// satisfaction needs identical method signatures, and a method returning
// driver.Rows does not satisfy one returning orm.Rows however alike the
// two look. Named types from different packages are simply different
// types.
//
// So driver re-exports these as aliases rather than redeclaring them. An
// alias is the same type, not a copy, which makes a driver.Conn an
// orm.Conn and leaves every existing reference to driver.Conn working
// untouched. This package still imports no driver, and still pulls in no
// database client; it gains context and nothing else.
//
// The set stays deliberately narrower than database/sql, for the reason
// driver's own documentation gives: a driver adapts its native client to
// this, rather than to a lowest common denominator it would have to fight.

// Result is what a statement did.
//
// Only RowsAffected for now. A driver whose database reports a last
// inserted id can gain a field here later without breaking any caller,
// since this is a struct rather than an interface. Postgres has no need of
// one: it returns generated values through RETURNING.
type Result struct {
	RowsAffected int64
}

// Row scans a single query result row.
type Row interface {
	Scan(dest ...any) error
}

// Rows is a cursor over query results.
//
// There is no way to ask it for the result's column names. Scanning is
// therefore positional, against the column list the caller generated,
// which is why a table records its columns in a fixed order.
type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close()
}

// Execer is the statement surface shared by a connection and an open
// transaction, so anything that only runs statements can take either.
type Execer interface {
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) Row
	Exec(ctx context.Context, sql string, args ...any) (Result, error)
}

// Tx is a transaction scoped Execer, without Begin or Close.
type Tx interface {
	Execer
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// Conn is the minimal query and exec surface each driver adapts its
// native client to.
type Conn interface {
	Execer
	Begin(ctx context.Context) (Tx, error)
	Close(ctx context.Context) error
}
