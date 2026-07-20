// Package postgres will hold Tork ORM's PostgreSQL driver, built on
// jackc/pgx/v5: connection handling, SQL DDL generation, and query
// execution. None of that exists yet. This package currently only adds
// the pgx dependency and hosts a connectivity smoke test (see
// tests/postgres).
//
// The model declaration types (Table, Column, ForeignKey, HasMany,
// BelongsTo) live in the module-root orm package, which never imports
// this package or pgx.
//
// Future drivers (SQLite, MySQL, SQL Server, and others) will follow the
// same pattern: one sibling package per database under driver/.
package postgres
