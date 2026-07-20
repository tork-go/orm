// Package postgres is Tork ORM's PostgreSQL driver, built on jackc/pgx/v5.
// It implements driver.Dialect: connecting, live schema introspection,
// migration DDL rendering, and the migrations history table.
//
// Query execution beyond migrations (the round 3 query API) is not built
// yet.
//
// Composite foreign key introspection is a known, documented gap:
// information_schema does not reliably preserve column-to-referenced-column
// ordinal alignment for multi-column foreign keys. Since the orm package's
// model API only ever produces single-column foreign keys, this is correct
// for every foreign key Tork ORM itself can create, and for the common
// case of single-column foreign keys in a hand-written schema.
//
// The model declaration types (Table, Column, ForeignKey, HasMany,
// BelongsTo) live in the module-root orm package, which never imports
// this package or pgx.
//
// Future drivers (SQLite, MySQL, SQL Server, and others) will follow the
// same pattern: one sibling package per database under driver/,
// implementing the same driver.Dialect contract.
package postgres
