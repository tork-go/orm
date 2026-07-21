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
// A column's ServerDefault is read back exactly as Postgres prints it,
// which is not always how it was written: a literal gains a cast and a
// call gains parentheses, so 'draft' comes back as 'draft'::text and
// now()::text as (now())::text. Reporting that faithfully is deliberate,
// since introspection says what the database contains; deciding that it
// matches what a model declared is the diff engine's job. An identity
// column reports no default, its sequence being an artefact of how
// identity is implemented rather than anything declared.
//
// Expression indexes and partial indexes are read back like any other,
// since schema.Index carries both expression keys and a predicate. Keys
// are read one at a time through pg_get_indexdef, which is what makes an
// expression key readable at all: pg_get_expr over indexprs returns every
// expression as one comma separated string, which cannot be split
// reliably when an expression itself contains a comma.
//
// An index mixing column and expression keys is still left alone.
// schema.Index records which keys an index has but not where each one sat,
// so a mixed index would come back with its keys reordered and read as a
// different index than the one in the database.
//
// The model declaration types (Table, Column, the typed column types, and
// the relationship markers) live in the module-root orm package, which
// never imports this package or pgx.
//
// Future drivers (SQLite, MySQL, SQL Server, and others) will follow the
// same pattern: one sibling package per database under driver/,
// implementing the same driver.Dialect contract.
package postgres
