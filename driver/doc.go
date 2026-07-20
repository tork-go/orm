// Package driver defines the contract every database driver implements:
// a minimal connection interface (Conn, Rows, Row, Tx) and a Dialect that
// knows how to talk to and generate SQL for one specific database.
//
// This package is deliberately not a database/sql replacement: it exists
// so each driver package can adapt its own native client (pgx for
// PostgreSQL, whatever's best for SQLite/MySQL/SQL Server later) instead
// of routing through a lowest-common-denominator interface.
//
// driver itself imports no database client. Only driver/postgres and
// future sibling packages do.
package driver
