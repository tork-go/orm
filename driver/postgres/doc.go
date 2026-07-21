// Package postgres is Tork ORM's PostgreSQL driver, built on jackc/pgx/v5.
// It implements driver.Dialect: connecting, live schema introspection,
// migration DDL rendering, and the migrations history table.
//
// Query execution beyond migrations (the round 3 query API) is not built
// yet.
//
// Foreign keys are read from pg_constraint rather than from
// information_schema, which cannot express a composite key: its
// constraint_column_usage view lists the referenced columns without tying
// each to the key column it pairs with. pg_constraint keeps conkey and
// confkey as ordered arrays, so unnesting them together pairs them off
// exactly as declared.
//
// A column's ServerDefault is rendered into a migration's DDL but is not
// read back by introspection, and is not compared by the diff engine.
// Introspecting it would need reading pg_attrdef, and comparing it would
// make every makemigrations run after the first propose the same "default
// changed" operation forever, since introspection would always report an
// empty default. This is a known, accepted limitation.
//
// Expression indexes and partial indexes are excluded from introspection,
// not misrepresented: schema.Index has no way to express either an
// expression key or a WHERE predicate, so they are left alone rather than
// introspected incorrectly.
//
// The model declaration types (Table, Column, the typed column types, and
// the relationship markers) live in the module-root orm package, which
// never imports this package or pgx.
//
// Future drivers (SQLite, MySQL, SQL Server, and others) will follow the
// same pattern: one sibling package per database under driver/,
// implementing the same driver.Dialect contract.
package postgres
